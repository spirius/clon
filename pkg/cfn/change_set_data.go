package cfn

import (
	"github.com/aws/aws-sdk-go/service/cloudformation"
)

const (
	// ChangeSetStatusNotFound is the State of change set,
	// when change set is not found.
	ChangeSetStatusNotFound = "CHANGE_SET_NOT_FOUND"
)

// ChangeSetData is the data structure
// containing change set information.
type ChangeSetData struct {
	ID              string
	Name            string
	Status          string
	StatusReason    string
	ExecutionStatus string
	StackData       *StackData

	IsNew   bool
	Changes []*cloudformation.ResourceChange
}

// IsInProgress indicates if change set is currently
// being updated.
func (c ChangeSetData) IsInProgress() bool {
	return c.Status == cloudformation.ChangeSetStatusCreatePending ||
		c.Status == cloudformation.ChangeSetStatusCreateInProgress
}

// IsComplete indicates if change set is in
// completed state.
func (c ChangeSetData) IsComplete() bool {
	return c.Status == cloudformation.ChangeSetStatusCreateComplete
}

// IsFailed indicates if change set is in
// failed state. Note, that if change set doesn't contain
// any changes, false is returned.
func (c ChangeSetData) IsFailed() bool {
	if c.Status == cloudformation.ChangeSetStatusFailed && c.StatusReason == `The submitted information didn't contain changes. Submit different information to create a change set.` {
		return false
	}
	return c.Status == cloudformation.ChangeSetStatusFailed
}

// Exists indicates if change set exists.
func (c ChangeSetData) Exists() bool {
	return c.Status != ChangeSetStatusNotFound
}

// IsExecutable indicates if change set can be executed.
func (c ChangeSetData) IsExecutable() bool {
	return c.ExecutionStatus == cloudformation.ExecutionStatusAvailable
}
