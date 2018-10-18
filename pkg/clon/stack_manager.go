package clon

import (
	"fmt"
	"io/ioutil"

	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/juju/errors"
	"github.com/spirius/clon/internal/pkg/cfn"
	"github.com/spirius/clon/internal/pkg/s3file"
)

// StackManager is high level API for managing AWS CloudFormation
// stacks.
type StackManager struct {
	name string

	awsClient *awsClient
	config    *Config

	stackOrder   []string
	stacks       map[string]*stack
	stackConfigs map[string]*StackConfig

	fileConfigs map[string]FileConfig

	bucket string
	vars   map[string]string
	files  map[string]*s3file.File

	emit func(interface{})
}

// stackName returns the full stack name.
func (sm *StackManager) stackName(name string) string {
	return fmt.Sprintf("%s-%s", sm.name, name)
}

// addStack adds the stack to stack manager. Stack will be
// initialized from CloudFormation backend.
func (sm *StackManager) addStack(name string, stackConfig StackConfig) error {
	if name == "" {
		return errors.Errorf("stack name is empty")
	}
	if _, ok := sm.stacks[name]; ok {
		return errors.Errorf("duplicate stack %s", name)
	}
	stack, err := newStack(sm, sm.stackName(name), name)
	if err != nil {
		return errors.Annotatef(err, "cannot create Stack")
	}
	sm.stacks[name] = stack
	sm.stackConfigs[name] = &stackConfig
	return nil
}

// getStack returns the stack and stack config.
func (sm *StackManager) getStack(name string) (*stack, *StackConfig, error) {
	stack, ok := sm.stacks[name]
	if !ok {
		return nil, nil, errors.Errorf("stack '%s' not found", name)
	}
	stackConfig, ok := sm.stackConfigs[name]
	if !ok {
		return nil, nil, errors.Errorf("stack config for '%s' not found", name)
	}
	return stack, stackConfig, nil
}

// renderStackData renders the StackData from config and uploads template to S3 bucket if needed.
// In order for S3 upload to work, bucket field must be set by calling SetBucket.
func (sm *StackManager) renderStackData(name string, stackConfig *StackConfig) (*StackData, error) {
	var err error
	sd := &StackData{
		StackData: cfn.StackData{
			Name:         sm.stackName(name),
			Parameters:   make(map[string]string, len(stackConfig.Parameters)),
			Tags:         make(map[string]string, len(stackConfig.Tags)),
			Capabilities: make([]string, len(stackConfig.Capabilities)),
		},
	}

	if sd.StackData.RoleARN, err = sm.render(stackConfig.RoleARN); err != nil {
		return nil, errors.Annotatef(err, "cannot render RoleARN of stack '%s'", name)
	}

	copy(sd.Capabilities, stackConfig.Capabilities)
	if err := sm.renderMapToMap(stackConfig.Parameters, sd.Parameters); err != nil {
		return nil, errors.Annotatef(err, "cannot render Parameters for stack '%s'", name)
	}
	if err := sm.renderMapToMap(stackConfig.Tags, sd.Tags); err != nil {
		return nil, errors.Annotatef(err, "cannot render Tags for stack '%s'", name)
	}

	if sm.bucket != "" {
		tpl, err := s3file.Write(sm.awsClient.s3conn, s3file.Config{
			Region: sm.awsClient.region,
			Bucket: sm.bucket,
			Prefix: "templates/",
			Source: stackConfig.Template,
		})
		if err != nil {
			return nil, errors.Annotatef(err, "cannot upload template '%s' for stack '%s'", stackConfig.Template, name)
		}
		sd.TemplateURL = tpl.URL
	} else {
		// should be used only for bootstrapping
		content, err := ioutil.ReadFile(stackConfig.Template)
		if err != nil {
			return nil, errors.Annotatef(err, "cannot read template for stack '%s'", name)
		}
		sd.TemplateBody = string(content)
	}
	return sd, nil
}

// renderMapToMap renders each element of src and sets the result in dst with same key.
// Argument maps must be initialized before calling this function.
func (sm *StackManager) renderMapToMap(src map[string]string, dst map[string]string) error {
	for k, v := range src {
		value, err := sm.render(v)
		if err != nil {
			return errors.Annotatef(err, "cannot render value '%s' for key '%s' ", v, k)
		}
		dst[k] = value
	}
	return nil
}

// render will render the content as golang template using
// context of StackManager.
func (sm *StackManager) render(content string) (string, error) {
	ctx := map[string]interface{}{
		"Name": sm.name,
		"Var":  sm.vars,
		"File": sm.files,
	}
	return renderTemplate(content, ctx, map[string]interface{}{
		"stack": sm.tplGetStackData,
	})
}

// tplGetStackData is function exposed to template engine with name
// GetStack.
func (sm *StackManager) tplGetStackData(name string) (*StackData, error) {
	stack, _, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot find stack '%s'", name)
	}
	if !stack.exists() {
		return nil, errors.Errorf("stack '%s' is not deployed", name)
	}
	return stack.stackData(), nil
}

