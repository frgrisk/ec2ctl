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
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/internal/terminal"

	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	GroupID: changeGroup,
	Use:     "start [INSTANCE-ID...]",
	Short:   "Bring stopped instances online",
	Long:    "Find stopped instances, review the matches, and confirm before starting them.",
	Example: `  # Review and start development instances
  ec2ctl start --regions us-east-1 \
    --tag Environment=dev

  # Review and start one instance
  ec2ctl start --regions us-east-1 \
    i-0123456789abcdef0

  # Start matching instances without confirmation
  ec2ctl start --regions us-east-1 \
    --tag Environment=dev --force`,
	Args: optionalInstanceArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			return err
		}
		return startStop(
			cmd.Context(),
			commandTerminal(cmd),
			args,
			aws.InstanceStart,
			force,
		)
	},
}

var instanceIDPattern = regexp.MustCompile(`^i-([0-9a-f]{8}|[0-9a-f]{17})$`)

func validateInstanceArgs(args []string) error {
	if len(args) == 0 {
		return errors.New("at least one instance ID is required")
	}
	return optionalInstanceArgs(nil, args)
}

func optionalInstanceArgs(_ *cobra.Command, args []string) error {
	for _, arg := range args {
		if !instanceIDPattern.MatchString(arg) {
			return fmt.Errorf("%q is not a valid instance ID; expected i- followed by 8 or 17 hexadecimal characters", arg)
		}
	}
	return nil
}

func startStop(
	ctx context.Context,
	ui *terminal.Terminal,
	instances []string,
	action string,
	force bool,
) error {
	var accSum aws.AccountSummary
	var wg sync.WaitGroup

	// Filter instances by region, tags, and current status
	var err error
	accSum, err = getAccountSummary(
		ctx,
		ui,
		regions,
		tags,
		action,
		instances,
	)
	if err != nil {
		return err
	}
	if len(accSum) == 0 {
		ui.Printf("No instances ready to %s match your filters. Try ec2ctl status with the same filters.\n", action)
		return nil
	}
	ui.Fleet(accSum, false)
	if !force {
		confirmed, err := ui.Confirm(
			ctx,
			"This action will "+action+" the instances above matching the filter. Would you like to proceed?",
			"",
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ui.Printf("Cancelled. No instances changed.\n")
			return nil
		}
	}

	// Run regions concurrently, then print results in a stable order.
	type actionResult struct {
		err     error
		message string
	}
	results := make([]actionResult, len(accSum))
	numInstances := 0
	for _, regionSum := range accSum {
		numInstances += len(regionSum.Instances)
	}
	progressErr := ui.Progress(
		ctx,
		fmt.Sprintf("Sending %s request for %d instances...", action, numInstances),
		func(ctx context.Context) error {
			for i, regionSum := range accSum {
				wg.Go(func() {
					ids := aws.IDs(regionSum.Instances)
					states, err := aws.StartStopInstance(
						ctx,
						regionSum.Region,
						action,
						ids,
					)
					if err != nil {
						results[i].err = fmt.Errorf("%s: cannot %s %v: %w", regionSum.Region, action, ids, err)
						return
					}
					for _, state := range states {
						results[i].message += fmt.Sprintf("%s  %s  %s -> %s\n", regionSum.Region, *state.InstanceId, state.PreviousState.Name, state.CurrentState.Name)
					}
				})
			}
			wg.Wait()
			return nil
		},
	)
	// Progress has joined the action even when animation or its caller was
	// cancelled. Report every collected result before returning that error.
	var failures []error
	for _, result := range results {
		ui.Printf("%s", result.message)
		if result.err != nil {
			failures = append(failures, result.err)
		}
	}
	if progressErr != nil {
		failures = append(failures, progressErr)
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	ui.Printf(
		"%s request accepted for %d instances. Run ec2ctl status with the same filters to check progress.\n",
		action,
		numInstances,
	)
	return nil
}

func init() {
	configureConfirmation(startCmd)
	rootCmd.AddCommand(startCmd)
}
