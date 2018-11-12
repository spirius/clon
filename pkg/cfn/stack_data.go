package cfn

import (
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
)

// StackData is a data structure
// containing stack information.
type StackData struct {
	// ID is the resource id of the stack.
	// If stack does not exists, the value is empty string.
	ID string

	// Name of cloudformation stack. This field is always set.
	Name         string
	Description  string
	RoleARN      string
	Capabilities []string

	Parameters map[string]string
	Tags       map[string]string

	// Used for update only
	TemplateURL  string
	TemplateBody string

	// Set only after reading the stack.
	Status       string
	StatusReason string
	Outputs      map[string]string
}

// IsInProgress indicates if stack is currently
// being updated.
func (sd StackData) IsInProgress() bool {
	return strings.HasSuffix(sd.Status, "_IN_PROGRESS")
}

// IsReviewInProgress indicates if stack is in review process,
// (stack is first time created by change set).
func (sd StackData) IsReviewInProgress() bool {
	return sd.Status == cloudformation.StackStatusReviewInProgress
}

// IsComplete indicates if stack have completed last operation.
func (sd StackData) IsComplete() bool {
	return strings.HasSuffix(sd.Status, "_COMPLETE")
}

// IsFailed indicates if stack has failed last operation.
func (sd StackData) IsFailed() bool {
	return strings.HasSuffix(sd.Status, "_FAILED")
}

// IsRollback indicates if stack is in any rollback state.
func (sd StackData) IsRollback() bool {
	return strings.Contains(sd.Status, "_ROLLBACK_")
}

// Exists indicates if stack exists.
func (sd StackData) Exists() bool {
	return sd.Status != StackStatusNotFound
}

const (
	// StackStatusNotFound is the State of the stack
	// when stack does not exists.
	StackStatusNotFound = "STACK_NOT_FOUND"
)

// newStackData creates new Stack from cloudformation.Stack.
// If cloudformation stack doesn't exists, stack will have
// StackStatusNotFound status.
// Name field is always set.
func newStackData(stackName string, s *cloudformation.Stack) *StackData {
	if s == nil {
		return &StackData{
			Name:   stackName,
			Status: StackStatusNotFound,
		}
	}
	stackData := &StackData{}
	stackData.unmarshalStack(s)
	return stackData
}

func (sd *StackData) unmarshalParameters(params []*cloudformation.Parameter) {
	if sd.Parameters == nil {
		sd.Parameters = make(map[string]string)
	}
	for _, p := range params {
		sd.Parameters[aws.StringValue(p.ParameterKey)] = aws.StringValue(p.ParameterValue)
	}
}

func (sd *StackData) unmarshalTags(tags []*cloudformation.Tag) {
	if sd.Tags == nil {
		sd.Tags = make(map[string]string)
	}
	for _, t := range tags {
		sd.Tags[aws.StringValue(t.Key)] = aws.StringValue(t.Value)
	}
}

func (sd *StackData) unmarshalStack(s *cloudformation.Stack) {
	sd.RoleARN = aws.StringValue(s.RoleARN)
	sd.Name = aws.StringValue(s.StackName)
	sd.ID = aws.StringValue(s.StackId)
	sd.Status = aws.StringValue(s.StackStatus)
	sd.StatusReason = aws.StringValue(s.StackStatusReason)
	sd.Description = aws.StringValue(s.Description)
	sd.Capabilities = aws.StringValueSlice(s.Capabilities)

	if sd.Outputs == nil {
		sd.Outputs = make(map[string]string)
	}
	for _, o := range s.Outputs {
		sd.Outputs[aws.StringValue(o.OutputKey)] = aws.StringValue(o.OutputValue)
	}

	sd.unmarshalParameters(s.Parameters)
	sd.unmarshalTags(s.Tags)
}

func (sd *StackData) unmarshalDescribeChangeChangeSetOutput(cs *cloudformation.DescribeChangeSetOutput) {
	sd.ID = aws.StringValue(cs.StackId)
	sd.Name = aws.StringValue(cs.StackName)

	sd.Capabilities = aws.StringValueSlice(cs.Capabilities)
	sd.unmarshalParameters(cs.Parameters)
	sd.unmarshalTags(cs.Tags)
}

func (sd StackData) marshalCreateChangeSetInput(s *cloudformation.CreateChangeSetInput) {
	if sd.ID != "" {
		s.StackName = aws.String(sd.ID)
	} else if sd.Name != "" {
		s.StackName = aws.String(sd.Name)
	}
	if sd.RoleARN != "" {
		s.RoleARN = aws.String(sd.RoleARN)
	}
	if sd.Description != "" {
		s.Description = aws.String(sd.Description)
	}
	s.Capabilities = aws.StringSlice(sd.Capabilities)

	for k, v := range sd.Parameters {
		s.Parameters = append(s.Parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String(k),
			ParameterValue: aws.String(v),
		})
	}

	for k, v := range sd.Tags {
		s.Tags = append(s.Tags, &cloudformation.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	if sd.TemplateURL != "" {
		s.TemplateURL = aws.String(sd.TemplateURL)
	} else if sd.TemplateBody != "" {
		s.TemplateBody = aws.String(sd.TemplateBody)
	} else {
		s.UsePreviousTemplate = aws.Bool(true)
	}
}
