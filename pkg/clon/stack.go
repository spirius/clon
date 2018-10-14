package clon

import (
	"fmt"
	"time"

	"github.com/spirius/clon/internal/pkg/cfn"
	"github.com/spirius/clon/internal/pkg/closer"

	"github.com/juju/errors"
)

// StackData represents the stack data.
type StackData struct {
	cfn.StackData
	ConfigName string
}

type stack struct {
	name       string
	configName string
	sm         *StackManager
	stack      *cfn.Stack
}

func newStack(sm *StackManager, stackName, configName string) (*stack, error) {
	cfnStack, err := cfn.NewStack(sm.awsClient.cfnconn, stackName)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot create new stack %s", stackName)
	}
	s := &stack{
		name:       stackName,
		configName: configName,
		stack:      cfnStack,
		sm:         sm,
	}
	return s, nil
}

func (s *stack) exists() bool {
	return s.stack == nil || s.stack.Data().Status == cfn.StackStatusNotFound
}

func (s *stack) stackData() *StackData {
	return s.newStackData(s.stack.Data())
}

func (s *stack) newStackData(data *cfn.StackData) *StackData {
	if data == nil {
		return nil
	}
	return &StackData{
		ConfigName: s.configName,
		StackData:  *data,
	}
}

func (s *stack) newChangeSetName() string {
	return fmt.Sprintf("%s-%s-%s", s.name, s.sm.awsClient.sessionName, time.Now().Format("20060102030405"))
}

func (s *stack) plan(stackData *StackData) (*cfn.ChangeSet, error) {
	csData := &cfn.ChangeSetData{
		Name:      s.newChangeSetName(),
		StackData: &stackData.StackData,
		IsNew:     !s.stack.Data().Exists() || s.stack.Data().IsReviewInProgress(),
	}

	cs, err := cfn.CreateChangeSet(s.sm.awsClient.cfnconn, csData)

	if err != nil {
		return nil, errors.Annotatef(err, "cannot create change set (%s)", csData.Name)
	}

	cl := closer.New()

	cs.Wait(cfn.ChangeSetWaitConfig{
		Callback: func(csData *cfn.ChangeSetData) (bool, error) {
			s.sm.emit(csData)
			if csData.IsFailed() {
				return false, errors.Trace(fmt.Errorf("cannot create change set"))
			} else if csData.IsInProgress() {
				return true, nil
			}
			return false, nil
		},
		Closer:       &cl,
		CloseOnError: true,
		CloseOnEnd:   true,
	})

	err = errors.Trace(cl.Wait())

	return cs, err
}

func (s *stack) trackUpdates(fn func(stack *cfn.StackData) (bool, error)) *closer.Closer {
	cl := closer.New()

	s.stack.Wait(cfn.StackWaitConfig{
		Callback: func(stack *cfn.StackData) (bool, error) {
			retry, err := fn(stack)
			if retry {
				s.sm.emit(s.newStackData(stack))
			}
			return retry, errors.Trace(err)
		},
		Closer:       &cl,
		CloseOnError: true,
		CloseOnEnd:   true,
	})

	se, err := cfn.NewStackEvents(s.sm.awsClient.cfnconn, s.name)
	if err != nil {
		return &cl
	}

	se.Wait(cfn.StackEventsWaitConfig{
		Callback: func(stackEvent *cfn.StackEventData) (bool, error) {
			s.sm.emit(stackEvent)
			return true, nil
		},
		Closer: &cl,
	})

	return &cl
}

func (s *stack) getChangeSet(csData *cfn.ChangeSetData) (*cfn.ChangeSet, error) {
	cs, err := cfn.NewChangeSet(s.sm.awsClient.cfnconn, csData)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return cs, nil
}

func (s *stack) execute(csData *cfn.ChangeSetData) (err error) {
	cs, err := cfn.NewChangeSet(s.sm.awsClient.cfnconn, csData)
	if err != nil {
		return errors.Trace(err)
	}

	if err = cs.Execute(); err != nil {
		return errors.Annotatef(err, "cannot execute change set '%s'", csData.Name)
	}

	cl := s.trackUpdates(func(stack *cfn.StackData) (bool, error) {
		if stack.IsInProgress() {
			return true, nil
		} else if stack.IsComplete() && !stack.IsRollback() {
			return false, nil
		}
		return false, errors.Errorf("stack '%s' has invlid status '%s'", stack.Name, stack.Status)
	})

	return errors.Trace(cl.Wait())
}

func (s *stack) destroy() error {
	err := s.stack.Destroy()
	if err != nil {
		return errors.Annotatef(err, "cannot destroy stack '%s'", s.name)
	}

	cl := s.trackUpdates(func(stack *cfn.StackData) (bool, error) {
		if stack.IsInProgress() {
			return true, nil
		} else if !stack.Exists() {
			return false, nil
		}
		return false, errors.Errorf("stack '%s' has invlid status '%s'", stack.Name, stack.Status)
	})

	return errors.Trace(cl.Wait())
}
