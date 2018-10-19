package cfn

import (
	"strings"
	"time"

	"github.com/spirius/clon/pkg/closer"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/juju/errors"
	log "github.com/sirupsen/logrus"
)

const stackEventsWaitInterval = 2 * time.Second

// StackEventData is the data structure
// containing stack event information.
type StackEventData struct {
	EventID              string
	LogicalResourceID    string
	PhysicalResourceID   string
	ResourceProperties   string
	ResourceStatus       string
	ResourceStatusReason string
	ResourceType         string
	StackID              string
	StackName            string
}

// IsComplete indicates if resource in event is in
// completed state.
func (c StackEventData) IsComplete() bool {
	return strings.HasSuffix(c.ResourceStatus, "_COMPLETE")
}

func newStackEventData(in *cloudformation.StackEvent) *StackEventData {
	return &StackEventData{
		EventID:              aws.StringValue(in.EventId),
		LogicalResourceID:    aws.StringValue(in.LogicalResourceId),
		PhysicalResourceID:   aws.StringValue(in.PhysicalResourceId),
		ResourceProperties:   aws.StringValue(in.ResourceProperties),
		ResourceStatus:       aws.StringValue(in.ResourceStatus),
		ResourceStatusReason: aws.StringValue(in.ResourceStatusReason),
		ResourceType:         aws.StringValue(in.ResourceType),
		StackID:              aws.StringValue(in.StackId),
		StackName:            aws.StringValue(in.StackName),
	}
}

// StackEvents tracks events on single stack.
// It keeps internal identifier on last seen event and
// will notify only new events.
type StackEvents struct {
	name    string
	last    string
	cfnconn cloudformationiface.CloudFormationAPI
}

// NewStackEvents creates new StackEvents and moves the last event
// identifier to the last event of currently available events.
func NewStackEvents(cfnconn cloudformationiface.CloudFormationAPI, name string) (*StackEvents, error) {
	se := &StackEvents{
		name:    name,
		cfnconn: cfnconn,
	}
	events, err := se.getEvents()
	if err != nil {
		return nil, errors.Annotatef(err, "cannot initialize stack events")
	}
	if len(events) > 0 {
		se.last = aws.StringValue(events[len(events)-1].EventId)
	}
	return se, nil
}

func (se *StackEvents) getEvents() ([]*cloudformation.StackEvent, error) {
	in := &cloudformation.DescribeStackEventsInput{
		StackName: aws.String(se.name),
	}
	events := make([]*cloudformation.StackEvent, 0)
	for {
		out, err := se.cfnconn.DescribeStackEvents(in)
		if err != nil {
			return nil, errors.Annotatef(err, "DescribeStackEvents failed for stack '%s'", se.name)
		}
		events = append(events, out.StackEvents...)
		if out.NextToken == nil {
			break
		}
		in.NextToken = out.NextToken
	}
	// reverse
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func (se *StackEvents) update(config StackEventsWaitConfig) error {
	log.Debugf("starting stack events update for '%s'", se.name)
loop:
	for {
		events, err := se.getEvents()
		if err != nil {
			return errors.Annotatef(err, "cannot read events")
		}
		found := se.last == ""
		for _, e := range events {
			eventID := aws.StringValue(e.EventId)
			if found {
				eventData := newStackEventData(e)
				retry, err := config.Callback(eventData)
				if err != nil {
					return errors.Trace(err)
				} else if !retry {
					break loop
				}
				se.last = eventID
			} else if se.last == eventID {
				found = true
			}
		}
		select {
		case <-time.After(stackEventsWaitInterval):
		case <-config.Closer.Chan():
			return nil
		}
	}
	return nil
}

// StackEventsWaitFunc is the callback function type
// which is called to notify about new stack events.
type StackEventsWaitFunc func(*StackEventData) (again bool, err error)

// StackEventsWaitConfig is the waiter configuration
// for stack.
type StackEventsWaitConfig struct {
	// Callback is the function which is called
	// each time there is an update.
	Callback StackEventsWaitFunc

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

// Wait function periodically reads new stack events and
// invokes the config.Callback function for each of them.
// Waiter will stop if one of following hapenes:
//   * Error occurred when reading data
//   * Callback returns false
//   * Closer is closed
// CloseOnEnd and CloseOnError options indicate if
// Closer should be closed and if Closer should be closed
// when there is an error while reading.
func (se *StackEvents) Wait(config StackEventsWaitConfig) {
	go func() {
		err := se.update(config)
		if err != nil && config.CloseOnError {
			config.Closer.Close(errors.Trace(err))
			return
		}
		if config.CloseOnEnd {
			config.Closer.Close(nil)
		}
	}()
}
