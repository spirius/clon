package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/juju/errors"
)

func askForConfirmation(msg string) error {
	if configFlags.autoApprove {
		return nil
	}
	if !configFlags.input {
		return errors.Errorf("cannot confirm change, neither auto-approve nor input flags are set")
	}
	for {
		var res string
		fmt.Fprintf(os.Stderr, "\n%s", color.RedString("%s [yes/no]: ", msg))
		_, err := fmt.Scanln(&res)
		if err != nil {
			if err.Error() == "unexpected newline" {
				continue
			}
			return errors.Annotatef(err, "cannot read from stdin")
		}
		if res == "yes" {
			return nil
		} else if res == "no" || res == "n" {
			return errors.Errorf("not confirmed")
		}
	}
}
