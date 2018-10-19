package cfn

import (
	"time"

	"github.com/spirius/clon/pkg/closer"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/juju/errors"
)

// ChangeSet represents that AWS CloudFormation
// change set resoruce.
type ChangeSet struct {
	id        string
	name      string
	stackName string
	cfnconn   cloudformationiface.CloudFormationAPI
	data      *ChangeSetData
}

func (cs *ChangeSet) newChangeSetData(in *cloudformation.DescribeChangeSetOutput) *ChangeSetData {
	if in == nil {
		return &ChangeSetData{
			ID:        cs.id,
			Name:      cs.name,
			Status:    ChangeSetStatusNotFound,
			StackData: &StackData{Name: cs.stackName},
		}
	}
	c := &ChangeSetData{
		ID:              aws.StringValue(in.ChangeSetId),
		Name:            aws.StringValue(in.ChangeSetName),
		ExecutionStatus: aws.StringValue(in.ExecutionStatus),
		Status:          aws.StringValue(in.Status),
		StatusReason:    aws.StringValue(in.StatusReason),
		StackData:       &StackData{},
		Changes:         make([]*cloudformation.ResourceChange, 0, len(in.Changes)),
	}

	for _, change := range in.Changes {
		c.Changes = append(c.Changes, change.ResourceChange)
	}

	c.StackData.unmarshalDescribeChangeChangeSetOutput(in)

	return c
}

// NewChangeSet creates new ChangeSet object from existing
// AWS CloudFormation changeset.
func NewChangeSet(conn cloudformationiface.CloudFormationAPI, csData *ChangeSetData) (*ChangeSet, error) {
	if csData.ID == "" && (csData.Name == "" || csData.StackData == nil) {
		return nil, errors.Errorf("neither change set id nor change set and stack names are set")
	}
	cs := &ChangeSet{
		cfnconn: conn,
		data:    csData,
		id:      csData.ID,
		name:    csData.Name,
	}
	if csData.StackData != nil {
		cs.stackName = csData.StackData.Name
	}
	if err := cs.updateOnce(); err != nil {
		return nil, errors.Trace(err)
	}
	return cs, nil
}

// CreateChangeSet creates new ChangeSet described by
// csData argument.
func CreateChangeSet(conn cloudformationiface.CloudFormationAPI, csData *ChangeSetData) (*ChangeSet, error) {
	cs := &ChangeSet{
		cfnconn:   conn,
		name:      csData.Name,
		stackName: csData.StackData.Name,
	}
	in := &cloudformation.CreateChangeSetInput{}
	csData.StackData.marshalCreateChangeSetInput(in)

	if csData.IsNew {
		in.ChangeSetType = aws.String(cloudformation.ChangeSetTypeCreate)
	} else {
		in.ChangeSetType = aws.String(cloudformation.ChangeSetTypeUpdate)
	}

	in.ChangeSetName = aws.String(csData.Name)
	out, err := conn.CreateChangeSet(in)
	if err != nil {
		return nil, errors.Annotatef(err, "CreateChangeSet failed")
	}

	cs.id = aws.StringValue(out.Id)

	return cs, nil
}

func (cs *ChangeSet) update(config ChangeSetWaitConfig, interval time.Duration) error {
	var (
		csData, newData *ChangeSetData
		err             error
		out             *cloudformation.DescribeChangeSetOutput
		retry           bool
	)
	in := &cloudformation.DescribeChangeSetInput{
		ChangeSetName: aws.String(cs.id),
	}
	if cs.id != "" {
		in.ChangeSetName = aws.String(cs.id)
	} else if cs.name != "" && cs.stackName != "" {
		in.ChangeSetName = aws.String(cs.name)
		in.StackName = aws.String(cs.stackName)
	} else {
		return errors.Errorf("neither change set id nor change set and stack names are set")
	}

loop:
	for {
		// get new data
		out, err = cs.cfnconn.DescribeChangeSet(in)
		if err != nil {
			if e, ok := err.(awserr.RequestFailure); ok && e.Code() == cloudformation.ErrCodeChangeSetNotFoundException {
				// not found
				out = nil
			} else {
				err = errors.Annotatef(err, "cannot describe change set (%s)", cs.id)
				break
			}
		}

		newData = cs.newChangeSetData(out)
		if csData != nil {
			csData.Changes = append(csData.Changes, newData.Changes...)
		} else {
			csData = newData
		}

		retry, err = config.Callback(csData)
		if err != nil {
			err = errors.Trace(err)
			break
		} else if retry {
			select {
			case <-time.After(interval):
			case <-config.Closer.Chan():
				break loop
			}
			csData = nil
			in.NextToken = nil
			continue
		}

		if out == nil || out.NextToken == nil {
			break
		}
		in.NextToken = out.NextToken
	}
	if csData == nil {
		csData = cs.newChangeSetData(nil)
	}
	cs.data = csData
	cs.name = csData.Name

	return err
}

// ChangeSetWaitFunc is the callback function type
// which is called to verify change set updates.
type ChangeSetWaitFunc func(*ChangeSetData) (again bool, err error)

// ChangeSetWaitConfig is the waiter configuration
// for change set.
type ChangeSetWaitConfig struct {
	// Callback is the function which is called
	// each time there is an update.
	Callback ChangeSetWaitFunc

	// Closer is used to stop waiting when
	// it is closed or close it depending
	// on values of CloseOnEnd and CloseOnError
	Closer *closer.Closer

	// CloseOnEnd is an option to close the Closer
	// when waiter finishes.
	CloseOnEnd bool

	// CloseOnError is an option to close the Closer
	// in case of error.
	CloseOnError bool
}

// Wait function periodically reads change set data and
// invokes the config.Callback function.
// Waiter will stop if one of following hapenes:
//   * Error occurred when reading data
//   * Callback returns false
//   * Closer is closed
// CloseOnEnd and CloseOnError options indicate if
// Closer should be closed and if Closer should be closed
// when there is an error while reading.
func (cs *ChangeSet) Wait(config ChangeSetWaitConfig) {
	go func() {
		err := cs.update(config, 2*time.Second)
		if err != nil && config.CloseOnError {
			config.Closer.Close(errors.Trace(err))
			return
		}
		if config.CloseOnEnd {
			config.Closer.Close(nil)
		}
	}()
}

// Data returns the change set data.
func (cs *ChangeSet) Data() *ChangeSetData {
	if cs.data == nil {
		cs.data = cs.newChangeSetData(nil)
	}
	return cs.data
}

// Execute invokes AWS CloudFormation ExecuteChangeSet
// API.
func (cs *ChangeSet) Execute() error {
	in := &cloudformation.ExecuteChangeSetInput{}

	if cs.id != "" {
		in.ChangeSetName = aws.String(cs.id)
	} else if cs.name != "" && cs.stackName != "" {
		in.ChangeSetName = aws.String(cs.name)
		in.StackName = aws.String(cs.stackName)
	} else {
		return errors.Errorf("neither change set id nor change set and stack names are set")
	}

	_, err := cs.cfnconn.ExecuteChangeSet(in)
	return errors.Trace(err)
}

func (cs *ChangeSet) updateOnce() error {
	return errors.Trace(cs.update(ChangeSetWaitConfig{
		Callback: func(*ChangeSetData) (bool, error) {
			return false, nil
		},
	}, 0))
}
