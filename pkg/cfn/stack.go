package cfn

import (
	"time"

	"github.com/spirius/clon/pkg/closer"

	"github.com/juju/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
)

// Stack represents AWS CloudFormation
// stack resource.
type Stack struct {
	Name    string
	data    *StackData
	cfnconn cloudformationiface.CloudFormationAPI
}

// NewStack creates new Stack.
func NewStack(cfnconn cloudformationiface.CloudFormationAPI, name string) (*Stack, error) {
	stack := &Stack{Name: name, cfnconn: cfnconn}
	if err := stack.updateOnce(); err != nil {
		return nil, errors.Annotatef(err, "cannot create new stack")
	}
	return stack, nil
}

// read the stack. If stack is not found, nil is returned.
func (s *Stack) read() (*cloudformation.Stack, error) {
	out, err := s.cfnconn.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: aws.String(s.Name),
	})
	if err != nil {
		if e, ok := err.(awserr.Error); ok && e.Code() == "ValidationError" {
			return nil, nil
		}
		return nil, errors.Annotatef(err, "cannot read stack")
	}
	if out.Stacks == nil || len(out.Stacks) == 0 {
		return nil, nil
	}
	return out.Stacks[0], nil
}

func (s *Stack) update(config StackWaitConfig, interval time.Duration) error {
	for {
		cfnStack, err := s.read()
		if err != nil {
			return errors.Annotatef(err, "cannot read stack")
		}
		s.data = newStackData(s.Name, cfnStack)
		retry, err := config.Callback(s.data)
		if err != nil {
			return errors.Trace(err)
		} else if !retry {
			break
		}
		select {
		case <-time.After(interval):
		case <-config.Closer.Chan():
			return nil
		}
	}
	return nil
}

func (s *Stack) updateOnce() error {
	return errors.Trace(s.update(StackWaitConfig{
		Callback: func(*StackData) (bool, error) {
			return false, nil
		},
	}, 0))
}

// StackWaitFunc is the callback function type
// which is called to verify stack updates.
type StackWaitFunc func(*StackData) (again bool, err error)

// StackWaitConfig is the waiter configuration
// for stack.
type StackWaitConfig struct {
	// Callback is the function which is called
	// each time there is an update.
	Callback StackWaitFunc

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

// Wait function periodically reads stack data and
// invokes the config.Callback function.
// Waiter will stop if one of following hapenes:
//   * Error occurred when reading data
//   * Callback returns false
//   * Closer is closed
// CloseOnEnd and CloseOnError options indicate if
// Closer should be closed and if Closer should be closed
// when there is an error while reading.
func (s *Stack) Wait(config StackWaitConfig) {
	go func() {
		err := errors.Trace(s.update(StackWaitConfig{
			Callback: config.Callback,
			Closer:   config.Closer,
		}, 2*time.Second))
		if err != nil && config.CloseOnError {
			config.Closer.Close(errors.Trace(err))
			return
		}
		if config.CloseOnEnd {
			config.Closer.Close(nil)
		}
	}()
}

// Data returns the stack data.
func (s *Stack) Data() *StackData {
	if s.data == nil {
		s.data = newStackData(s.Name, nil)
	}
	return s.data
}

// Destroy invokes AWS CloudFormation DeleteStack API.
func (s *Stack) Destroy() error {
	_, err := s.cfnconn.DeleteStack(&cloudformation.DeleteStackInput{
		StackName: aws.String(s.Name),
	})
	return errors.Annotatef(err, "DeleteStack failed for stack '%s'", s.Name)
}
