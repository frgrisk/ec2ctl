package cmd

import (
	"github.com/frgrisk/ec2ctl/internal/terminal"
	"github.com/spf13/cobra"
)

func commandTerminal(command *cobra.Command) *terminal.Terminal {
	return terminal.New(command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
}
