package cmd

import (
	"os"

	"github.com/juju/errors"
	"github.com/spirius/clon/pkg/clon"
)

const bootstrapStackName = "bootstrap"

type stackCmdHandler struct {
	sm *clon.StackManager
}

// newStackCmdHandler creates new stackCmdHandler from config.
func newStackCmdHandler(config clon.Config) (*stackCmdHandler, error) {
	s := &stackCmdHandler{}
	config.Bootstrap.Name = bootstrapStackName
	config.Stacks = append(config.Stacks, config.Bootstrap)
	sm, err := clon.NewStackManager(config)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot create clon")
	}
	sm.SetEventHandler(s.eventHandler)
	s.sm = sm
	return s, nil
}

// eventHandler outputs information about updates emitted from
// cloudformation updates.
func (s *stackCmdHandler) eventHandler(event interface{}) {
	newOutput(event).StatusLine().Output(os.Stderr)
}

func (s *stackCmdHandler) list() ([]output, error) {
	list, err := s.sm.List()
	if err != nil {
		return nil, errors.Annotatef(err, "cannot list stacks")
	}
	res := make([]output, 0, len(list))
	for _, stack := range list {
		res = append(res, newOutput(stack).Short())
	}
	return res, nil
}

func (s *stackCmdHandler) status(name string) (output, error) {
	stack, err := s.sm.Get(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot read stack")
	}
	return newOutput(stack), nil
}

func (s *stackCmdHandler) deployStack(name string) (*clon.StackData, error) {
	plan, err := s.sm.Plan(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot plan stack '%s'", name)
	}
	stack := plan.Stack
	if plan.HasChange {
		newOutput(plan).Output(os.Stderr)
		if err = askForConfirmation("Do you want to apply these changes on stack?"); err != nil {
			return nil, errors.Annotatef(err, "changes are not approved")
		}
		stack, err = s.sm.Execute(name, plan.ID)
		if stack != nil {
			newOutput(stack).Output(os.Stderr)
		}
		if err != nil {
			return nil, errors.Annotatef(err, "execution of stack '%s' failed", name)
		}
	}
	return stack, nil
}

func (s *stackCmdHandler) deploy(name string) (output, error) {
	if name != bootstrapStackName {
		s.init()
	}
	stack, err := s.deployStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "deployment of stack '%s' failed", err)
	}
	return newOutput(stack), nil
}

// init initialized the stack, which is equivavlent of planing and
// if needed deploying the bootstrap stack.
func (s *stackCmdHandler) init() (output, error) {
	stack, err := s.deployStack(bootstrapStackName)
	if err != nil {
		return nil, errors.Annotatef(err, "initialization failed")
	}

	bucket, ok := stack.Outputs["Bucket"]
	if !ok {
		return newOutput(stack), errors.Annotatef(err, "bootstrap stack must have 'Bucket' in outputs")
	}

	s.sm.SetBucket(bucket)

	if err = s.sm.SyncFiles(); err != nil {
		return nil, errors.Annotatef(err, "cannot sync files")
	}

	return newOutput(stack), nil
}

func (s *stackCmdHandler) plan(name string) (output, error) {
	if name != bootstrapStackName {
		_, err := s.init()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot initialize")
		}
	}

	plan, err := s.sm.Plan(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot plan stack '%s'", name)
	}
	newOutput(plan).Output(os.Stderr)
	code := 0
	if plan.HasChange {
		code = 2
	}

	return newOutput(plan).Short(), &errorCode{nil, code}
}

func (s *stackCmdHandler) planStatus(name, planID string) (output, error) {
	plan, err := s.sm.GetPlan(name, planID)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get plan '%s', stack '%s'", planID, name)
	}
	newOutput(plan).Output(os.Stderr)
	code := 0
	if plan.HasChange {
		code = 2
	}
	return newOutput(plan).Short(), &errorCode{nil, code}
}

func (s *stackCmdHandler) destroy(name string) (output, error) {
	stackStatus, err := s.status(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack '%s'", name)
	}
	stackStatus.Output(os.Stderr)

	if err = askForConfirmation("Are you sure you want to destroy this stack?"); err != nil {
		return nil, errors.Annotatef(err, "changes are not approved")
	}

	stack, err := s.sm.Destroy(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot destroy stack '%s'", name)
	}

	return newOutput(stack), nil
}

func (s *stackCmdHandler) execute(name, planID string) (output, error) {
	stack, err := s.sm.Execute(name, planID)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot execute plan '%s' on stack '%s'", planID, name)
	}
	return newOutput(stack), nil
}
