package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spirius/clon/pkg/clon"

	"github.com/fatih/color"
	"github.com/juju/errors"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"
)

// Revision is the revision number of build (commit Id).
var Revision = ""

// Version is the version of the curren build.
var Version = "v0.0.1"

type errorCode struct {
	err  error
	code int
}

func (e errorCode) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return "nil"
}

var config clon.Config

var configFlags struct {
	// Automatically approve changes
	autoApprove bool

	// Enables debug logging
	debug bool

	// Enabled tracing on errors
	trace bool

	// Indicates if user-input is available.
	// Defaults to true, if stdin is terminal.
	input bool

	config         string
	configOverride string

	ignoreNestedUpdates bool
}

func cmdResultHandler(out interface{}, err error) error {
	if out != nil {
		switch res := out.(type) {
		case output:
			res.Output(os.Stdout)
		case []output:
			for _, r := range res {
				r.Output(os.Stdout)
				fmt.Printf("\n")
			}
		default:
			fmt.Printf("Unknown type %#+v\n", res)
		}
	}
	if err != nil {
		return errors.Annotatef(err, "command returned error")
	}
	return nil
}

var stackHandler *stackCmdHandler

func decodeConfig(config *clon.Config, r io.Reader) error {
	var err error
	config.IgnoreNestedUpdates = configFlags.ignoreNestedUpdates
	m := make(map[string]interface{})
	if err = yaml.NewDecoder(r).Decode(m); err != nil {
		return errors.Annotatef(err, "syntax error")
	}

	if err = mapstructure.WeakDecode(m, &config); err != nil {
		return errors.Annotatef(err, "cannot parse config")
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:           "clon",
	Short:         "clon is a CLoudFormatiON stack management tool",
	SilenceErrors: true,
	SilenceUsage:  true,

	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		log.SetFormatter(&logFormatter{})
		if configFlags.debug {
			log.SetLevel(log.DebugLevel)
		}
		c, err := os.Open(configFlags.config)
		if err != nil {
			return errors.Annotatef(err, "cannot open config")
		}
		if err = decodeConfig(&config, c); err != nil {
			return errors.Annotatef(err, "cannot parse config")
		}
		if configFlags.configOverride != "" {
			c, err = os.Open(configFlags.configOverride)
			if err != nil {
				errors.Annotatef(err, "cannot open config")
			}
			if err = decodeConfig(&config, c); err != nil {
				return errors.Annotatef(err, "cannot parse config")
			}
		}

		if stackHandler, err = newStackCmdHandler(config); err != nil {
			return errors.Annotatef(err, "cannot initialize clon")
		}
		return nil

	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&configFlags.debug, "debug", "d", false, "Enable debug mode")
	rootCmd.PersistentFlags().BoolVarP(&configFlags.trace, "trace", "t", false, "Enable error tracing output")
	rootCmd.PersistentFlags().BoolVarP(&configFlags.input, "input", "i", terminal.IsTerminal(int(os.Stdin.Fd())), "User input availability. If not specified, value is identified from terminal.")
	rootCmd.PersistentFlags().StringVarP(&configFlags.config, "config", "c", "config.yml", "Config file")
	rootCmd.PersistentFlags().StringVarP(&configFlags.configOverride, "config-override", "e", "", "Override config file")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List stacks",
		Long:  `List short status information of all stacks.`,
		Args:  exactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := stackHandler.list()
			return cmdResultHandler(res, err)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "status {stack-name}",
		Short: "Show stack status",
		Long:  `Show status of the stack.`,
		Args:  exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := stackHandler.status(args[0])
			return cmdResultHandler(res, errors.Trace(err))
		},
	})

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize bootstrap stack",
		Long:  `Initialize bootstrap stack.`,
		Args:  exactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := stackHandler.init()
			return cmdResultHandler(res, errors.Annotatef(err, "error while initializing"))
		},
	}
	initCmd.PersistentFlags().BoolVarP(&configFlags.autoApprove, "auto-approve", "a", false, "Auto-approve changes")
	rootCmd.AddCommand(initCmd)

	planCmd := &cobra.Command{
		Use:   "plan {stack-name} [plan-id]",
		Short: "Plan stack changes",
		Long: `Plan the changes on stack using change set.
If plan-id is specified, displays previously planned change.

  exit codes are following:
  0 - no changes on stack
  1 - error occurred
  2 - contains changes
`,
		Args: rangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			var (
				res interface{}
				err error
			)
			if len(args) == 1 {
				res, err = stackHandler.plan(args[0])
			} else {
				res, err = stackHandler.planStatus(args[0], args[1])
			}
			return cmdResultHandler(res, errors.Annotatef(err, "error while planning"))
		},
	}
	rootCmd.AddCommand(planCmd)
	planCmd.PersistentFlags().BoolVarP(
		&configFlags.ignoreNestedUpdates,
		"ignore-nested-updates",
		"",
		true,
		"Do not consider stack changed, if only nested stack automatics updates are performed",
	)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "execute {stack-name} {plan-id}",
		Short: "Execute previously planned change",
		Long:  `Execute previously planned change on stack.`,
		Args:  exactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := stackHandler.execute(args[0], args[1])
			return cmdResultHandler(res, errors.Annotatef(err, "error while executing"))
		},
	})

	destroyCmd := &cobra.Command{
		Use:   "destroy {stack-name}",
		Short: "Destroy stack",
		Long: `Destroy AWS CloudFormation stack.

This command requires interactive shell or -a flag to be specified.`,
		Args: exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := stackHandler.destroy(args[0])
			return cmdResultHandler(res, errors.Annotatef(err, "error while destroying"))
		},
	}
	destroyCmd.PersistentFlags().BoolVarP(&configFlags.autoApprove, "auto-approve", "a", false, "Auto-approve changes")
	rootCmd.AddCommand(destroyCmd)

	deployCmd := &cobra.Command{
		Use:   "deploy {stack-name}",
		Short: "Deploy stack",
		Long: `Deploy AWS CloudFormation stack.

This command requires interactive shell or -a flag to be specified.`,
		Args: exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			res, err := stackHandler.deploy(args[0])
			return cmdResultHandler(res, errors.Annotatef(err, "error while deploying"))
		},
	}
	deployCmd.PersistentFlags().BoolVarP(&configFlags.autoApprove, "auto-approve", "a", false, "Auto-approve changes")
	deployCmd.PersistentFlags().BoolVarP(
		&configFlags.ignoreNestedUpdates,
		"ignore-nested-updates",
		"",
		true,
		"Do not consider stack changed, if only nested stack automatics updates are performed",
	)
	rootCmd.AddCommand(deployCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:               "version",
		Short:             "show version information",
		Args:              exactArgs(0),
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("clon %s\n", Version)
			return nil
		},
	})
}

