package clon

// Config is the configuration of StackManager
type Config struct {
	// Name of the deployment
	Name string

	// AccountID is the target AWS account ID.
	// If set, StackManager will verify, that
	// current AWS credentials are from that account.
	AccountID string

	// Region is the AWS region.
	Region string

	// Stacks is the list of stacks that are managed by StackManger.
	Stacks []StackConfig

	// Bootstrap is bootstrap stack configuration
	Bootstrap StackConfig

	// Files is the map of files to sync with S3 bucket.
	Files map[string]FileConfig

	// Variables is the map of variables.
	Variables map[string]string

	IgnoreNestedUpdates bool
	RootStack           string
}

// StackConfig is the configuration of single stack.
type StackConfig struct {
	Name         string
	Template     string
	RoleARN      string
	Parameters   map[string]string
	Capabilities []string
	Tags         map[string]string
}

// FileConfig is the configuration of single file.
type FileConfig struct {
	Src    string
	Bucket string
	Key    string
}
