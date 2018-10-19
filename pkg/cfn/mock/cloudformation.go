package cfn

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
)

// MockCloudFormationAPI is the mock for AWS CloudFormation API.
type MockCloudFormationAPI struct {
	cloudformationiface.CloudFormationAPI

	stackEventsLock sync.Mutex
	stackEvents     []*cloudformation.StackEvent

	stacksLock sync.Mutex
	stacks     map[string]*cloudformation.Stack

	changeSetsLock sync.Mutex
	changeSets     map[string]map[string]*cloudformation.DescribeChangeSetOutput

	// PageSize is the page size for returned data.
	PageSize int

	// MockDescribeStackEvents can be used to mock the call to DescribeStackEvents API.
	MockDescribeStackEvents func(*cloudformation.DescribeStackEventsInput) (*cloudformation.DescribeStackEventsOutput, error)

	// MockDescribeStacks can be used to mock the call to DescribeStacks API.
	MockDescribeStacks func(*cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error)

	// MockDescribeChangeSet can be used to mock the call to DescribeChangeSet API.
	MockDescribeChangeSet func(*cloudformation.DescribeChangeSetInput) (*cloudformation.DescribeChangeSetOutput, error)

	// MockDeleteStack can be used to mock the call to DeleteStack API.
	MockDeleteStack func(*cloudformation.DeleteStackInput) (*cloudformation.DeleteStackOutput, error)
}

// NewMockCloudFormationAPI creates new mock of CloudFormation API.
func NewMockCloudFormationAPI() *MockCloudFormationAPI {
	return &MockCloudFormationAPI{
		PageSize:   10,
		stacks:     make(map[string]*cloudformation.Stack),
		changeSets: make(map[string]map[string]*cloudformation.DescribeChangeSetOutput),
	}
}

// DescribeStackEvents invokes the mock method if it is set,
// otherwise it will return stack events from default mock implementation.
func (c *MockCloudFormationAPI) DescribeStackEvents(in *cloudformation.DescribeStackEventsInput) (*cloudformation.DescribeStackEventsOutput, error) {
	if c.MockDescribeStackEvents != nil {
		return c.MockDescribeStackEvents(in)
	}
	c.stackEventsLock.Lock()
	defer c.stackEventsLock.Unlock()
	var stackEvents []*cloudformation.StackEvent
	if in.StackName != nil {
		for _, e := range c.stackEvents {
			if strPtrCmp(e.StackName, in.StackName) || strPtrCmp(e.StackId, in.StackName) {
				stackEvents = append(stackEvents, e)
			}
		}
	} else {
		stackEvents = c.stackEvents
	}

	out := &cloudformation.DescribeStackEventsOutput{}

	start := 0
	if in.NextToken != nil {
		start, _ = strconv.Atoi(aws.StringValue(in.NextToken))
	}
	if start+c.PageSize < len(stackEvents) {
		out.NextToken = aws.String(fmt.Sprintf("%d", start+c.PageSize))
		out.StackEvents = stackEvents[start : start+c.PageSize]
	} else if start < len(stackEvents) {
		out.StackEvents = stackEvents[start:]
	}
	return out, nil
}

// AddStackEvents adds new stack events to mock implementation.
func (c *MockCloudFormationAPI) AddStackEvents(stackEvents []*cloudformation.StackEvent) {
	c.stackEventsLock.Lock()
	defer c.stackEventsLock.Unlock()
	c.stackEvents = append(stackEvents, c.stackEvents...)
}

func strPtrCmp(a, b *string) bool {
	if b == nil {
		return true
	} else if a == nil {
		return false
	}
	return *a == *b
}

func stackEventCmp(a, b *cloudformation.StackEvent) bool {
	return strPtrCmp(a.EventId, b.EventId) &&
		strPtrCmp(a.LogicalResourceId, b.LogicalResourceId) &&
		strPtrCmp(a.ResourceType, b.ResourceType) &&
		strPtrCmp(a.StackName, b.StackName)
}

// RemoveStackEvents removes the stack events from mock implementation.
func (c *MockCloudFormationAPI) RemoveStackEvents(stackEvents []*cloudformation.StackEvent) {
	for _, e1 := range stackEvents {
		for k, e2 := range c.stackEvents {
			if stackEventCmp(e2, e1) {
				c.stackEvents = append(c.stackEvents[:k], c.stackEvents[k+1:]...)
			}
		}
	}
}

