package clon

import (
	"regexp"
	"strings"

	"github.com/juju/errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/sts"
)

type awsClient struct {
	sess *session.Session

	s3conn  s3iface.S3API
	cfnconn cloudformationiface.CloudFormationAPI

	accountID   string
	region      string
	sessionName string
}

func newAWSClient() (a *awsClient, err error) {
	a = &awsClient{}

	if a.sess, err = session.NewSession(); err != nil {
		return nil, errors.Annotatef(err, "cannot create awsClient")
	}

	a.s3conn = s3.New(a.sess)

	cfnconn := cloudformation.New(a.sess)
	cfnconn.Handlers.Retry.PushBack(func(r *request.Request) {
		if r.Operation.Name == "DescribeStackEvents" || r.Operation.Name == "DescribeStacks" {
			if e, ok := r.Error.(awserr.Error); ok && e.Code() == "Throttling" && strings.Contains(e.Message(), "Rate exceeded") {
				r.Retryable = aws.Bool(true)
			}
		}
	})
	a.cfnconn = cfnconn

	stsConn := sts.New(a.sess)

	out, err := stsConn.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, errors.Annotatef(err, "cannot identify user identity")
	}

	identityArn, err := arn.Parse(aws.StringValue(out.Arn))
	if err != nil {
		return nil, err
	}
	a.accountID = identityArn.AccountID
	a.sessionName = regexp.MustCompile("[^A-Za-z0-9-]").ReplaceAllString(identityArn.Resource, "-")
	a.region = aws.StringValue(a.sess.Config.Region)

	return
}
