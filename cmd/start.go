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
	"regexp"
	"sync"

	"github.com/frgrisk/ec2ctl/adapter/aws"

	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start INSTANCE-ID [INSTANCE-ID...]",
	Short: "Start one or more instances",
	Long:  `This command starts the specified instance(s).`,
	Args: func(cmd *cobra.Command, args []string) error {
		return validateInstanceArgs(args)
	},
	Run: func(cmd *cobra.Command, args []string) {
		startStop(args, aws.InstanceStart)
	},
}

func validateInstanceArgs(args []string) error {
	if len(args) < 1 && len(regions) == 0 {
		return errors.New("at least one instance ID is required")
	}
	for _, arg := range args {
		matched, err := regexp.MatchString("^i-[a-z|0-9]{8}|[a-z|0-9]{17}", arg)
		if err != nil {
			return err
		}
		if !matched || (len(arg) != 10 && len(arg) != 19) {
			return fmt.Errorf("%q is not a valid instance id", arg)
		}
	}
	return nil
}

func startStop(instances []string, action string) {
	// If a single region is not specified, query the instances in all regions and
	// determine the region the instance is located in
	regCheck := len(regions)
	var accSumRegions aws.AccountSummary
	var accSum aws.AccountSummary
	var wg sync.WaitGroup
	var region string
	var err error
	//preprocessing is done to filter and group the instances by the region
	//The grouping is done such that the maximum number of API calls correlates to the maximum nunber of avaiable regions
	regionMap := make(map[string][]string)
	//determine if user included regions tag
	if len(instances) > 0 {
		accSum = getAccountSummary([]string{}, tags)
		for _, instanceID := range instances {
			region, err = aws.GetInstanceRegion(accSum, instanceID)
			if err != nil {
				fmt.Printf("Error enountered looking up region for instance %q: %s\n", instanceID, err)
				return
			} else {
				regionMap[region] = append(regionMap[region], instanceID)
			}
		}
	}
	if regCheck > 0 {
		accSumRegions = getAccountSummary(regions, tags)
		accSumRegions.Prompt(action, &regionMap)
	}
	//initialised go routine for parallel api calls to increase speed
	for region, instanceSlice := range regionMap {
		wg.Add(1)
		go func(region string, instanceSlice []string) {
			defer wg.Done()
			state, err := aws.StartStopInstance(region, action, instanceSlice)
			if err != nil {
				fmt.Printf("Failed to %s instances %q in region %q: %v\n", action, instanceSlice, region, err)
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
		}(region, instanceSlice)
	}
	wg.Wait()
}

func init() {
	rootCmd.AddCommand(startCmd)
}
