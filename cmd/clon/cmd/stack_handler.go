package cmd

import (
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"
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
	config.Stacks = append([]clon.StackConfig{config.Bootstrap}, config.Stacks...)
	config.RootStack = bootstrapStackName
	sm, err := clon.NewStackManager(config)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot create clon")
	}
	sm.SetEventHandler(s.eventHandler)
	sm.SetVerify(s.verifyStack)
	s.sm = sm
	return s, nil
}

func (s *stackCmdHandler) verifyStack(name string) error {
	log := log.WithFields(log.Fields{"stack": name})
	if !configFlags.verifyParentStacks {
		log.Info("skipping parent stack verification")
		return nil
	}
	log.Info("verifying parent stack")
	stack, updated, err := s.deployStack(name)
	if err != nil {
		return errors.Annotatef(err, "cannot verify parent stack '%s', deploy failed", name)
	}
	if updated {
		log.Info("stack updated")
		newOutput(stack).Output(stderr)
	} else {
		log.Info("parent stack does not contain changes")
	}
	return nil
}

// eventHandler outputs information about updates emitted from
// cloudformation updates.
func (s *stackCmdHandler) eventHandler(event interface{}) {
	newOutput(event).StatusLine().Output(stderr)
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

func (s *stackCmdHandler) deployStack(name string) (*clon.StackData, bool, error) {
	log := log.WithFields(log.Fields{"stack": name})
	plan, err := s.sm.Plan(name)
	if err != nil {
		return nil, false, errors.Annotatef(err, "cannot plan stack '%s'", name)
	}
	stack := plan.Stack
	if plan.HasChange {
		newOutput(plan).Output(stderr)
		if err = askForConfirmation("Do you want to apply these changes on stack?"); err != nil {
			return nil, false, errors.Annotatef(err, "changes are not approved")
		}
		log.Infof("changes approved, starting plan execution for stack %s", name)
		stack, err = s.sm.Execute(name, plan.ID)
		if err != nil {
			return nil, false, errors.Annotatef(err, "execution of stack '%s' failed", name)
		}
	}
	return stack, plan.HasChange, nil
}

func (s *stackCmdHandler) deploy(name string) (output, error) {
	if name != bootstrapStackName {
		_, err := s.init()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot deploy stack, init failed")
		}
	}
	stack, _, err := s.deployStack(name)
	if err != nil {
		return nil, errors.Annotatef(err, "deployment of stack '%s' failed", err)
	}
	return newOutput(stack), nil
}

// init initialized the stack, which is equivavlent of planing and
// if needed deploying the bootstrap stack.
func (s *stackCmdHandler) init() (output, error) {
	stack, hasChange, err := s.deployStack(bootstrapStackName)
	if err != nil {
		return nil, errors.Annotatef(err, "initialization failed")
	}

	if hasChange {
		newOutput(stack).Output(stderr)
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

func (s *stackCmdHandler) verifyStackName(name string) error {
	_, err := s.sm.Get(name)
	return err
}

func (s *stackCmdHandler) plan(name string) (output, error) {
	log := log.WithFields(log.Fields{"stack": name})
	log.Info("planning stack")
	err := s.verifyStackName(name)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get stack")
	}
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
	newOutput(plan).Output(stderr)
	code := 0
	if plan.HasChange {
		code = 2
	}

	if code == 0 {
		log.Info("stack does not contain changes")
	}

	return newOutput(plan).Short(), &errorCode{nil, code}
}

func (s *stackCmdHandler) planStatus(name, planID string) (output, error) {
	plan, err := s.sm.GetPlan(name, planID)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get plan '%s', stack '%s'", planID, name)
	}
	newOutput(plan).Output(stderr)
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
	stackStatus.Output(stderr)

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
	plan, err := s.sm.GetPlan(name, planID)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot get plan '%s' for stack '%s'", planID, name)
	}
	newOutput(plan).Output(stderr)
	stack, err := s.sm.Execute(name, planID)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot execute plan '%s' on stack '%s'", planID, name)
	}
	return newOutput(stack), nil
}
