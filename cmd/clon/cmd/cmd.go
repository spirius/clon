package cmd

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/spf13/cobra"
)

func cmdResultHandler(out interface{}, err error) error {
	if out != nil {
		switch res := out.(type) {
		case output:
			res.Output(stdout)
		case []output:
			for _, r := range res {
				r.Output(stdout)
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

// nolint: unparam
func newCmd(parent *cobra.Command, cmd *cobra.Command, fn func(*cobra.Command, []string) (interface{}, error), flags ...func(*cobra.Command)) *cobra.Command {
	cmd.DisableFlagsInUseLine = true
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		out, err := fn(cmd, args)
		return cmdResultHandler(out, errors.Trace(err))
	}
	for _, flag := range flags {
		flag(cmd)
	}
	parent.AddCommand(cmd)
	return cmd
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
