package clon

import (
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spirius/clon/pkg/cfn"
	"github.com/spirius/clon/pkg/closer"

	"github.com/juju/errors"
)

const awsCloudFormationStack = "AWS::CloudFormation::Stack"

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

	nestedStackLock     sync.Mutex
	nestedStackTracking map[string]*closer.Closer

	planned   bool
	hasChange bool
	updated   bool

	children map[string]*stack
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

		nestedStackTracking: make(map[string]*closer.Closer),
		children:            make(map[string]*stack),
	}
	return s, nil
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

func (s *stack) addChild(child *stack) error {
	if _, ok := s.children[child.configName]; ok {
		return nil
	}
	chain := child.isChild(s.configName)
	if chain != nil {
		chain = append(chain, s.configName)
		return errors.Errorf("cyclic dependency between stacks: %s", strings.Join(chain, " -> "))
	}
	s.children[child.configName] = child
	return nil
}

func (s *stack) isChild(name string) []string {
	for n, c := range s.children {
		if n == name {
			return []string{c.configName, s.configName}
		}
		r := c.isChild(name)
		if r != nil {
			return append(r, s.configName)
		}
	}
	return nil
}

// plan create new change set on stack and waits until it finishes.
// It might return both changeSet object and error in case if error
// occurred while waiting.
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
		Closer:       cl,
		CloseOnError: true,
		CloseOnEnd:   true,
	})

	err = errors.Trace(cl.Wait())

	return cs, err
}

func (s *stack) verifyNestedStackTracking(e *cfn.StackEventData, parentCl *closer.Closer) {
	stackID := e.PhysicalResourceID

	if stackID == "" || e.LogicalResourceID == s.name || e.StackID == s.name {
		return
	}

	s.nestedStackLock.Lock()
	defer s.nestedStackLock.Unlock()
	cl, ok := s.nestedStackTracking[stackID]

	if e.IsComplete() {
		if ok {
			log.Debugf("removing nested stack tracking for stack '%s'", stackID)
			cl.Close(nil)
			delete(s.nestedStackTracking, stackID)
		}
	}

	if !ok {
		log.Debugf("adding nested stack tracking for stack '%s'", stackID)
		cl := parentCl.Child()
		s.nestedStackTracking[stackID] = cl
		err := s.trackStackEvents(stackID, cl)
		if err != nil {
			log.Errorf("nested stack '%s' tracking failed: %s", stackID, err)
		}
	}
}

func (s *stack) trackStackEvents(name string, cl *closer.Closer) error {
	log.Debugf("starting stack events tracking for stack '%s'", name)
	se, err := cfn.NewStackEvents(s.sm.awsClient.cfnconn, name)

	if err != nil {
		return errors.Annotatef(err, "cannot track '%s'", name)
	}

	se.Wait(cfn.StackEventsWaitConfig{
		Callback: func(stackEvent *cfn.StackEventData) (bool, error) {
			if stackEvent.ResourceType == awsCloudFormationStack {
				s.verifyNestedStackTracking(stackEvent, cl)
			}
			s.sm.emit(stackEvent)
			return true, nil
		},
		Closer: cl,
	})

	return nil
}

func (s *stack) trackUpdates(fn func(stack *cfn.StackData) (bool, error)) *closer.Closer {
	log.Debugf("starting stack update tracking for stack '%s'", s.name)
	cl := closer.New()

	lastStatus := ""
	s.stack.Wait(cfn.StackWaitConfig{
		Callback: func(stack *cfn.StackData) (bool, error) {
			log.Debugf("received update for stack '%s', status %s", stack.Name, stack.Status)
			retry, err := fn(stack)
			if retry {
				if stack.Status != lastStatus {
					s.sm.emit(s.newStackData(stack))
					lastStatus = stack.Status
				}
			}
			return retry, errors.Trace(err)
		},
		Closer:       cl,
		CloseOnError: true,
		CloseOnEnd:   true,
	})

	err := s.trackStackEvents(s.name, cl)
	if err != nil {
		log.Errorf("cannot track stack events: %s", err)
	}

	return cl
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
