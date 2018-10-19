package cfn

import (
	"fmt"
	"testing"

	"github.com/juju/errors"
	"github.com/stretchr/testify/require"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"

	mock "github.com/spirius/clon/pkg/cfn/mock"
)

func TestChangeSet_NewChangeSet_basic(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	csName := "my-cs"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.AddChangeSets([]*cloudformation.DescribeChangeSetOutput{{
		StackName:     aws.String(name),
		ChangeSetName: aws.String(csName),
		Status:        aws.String(cloudformation.ChangeSetStatusCreateComplete),
	}})

	csData := &ChangeSetData{
		Name: csName,
		StackData: &StackData{
			Name: name,
		},
	}

	cs, err := NewChangeSet(cfnconn, csData)
	require.Nil(err)
	require.NotNil(cs)

	data := cs.Data()
	require.Equal(csName, data.Name)
	require.Equal(name, data.StackData.Name)
	require.Equal(cloudformation.ChangeSetStatusCreateComplete, data.Status)
}

func TestChangeSet_NewChangeSet_error1(t *testing.T) {
	require := require.New(t)

	name := "mystack"
	cfnconn := mock.NewMockCloudFormationAPI()

	csData := &ChangeSetData{
		StackData: &StackData{
			Name: name,
		},
	}

	cs, err := NewChangeSet(cfnconn, csData)
	require.NotNil(err)
	require.Nil(cs)
}

func TestChangeSet_NewChangeSet_error2(t *testing.T) {
	require := require.New(t)

	experr := fmt.Errorf("error")
	name := "mystack"
	csName := "my-cs"
	cfnconn := mock.NewMockCloudFormationAPI()
	cfnconn.MockDescribeChangeSet = func(in *cloudformation.DescribeChangeSetInput) (*cloudformation.DescribeChangeSetOutput, error) {
		return nil, experr
	}

	csData := &ChangeSetData{
		Name: csName,
		StackData: &StackData{
			Name: name,
		},
	}

	cs, err := NewChangeSet(cfnconn, csData)
	require.NotNil(err)
	require.Nil(cs)
	require.Equal(experr, err.(*errors.Err).Cause())
}
