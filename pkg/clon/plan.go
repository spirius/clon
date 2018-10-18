package clon

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/juju/errors"

	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/spirius/clon/internal/pkg/cfn"
)

// DiffString is helper type for tracking
// changes in string types.
type DiffString struct {
	old, new string
}

// String returns string representation of diff.
func (d DiffString) String() string {
	if d.IsEqual() {
		return strconv.Quote(d.old)
	}
	return fmt.Sprintf(`%s => %s`, strconv.Quote(d.old), strconv.Quote(d.new))
}

// IsEqual indicates if underlying strings are equal.
func (d DiffString) IsEqual() bool {
	return d.old == d.new
}

// DiffStringMap is map of string diffs.
type DiffStringMap map[string]DiffString

// HasChange indicates if there is a change in
// any of strings in underlying map.
func (d DiffStringMap) HasChange() bool {
	for _, diff := range d {
		if !diff.IsEqual() {
			return true
		}
	}
	return false
}

func newDiffStringMap(src map[string]string, dst map[string]string) DiffStringMap {
	res := make(map[string]DiffString)
	for k, v := range src {
		res[k] = DiffString{old: v}
	}
	for k, v := range dst {
		r, ok := res[k]
		if ok {
			r.new = v
		} else {
			r = DiffString{new: v}
		}
		res[k] = r
	}
	return res
}

// Plan represents the plan of changes on stack.
type Plan struct {
	ID        string
	ChangeSet *cfn.ChangeSetData
	Stack     *StackData

	RoleARN    DiffString
	Parameters DiffStringMap
	HasChange  bool
}

func newPlan(cs *cfn.ChangeSetData, stack *StackData) (*Plan, error) {
	csARN, err := arn.Parse(cs.ID)

	if err != nil {
		return nil, errors.Annotatef(err, "cannot parse change set id '%s'", cs.ID)
	}

	p := &Plan{
		ID:         strings.TrimPrefix(csARN.Resource, "changeSet/"),
		ChangeSet:  cs,
		Stack:      stack,
		RoleARN:    DiffString{stack.RoleARN, cs.StackData.RoleARN},
		Parameters: newDiffStringMap(stack.Parameters, cs.StackData.Parameters),
	}

	p.HasChange = p.Parameters.HasChange() || len(p.ChangeSet.Changes) > 0

	return p, nil
}
