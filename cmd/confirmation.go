package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func configureConfirmation(command *cobra.Command) {
	command.Flags().BoolP("force", "f", false, "Do not prompt for confirmation (required for non-interactive use)")
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.PreRunE = func(command *cobra.Command, _ []string) error {
		force, err := command.Flags().GetBool("force")
		if err != nil {
			return err
		}
		if !force && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("cannot confirm %s without terminal input; use --force (-f) to skip confirmation", command.Name())
		}
		return nil
	}
}
