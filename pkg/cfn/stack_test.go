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

func TestStack_read_basic(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack := &Stack{Name: name, cfnconn: cfnconn}

	cfnstack, err := stack.read()
	require.Nil(err)
	require.NotNil(cfnstack)

	require.Equal(name, aws.StringValue(cfnstack.StackName))
	require.Equal(cloudformation.StackStatusCreateComplete, aws.StringValue(cfnstack.StackStatus))
}

func TestStack_read_notFound(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	stack := &Stack{Name: name, cfnconn: cfnconn}

	cfnstack, err := stack.read()
	require.Nil(err)
	require.Nil(cfnstack)

	cfnconn.MockDescribeStacks = func(in *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
		return &cloudformation.DescribeStacksOutput{}, nil
	}

	stack = &Stack{Name: name, cfnconn: cfnconn}

	cfnstack, err = stack.read()
	require.Nil(err)
	require.Nil(cfnstack)
}

func TestStack_read_error(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	experr := fmt.Errorf("error")
	cfnconn.MockDescribeStacks = func(in *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
		return nil, experr
	}

	stack := &Stack{Name: name, cfnconn: cfnconn}

	cfnstack, err := stack.read()
	require.NotNil(err)
	require.Equal(experr, err.(*errors.Err).Cause())
	require.Nil(cfnstack)
}

func TestStack_NewStack_basic(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)

	require.Nil(err)
	require.NotNil(stack)
	data := stack.Data()
	require.Equal(name, stack.Name)
	require.Equal(name, data.Name)
	require.Equal(cloudformation.StackStatusCreateComplete, data.Status)
}

func TestStack_NewStack_error(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	experr := fmt.Errorf("error")
	cfnconn.MockDescribeStacks = func(in *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
		return nil, experr
	}

	stack, err := NewStack(cfnconn, name)
	require.NotNil(err)
	require.Nil(stack)
}

func TestStack_update_closer(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)

	var wg sync.WaitGroup
	wg.Add(1)

	cl := closer.New()
	go func() {
		stack.update(StackWaitConfig{
			Closer: cl,
			Callback: func(d *StackData) (bool, error) {
				return true, nil
			},
		}, 0)
		wg.Done()
	}()

	err = fmt.Errorf("error")
	cl.Close(err)
	wg.Wait()

	require.Equal(err, cl.Wait())
}

func TestStack_update_error1(t *testing.T) {
	require := require.New(t)

	experr := fmt.Errorf("error")
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)

	cfnconn.MockDescribeStacks = func(*cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
		return nil, experr
	}

	cl := closer.New()
	err = stack.update(StackWaitConfig{
		Closer: cl,
		Callback: func(d *StackData) (bool, error) {
			return true, nil
		},
	}, 0)

	require.Equal(experr, err.(*errors.Err).Cause())
}

func TestStack_update_error2(t *testing.T) {
	require := require.New(t)

	experr := fmt.Errorf("error")
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)

	cl := closer.New()
	err = stack.update(StackWaitConfig{
		Closer: cl,
		Callback: func(d *StackData) (bool, error) {
			return false, experr
		},
	}, 0)

	require.Equal(experr, err.(*errors.Err).Cause())
}

func TestStack_Wait_closeOnEnd(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)

	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusUpdateComplete),
	}})

	cl := closer.New()
	stack.Wait(StackWaitConfig{
		Closer: cl,
		Callback: func(d *StackData) (bool, error) {
			return false, nil
		},
		CloseOnEnd: true,
	})

	cl.Wait()

	data := stack.Data()
	require.Equal(cloudformation.StackStatusUpdateComplete, data.Status)
}

func TestStack_Wait_closeOnError(t *testing.T) {
	require := require.New(t)

	experr := fmt.Errorf("error")
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusCreateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)

	cl := closer.New()
	stack.Wait(StackWaitConfig{
		Closer: cl,
		Callback: func(d *StackData) (bool, error) {
			return false, experr
		},
		CloseOnError: true,
	})

	err = cl.Wait()

	require.Equal(experr, err.(*errors.Err).Cause())
}

func TestStack_Data(t *testing.T) {
	require := require.New(t)
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	data := (&Stack{Name: name, cfnconn: cfnconn}).Data()
	require.Equal(name, data.Name)
	require.Equal(StackStatusNotFound, data.Status)
}

func TestStack_Destroy(t *testing.T) {
	require := require.New(t)
	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	cfnconn.AddStacks([]*cloudformation.Stack{{
		StackName:   aws.String(name),
		StackStatus: aws.String(cloudformation.StackStatusUpdateComplete),
	}})

	stack, err := NewStack(cfnconn, name)
	require.Nil(err)
	require.NotNil(stack)
	require.Equal(cloudformation.StackStatusUpdateComplete, stack.Data().Status)

	err = stack.Destroy()
	require.Nil(err)

	cl := closer.New()
	stack.Wait(StackWaitConfig{
		Closer: cl,
		Callback: func(d *StackData) (bool, error) {
			return d.Status != StackStatusNotFound, nil
		},
		CloseOnEnd:   true,
		CloseOnError: true,
	})
	err = cl.Wait()
	require.Nil(err)

	require.Equal(StackStatusNotFound, stack.Data().Status)
	stack, err = NewStack(cfnconn, name)
	require.Nil(err)
	require.Equal(StackStatusNotFound, stack.Data().Status)
}
