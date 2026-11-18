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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/cmd/types"
	"github.com/frgrisk/ec2ctl/internal/terminal"
	"github.com/spf13/cobra"
)

var wide bool

var statusCmd = &cobra.Command{
	GroupID: exploreGroup,
	Use:     "status [INSTANCE-ID...]",
	Aliases: []string{"ls", "list"},
	Short:   "See your fleet, grouped by region",
	Long:    "Show matching instances with their state, type and private IP. Use --wide for all details.",
	Example: `  # Compare regions
  ec2ctl status --regions \
    us-east-1,ap-southeast-1

  # Find development instances
  ec2ctl status --tag Environment=dev

  # Inspect all details for one instance
  ec2ctl status i-0123456789abcdef0 \
    --wide

  # Export fleet data
  ec2ctl status -o json`,
	Args: optionalInstanceArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ui := commandTerminal(cmd)
		summary, err := getAccountSummary(
			cmd.Context(),
			ui,
			regions,
			tags,
			aws.InstanceStatus,
			args,
		)
		if err != nil {
			return err
		}
		if output == types.JSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(summary)
		}
		if len(summary) == 0 {
			ui.Printf("No instances match your filters. Check --regions, --tag and your AWS profile.\n")
			return nil
		}
		ui.Fleet(summary, wide)
		return nil
	},
}

func getAccountSummary(
	ctx context.Context,
	ui *terminal.Terminal,
	regions []string,
	tags map[string]string,
	action string,
	instanceIDs []string,
) (aws.AccountSummary, error) {
	if len(regions) == 0 {
		err := ui.Progress(ctx, "Discovering enabled regions...", func(ctx context.Context) error {
			var err error
			regions, err = aws.GetRegions(ctx)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("discover regions: %w", err)
		}
	}
	regions = slices.Compact(slices.Sorted(slices.Values(regions)))
	summary := make(aws.AccountSummary, 0, len(regions))
	results := make([]aws.RegionSummary, len(regions))
	failures := make([]error, len(regions))
	err := ui.Progress(
		ctx,
		fmt.Sprintf("Scanning %d regions for matching instances...", len(regions)),
		func(ctx context.Context) error {
			var wg sync.WaitGroup
			for i, region := range regions {
				wg.Go(func() {
					var err error
					results[i], err = aws.GetDeployedInstances(
						ctx,
						region,
						tags,
						action,
						instanceIDs,
					)
					if err != nil {
						failures[i] = fmt.Errorf("%s: %w", region, err)
					}
				})
			}
			wg.Wait()
			return errors.Join(failures...)
		},
	)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if len(result.Instances) > 0 {
			summary = append(summary, result)
		}
	}
	return summary, nil
}

func init() {
	statusCmd.Flags().BoolVarP(&wide, "wide", "w", false, "Include environment, placement, lifecycle and hibernation details")
	rootCmd.AddCommand(statusCmd)
}