// AddStacks adds new stack to mock implementation.
func (c *MockCloudFormationAPI) AddStacks(stacks []*cloudformation.Stack) {
	c.stacksLock.Lock()
	defer c.stacksLock.Unlock()
	for _, s := range stacks {
		c.stacks[aws.StringValue(s.StackName)] = s
	}
}

// DescribeStacks invokes the mocked method if is is set,
// otherwise it will return stack from default implementation.
func (c *MockCloudFormationAPI) DescribeStacks(in *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
	if c.MockDescribeStacks != nil {
		return c.MockDescribeStacks(in)
	}
	if in.StackName != nil {
		c.stacksLock.Lock()
		defer c.stacksLock.Unlock()
		stackName := normalizeStackName(aws.StringValue(in.StackName))
		stack := c.stacks[stackName]
		if stack == nil {
			return nil, awserr.New("ValidationError", fmt.Sprintf("Stack with id %s does not exist", stackName), nil)
		}
		return &cloudformation.DescribeStacksOutput{
			Stacks: []*cloudformation.Stack{stack},
		}, nil
	}
	return nil, fmt.Errorf("Mock is not implemented")
}

func normalizeStackName(name string) string {
	stackARN, err := arn.Parse(name)
	if err == nil {
		name = strings.SplitAfter(strings.TrimPrefix(stackARN.Resource, "stack/"), "/")[0]
	}
	return name
}

func normalizeChangeSetName(name string) string {
	changeSetARN, err := arn.Parse(name)
	if err == nil {
		name = strings.SplitAfter(strings.TrimPrefix(changeSetARN.Resource, "changeSet/"), "/")[0]
	}
	return name
}

func (c *MockCloudFormationAPI) getStack(name string) *cloudformation.Stack {
	return c.stacks[normalizeStackName(name)]
}

// DeleteStack invokes mocked method if it is not nil,
// otherwise the mocked implementation is invoked.
func (c *MockCloudFormationAPI) DeleteStack(in *cloudformation.DeleteStackInput) (*cloudformation.DeleteStackOutput, error) {
	if c.MockDeleteStack != nil {
		return c.MockDeleteStack(in)
	}
	c.stacksLock.Lock()
	defer c.stacksLock.Unlock()
	stack := c.getStack(aws.StringValue(in.StackName))
	if stack == nil {
		return nil, nil
	}
	delete(c.stacks, aws.StringValue(stack.StackName))
	return &cloudformation.DeleteStackOutput{}, nil
}

// AddChangeSets adds new change sets to default mock implementation.
func (c *MockCloudFormationAPI) AddChangeSets(changeSets []*cloudformation.DescribeChangeSetOutput) {
	c.changeSetsLock.Lock()
	defer c.changeSetsLock.Unlock()
	for _, cs := range changeSets {
		stackName := aws.StringValue(cs.StackName)
		csName := aws.StringValue(cs.ChangeSetName)
		if _, ok := c.changeSets[stackName]; !ok {
			c.changeSets[stackName] = make(map[string]*cloudformation.DescribeChangeSetOutput)
		}
		c.changeSets[stackName][csName] = cs
	}
}

// DescribeChangeSet invokes mocked method if it is not nil,
// otherwise the mocked implementation is invoked.
func (c *MockCloudFormationAPI) DescribeChangeSet(in *cloudformation.DescribeChangeSetInput) (*cloudformation.DescribeChangeSetOutput, error) {
	if c.MockDescribeChangeSet != nil {
		return c.MockDescribeChangeSet(in)
	}
	stackName := normalizeStackName(aws.StringValue(in.StackName))
	csName := normalizeChangeSetName(aws.StringValue(in.ChangeSetName))

	c.changeSetsLock.Lock()
	defer c.changeSetsLock.Unlock()

	css, ok := c.changeSets[stackName]
	if !ok {
		return nil, awserr.New("ValidationError", fmt.Sprintf("Stack [%s] does not exist", stackName), nil)
	}
	cs, ok := css[csName]
	if !ok {
		return nil, awserr.New("ChangeSetNotFound", fmt.Sprintf("ChangeSet [%s] does not exist", csName), nil)

	}
	out := *cs
	start := 0
	if in.NextToken != nil {
		start, _ = strconv.Atoi(aws.StringValue(in.NextToken))
	}
	if start+c.PageSize < len(out.Changes) {
		out.NextToken = aws.String(fmt.Sprintf("%d", start+c.PageSize))
		out.Changes = cs.Changes[start : start+c.PageSize]
	} else if start < len(out.Changes) {
		out.Changes = cs.Changes[start:]
	}
	return &out, nil
}
