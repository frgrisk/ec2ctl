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
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sync"

	"github.com/charmbracelet/huh/spinner"
	"github.com/frgrisk/ec2ctl/adapter/aws"

	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start one or more instances",
	Long: `This command lists all matching stopped instance(s), and gives option to
	starts the matched instance(s).

	Examples:
	# Start all regions
	ec2ctl start
	# Start specific regions
	ec2ctl start --regions us-east-1,ap-southeast-1
	# Start specific tags
	ec2ctl start --tag Environment:dev
	`,
	Run: func(_ *cobra.Command, args []string) {
		startStop(args, aws.InstanceStart)
	},
}

func validateInstanceArgs(args []string) error {
	if len(args) < 1 && len(regions) == 0 {
		return errors.New("at least one instance ID is required")
	}
	re := regexp.MustCompile("^i-[a-z|0-9]{8}|[a-z|0-9]{17}")
	for _, arg := range args {
		matched := re.MatchString(arg)
		if !matched || (len(arg) != 10 && len(arg) != 19) {
			return fmt.Errorf("%q is not a valid instance id", arg)
		}
	}
	return nil
}

func startStop(instances []string, action string) {
	var accSum aws.AccountSummary
	var wg sync.WaitGroup

	// Filter instances by region, tags, and current status
	accSum = getAccountSummary(regions, tags, action, instances)
	// Show confirmation prompt to user, showing list of matched instances
	accSum = accSum.Prompt(action)

	// Preprocessing is done to filter and group the instances by the region
	// The grouping is done such that the maximum number of API calls correlates to the maximum nunber of available regions
	// Initialised go routine for parallel api calls to increase speed
	numInstances := 0
	for _, regionSum := range accSum {
		numInstances += len(regionSum.Instances)
		wg.Add(1)
		var instanceIDs []string
		for _, instance := range regionSum.Instances {
			instanceIDs = append(instanceIDs, instance.ID)
		}
		region := regionSum.Region
		go func(region string, instanceIDs []string) {
			defer wg.Done()
			state, err := aws.StartStopInstance(region, action, instanceIDs)
			if err != nil {
				fmt.Printf("Failed to %s instances %q in region %q: %v\n", action, instanceIDs, region, err)
				return
			}
			for _, stateChange := range state {
				if stateChange.PreviousState.Name == stateChange.CurrentState.Name {
					fmt.Printf(
						"Instance %s was already in a %s state.\n",
						*stateChange.InstanceId,
						stateChange.PreviousState.Name,
					)
				} else {
					fmt.Printf(
						"Instance %s state changed from %s to %s.\n",
						*stateChange.InstanceId,
						stateChange.PreviousState.Name,
						stateChange.CurrentState.Name,
					)
				}
			}
		}(region, instanceIDs)
	}
	if err := spinner.New().
		Title(fmt.Sprintf("Performing %s on %d instances...", action, numInstances)).
		Accessible(os.Getenv("ACCESSIBLE") != "").
		Action(wg.Wait).
		Run(); err != nil {
		log.Fatalf("Cannot render spinner: %v", err)
	}
}

func init() {
	rootCmd.AddCommand(startCmd)
}
