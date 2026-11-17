package cmd

import (
	"github.com/spf13/cobra"
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
		return commandTerminal(command).RequireConfirmation(command.Name(), force)
	}
}
