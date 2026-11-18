/*
Copyright © 2021 FRG

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/spf13/cobra"
)

// terminateCmd represents the terminate command
var terminateCmd = &cobra.Command{
	GroupID: changeGroup,
	Use:     "terminate INSTANCE-ID [INSTANCE-ID...]",
	Short:   "Terminate one or more instances",
	Long:    `Permanently terminate the specified instances. This cannot be undone.`,
	Example: "  # Permanently terminate one instance after confirmation\n  ec2ctl terminate --regions us-east-1 i-0123456789abcdef0",
	Args: func(_ *cobra.Command, args []string) error {
		return validateInstanceArgs(args)
	},
	RunE:    terminateInstance,
	Aliases: []string{"delete", "destroy"},
}

func init() {
	configureConfirmation(terminateCmd)
	rootCmd.AddCommand(terminateCmd)
}

func terminateInstance(cmd *cobra.Command, instances []string) error {
	// Get account summary based on regions and tags specified
	ui := commandTerminal(cmd)
	accSum, err := getAccountSummary(
		cmd.Context(),
		ui,
		regions,
		tags,
		"",
		instances,
	)
	if err != nil {
		return err
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	targets, err := resolveExplicitInstances(accSum, instances)
	if err != nil {
		return err
	}
	ui.Fleet(accSum, false)
	for _, group := range targets.byRegion {
		k, v := group.region, group.ids
		if !force {
			confirm, err := ui.Confirm(
				cmd.Context(),
				fmt.Sprintf("Are you sure you want to terminate instance(s) [%s] in region %s?", strings.Join(v, ", "), k),
				"Termination is permanent and cannot be undone.")
			if err != nil {
				return err
			}

			if !confirm {
				ui.Printf("%s: cancelled. No instances terminated.\n", k)
				continue
			}
		}
		if err := ui.Progress(
			cmd.Context(),
			fmt.Sprintf("Requesting termination in %s...", k),
			func(ctx context.Context) error {
				return aws.TerminateInstances(ctx, k, v)
			},
		); err != nil {
			return fmt.Errorf("terminate in %s: %w", k, err)
		}
		ui.Printf("%s: termination requested for %v\n", k, v)
	}
	return nil
}