// Plan creates plan of changes.
func (sm *StackManager) Plan(name string) (*Plan, error) {
	stack, stackConfig, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot plan stack '%s'", name)
	}
	stackData, err := sm.renderStackData(name, stackConfig)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot plan '%s', stack input rendering failed", name)
	}

	cs, err := stack.plan(stackData)
	if err != nil {
		err = errors.Annotatef(err, "stack '%s' plan failed", name)
	}
	var plan *Plan
	var err2 error
	if cs != nil {
		plan, err2 = newPlan(cs.Data(), stack.stackData(), sm.config.IgnoreNestedUpdates)
		if err2 != nil {
			return nil, errors.Trace(err2)
		}
	}
	return plan, err
}

// GetPlan returns the Plan data.
func (sm *StackManager) GetPlan(name, planID string) (*Plan, error) {
	stack, _, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack '%s'", name)
	}
	changeSetID := (arn.ARN{
		Partition: "aws",
		Service:   "cloudformation",
		Region:    sm.awsClient.region,
		AccountID: sm.awsClient.accountID,
		Resource:  "changeSet/" + planID,
	}).String()
	cs, err := stack.getChangeSet(&cfn.ChangeSetData{
		ID:        changeSetID,
		StackData: &stack.stackData().StackData,
	})
	if err != nil {
		return nil, errors.Annotatef(err, "cannot execute change set '%s' for stack '%s'", changeSetID, name)
	}
	return newPlan(cs.Data(), stack.stackData(), sm.config.IgnoreNestedUpdates)
}

// SetEventHandler sets the function which is called
// each time some event in stack manager occures.
func (sm *StackManager) SetEventHandler(fn func(interface{})) {
	sm.emit = fn
}

// SetBucket sets the bucket name which is used
// for uploading stack templates.
func (sm *StackManager) SetBucket(bucket string) {
	sm.bucket = bucket
}

// List returns list of stacks.
func (sm *StackManager) List() ([]*StackData, error) {
	res := make([]*StackData, 0, len(sm.stacks))
	for _, name := range sm.stackOrder {
		res = append(res, sm.stacks[name].stackData())
	}

	return res, nil
}

// Execute executes the plan on the stack.
func (sm *StackManager) Execute(name string, planID string) (*StackData, error) {
	stack, _, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack '%s'", name)
	}
	changeSetID := (arn.ARN{
		Partition: "aws",
		Service:   "cloudformation",
		Region:    sm.awsClient.region,
		AccountID: sm.awsClient.accountID,
		Resource:  "changeSet/" + planID,
	}).String()
	err = stack.execute(&cfn.ChangeSetData{
		ID:        changeSetID,
		StackData: &stack.stackData().StackData,
	})
	return stack.stackData(), errors.Annotatef(err, "cannot execute change set '%s' for stack '%s'", changeSetID, name)
}

// Get returns stack data.
func (sm *StackManager) Get(name string) (*StackData, error) {
	stack, _, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack '%s'", name)
	}
	return stack.stackData(), nil
}

// Destroy destroys the stack.
func (sm *StackManager) Destroy(name string) (*StackData, error) {
	stack, _, err := sm.getStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack '%s'", name)
	}
	if err = stack.destroy(); err != nil {
		return nil, errors.Annotatef(err, "cannot destroy stack '%s'", name)
	}
	return stack.stackData(), nil
}

// SyncFiles synchronizes the files from local
// system to S3 bucket.
func (sm *StackManager) SyncFiles() error {
	for k, f := range sm.fileConfigs {
		config := s3file.Config{
			Region: sm.awsClient.region,
			Bucket: f.Bucket,
			Source: f.Src,
			Key:    f.Key,
		}
		if config.Bucket == "" {
			config.Bucket = sm.bucket
		}
		file, err := s3file.Write(sm.awsClient.s3conn, config)
		if err != nil {
			return errors.Annotatef(err, "cannot upload file '%s'", k)
		}
		sm.files[k] = file
	}
	return nil
}

// NewStackManager creates new instance of StackManager from config.
// It initialized AWS session, verifies account id and
// reads stacks statuses.
func NewStackManager(config Config) (*StackManager, error) {
	sm := &StackManager{
		config:       &config,
		name:         config.Name,
		stackOrder:   make([]string, 0, len(config.Stacks)),
		stacks:       make(map[string]*stack, len(config.Stacks)),
		stackConfigs: make(map[string]*StackConfig, len(config.Stacks)),
		emit:         func(interface{}) {},

		vars:  make(map[string]string, len(config.Variables)),
		files: make(map[string]*s3file.File, len(config.Files)),
	}
	awsClient, err := newAWSClient()
	if err != nil {
		return nil, errors.Annotatef(err, "cannot create new StackManager, aws error occurred")
	}

	if config.AccountID != "" && config.AccountID != awsClient.accountID {
		return nil, errors.Errorf("AccountID specified in config (%s) is not same as for AWS connection (%s)", config.AccountID, awsClient.accountID)
	}

	sm.awsClient = awsClient

	for k, v := range config.Variables {
		sm.vars[k] = v
	}
	sm.fileConfigs = config.Files

	for _, stackConfig := range config.Stacks {
		sm.stackOrder = append(sm.stackOrder, stackConfig.Name)
		if err = sm.addStack(stackConfig.Name, stackConfig); err != nil {
			return nil, errors.Annotatef(err, "cannot add %s stack", stackConfig.Name)
		}
	}

	return sm, nil
}
