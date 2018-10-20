package cfn

import (
	"fmt"
	"sync"
	"testing"

	"github.com/spirius/clon/pkg/closer"

	"github.com/juju/errors"
	"github.com/stretchr/testify/require"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"

	mock "github.com/spirius/clon/pkg/cfn/mock"
)

func TestStackEvents_getEvents_basic(t *testing.T) {
	require := require.New(t)

	n := 42
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	var events []*cloudformation.StackEvent
	for i := 0; i < n; i++ {
		events = append(events, &cloudformation.StackEvent{
			EventId:   aws.String(fmt.Sprintf("%d", i)),
			StackName: aws.String(name),
		})
	}

	cfnconn.AddStackEvents(events)

	e := &StackEvents{cfnconn: cfnconn, name: name}

	res, err := e.getEvents()
	require.Nil(err)
	require.NotNil(res)
	require.Equal(n, len(res))

	for i := 0; i < n; i++ {
		require.Equal(fmt.Sprintf("%d", i), aws.StringValue(res[n-i-1].EventId))
	}
}

func TestStackEvents_getEvents_error(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.MockDescribeStackEvents = func(*cloudformation.DescribeStackEventsInput) (*cloudformation.DescribeStackEventsOutput, error) {
		return nil, fmt.Errorf("the error")
	}

	e := &StackEvents{cfnconn: cfnconn, name: name}

	res, err := e.getEvents()
	require.NotNil(err)
	require.Nil(res)
}

func TestStackEvents_NewStackEvents_basic(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	var events = [][]*cloudformation.StackEvent{
		{
			{EventId: aws.String("e3"), StackName: aws.String(name)},
			{EventId: aws.String("e2"), StackName: aws.String(name)},
			{EventId: aws.String("e1"), StackName: aws.String(name)},
		},
		{
			{EventId: aws.String("e5"), StackName: aws.String(name)},
			{EventId: aws.String("e4"), StackName: aws.String(name)},
		},
		{
			{EventId: aws.String("e8"), StackName: aws.String(name)},
			{EventId: aws.String("e7"), StackName: aws.String(name)},
			{EventId: aws.String("e6"), StackName: aws.String(name)},
		},
	}

	cfnconn.AddStackEvents(events[0])

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)
	require.Equal(se.last, "e3")

	cl := closer.New()
	n := 4

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		cl.Close(se.update(StackEventsWaitConfig{
			Closer: cl,
			Callback: func(d *StackEventData) (bool, error) {
				if d.EventID == "e5" {
					wg.Done()
				}
				require.Equal(fmt.Sprintf("e%d", n), d.EventID)
				n++
				if d.EventID == "e8" {
					return false, nil
				}
				return true, nil
			},
		}))
	}()

	cfnconn.AddStackEvents(events[1])
	wg.Wait()
	cfnconn.AddStackEvents(events[2])

	cl.Wait()
}

func TestStackEvents_NewStackEvents_errors(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	experr := fmt.Errorf("error")

	cfnconn.MockDescribeStackEvents = func(*cloudformation.DescribeStackEventsInput) (*cloudformation.DescribeStackEventsOutput, error) {
		return nil, experr
	}

	se, err := NewStackEvents(cfnconn, name)
	require.NotNil(err)
	require.Equal(experr, err.(*errors.Err).Cause())
	require.Nil(se)

}

func TestStackEvents_update_closer(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)
	require.Equal(se.last, "")

	cl := closer.New()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		se.update(StackEventsWaitConfig{
			Closer: cl,
			Callback: func(d *StackEventData) (bool, error) {
				return true, nil
			},
		})
		wg.Done()
	}()

	err = fmt.Errorf("error")
	cl.Close(err)
	wg.Wait()

	require.Equal(err, cl.Wait())
}

func TestStackEvents_update_error1(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)
	require.Equal(se.last, "")

	experr := fmt.Errorf("error")

	cfnconn.MockDescribeStackEvents = func(*cloudformation.DescribeStackEventsInput) (*cloudformation.DescribeStackEventsOutput, error) {
		return nil, experr
	}

	cl := closer.New()

	err = se.update(StackEventsWaitConfig{
		Closer: cl,
		Callback: func(d *StackEventData) (bool, error) {
			return true, nil
		},
	})

	require.Equal(experr, err.(*errors.Err).Cause())
}

func TestStackEvents_update_error2(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)
	require.Equal(se.last, "")

	experr := fmt.Errorf("error")

	cfnconn.AddStackEvents([]*cloudformation.StackEvent{{
		EventId:   aws.String("aa"),
		StackName: aws.String(name),
	}})

	cl := closer.New()
	err = se.update(StackEventsWaitConfig{
		Closer: cl,
		Callback: func(d *StackEventData) (bool, error) {
			return false, experr
		},
	})

	require.Equal(experr, err.(*errors.Err).Cause())
}

func TestStackEvents_Wait_closeOnEnd(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)

	cl := closer.New()
	se.Wait(StackEventsWaitConfig{
		Closer: cl,
		Callback: func(d *StackEventData) (bool, error) {
			return false, nil
		},
		CloseOnEnd: true,
	})

	cfnconn.AddStackEvents([]*cloudformation.StackEvent{{
		EventId:   aws.String("aa"),
		StackName: aws.String(name),
	}})

	require.Nil(cl.Wait())
}

func TestStackEvents_Wait_closeOnError(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	se, err := NewStackEvents(cfnconn, name)
	require.Nil(err)
	require.NotNil(se)

	experr := fmt.Errorf("error")

	cl := closer.New()
	se.Wait(StackEventsWaitConfig{
		Closer: cl,
		Callback: func(d *StackEventData) (bool, error) {
			return false, experr
		},
		CloseOnError: true,
	})

	cfnconn.AddStackEvents([]*cloudformation.StackEvent{{
		EventId:   aws.String("aa"),
		StackName: aws.String(name),
	}})

	require.Equal(experr, cl.Wait().(*errors.Err).Cause())
}

func TestStackEvents_newStackEventData(t *testing.T) {
	require := require.New(t)

	var (
		eventID              = "11111"
		logicalResourceID    = "22222"
		physicalResourceID   = "33333"
		resourceProperties   = "44444"
		resourceStatus       = "55555"
		resourceStatusReason = "66666"
		resourceType         = "77777"
		stackID              = "88888"
		stackName            = "99999"
	)

	se := newStackEventData(&cloudformation.StackEvent{
		EventId:              aws.String(eventID),
		LogicalResourceId:    aws.String(logicalResourceID),
		PhysicalResourceId:   aws.String(physicalResourceID),
		ResourceProperties:   aws.String(resourceProperties),
		ResourceStatus:       aws.String(resourceStatus),
		ResourceStatusReason: aws.String(resourceStatusReason),
		ResourceType:         aws.String(resourceType),
		StackId:              aws.String(stackID),
		StackName:            aws.String(stackName),
	})

	require.Equal(eventID, se.EventID)
	require.Equal(logicalResourceID, se.LogicalResourceID)
	require.Equal(physicalResourceID, se.PhysicalResourceID)
	require.Equal(resourceProperties, se.ResourceProperties)
	require.Equal(resourceStatus, se.ResourceStatus)
	require.Equal(resourceStatusReason, se.ResourceStatusReason)
	require.Equal(resourceType, se.ResourceType)
	require.Equal(stackID, se.StackID)
	require.Equal(stackName, se.StackName)
}
