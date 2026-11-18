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
	"fmt"

	"github.com/frgrisk/ec2ctl/adapter/aws"

	"github.com/spf13/cobra"
)

// modifyCmd represents the modify command
var modifyCmd = &cobra.Command{
	Use:   "modify INSTANCE-ID [INSTANCE-ID...]",
	Short: "Modify one or more instances",
	Long:  `This command modifies the specified instance(s).`,
	Args: func(_ *cobra.Command, args []string) error {
		return validateInstanceArgs(args)
	},
	Example: "ec2ctl modify --type r6g.xlarge i-04f95703166d053ed",
	RunE:    modifyInstances,
}

func init() {
	rootCmd.AddCommand(modifyCmd)

	modifyCmd.Flags().String("type", "", "Instance type to change the instance(s) to.")
	_ = modifyCmd.MarkFlagRequired("type")
}

func modifyInstances(cmd *cobra.Command, instances []string) error {
	// Get account summary based on regions and tags specified
	ui := commandTerminal(cmd)
	accSum, err := getAccountSummary(
		ui,
		regions,
		tags,
		"",
		instances,
	)
	if err != nil {
		return err
	}

	t, err := cmd.Flags().GetString("type")
	if err != nil {
		return err
	}

	targets, err := resolveExplicitInstances(accSum, instances)
	if err != nil {
		return err
	}
	for _, instance := range targets.ordered {
		id := instance.ID
		if err := aws.ModifyInstanceType(instance.Region, t, id); err != nil {
			return fmt.Errorf("modify %s: %w", id, err)
		}
		ui.Printf("%s  %s  instance type changed to %s\n", instance.Region, id, t)
	}
	return nil
}