type argsError struct {
	err error
	cmd *cobra.Command
}

func (e argsError) Error() string {
	return e.err.Error()
}

func exactArgs(n int) func(cmd *cobra.Command, args []string) error {
	cb := cobra.ExactArgs(n)
	return func(cmd *cobra.Command, args []string) error {
		if err := cb(cmd, args); err != nil {
			return argsError{err, cmd}
		}
		return nil
	}
}

// nolint: unparam
func rangeArgs(min, max int) func(cmd *cobra.Command, args []string) error {
	cb := cobra.RangeArgs(min, max)
	return func(cmd *cobra.Command, args []string) error {
		if err := cb(cmd, args); err != nil {
			return argsError{err, cmd}
		}
		return nil
	}
}

// Execute will execute the root command and output.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		traceableError, ok := err.(*errors.Err)
		var stackTrace []string
		if ok {
			err = errors.Cause(traceableError)
			stackTrace = traceableError.StackTrace()
		}

		if e, ok := err.(argsError); ok {
			fmt.Fprintf(os.Stderr, "Error: %s\n", e)
			e.cmd.Usage()
			os.Exit(1)
			return
		}

		code := 1
		if e, ok := err.(*errorCode); ok {
			code = e.code
			err = e.err
		}

		if err != nil && code == 0 {
			fmt.Fprintln(os.Stderr, err)
		} else if err != nil {
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "%s", err)

			if stackTrace != nil && configFlags.trace {
				fmt.Fprintf(&buf, "\n%s", strings.Join(stackTrace[1:], "\n"))
				fmt.Fprintf(&buf, "\nclon %s (commit %s)", Version, Revision)
			}

			fmt.Println(color.RedString("Error: %s", buf.String()))
		}
		os.Exit(code)
	}
}
