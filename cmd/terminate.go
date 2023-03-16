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
	"bufio"
	"fmt"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

// terminateCmd represents the terminate command
var terminateCmd = &cobra.Command{
	Use:   "terminate INSTANCE-ID [INSTANCE-ID...]",
	Short: "Terminate one or more instances",
	Long:  `This command terminate one or more instances.`,
	Args: func(cmd *cobra.Command, args []string) error {
		return validateInstanceArgs(args)
	},
	Run:     terminateInstance,
	Aliases: []string{"delete", "destroy"},
}

func init() {
	rootCmd.AddCommand(terminateCmd)

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	terminateCmd.Flags().BoolP("force", "f", false, "Force terminate the instance (do not prompt for confirmation)")
}

func terminateInstance(cmd *cobra.Command, instances []string) {
    // Get account summary based on regions and tags specified
	accSum := getAccountSummary(regions, tags, "", instances)

	instanceMap := make(map[string]*aws.Instance, 0)
	instanceRegionMap := make(map[string][]string, 0)

	for _, i := range instances {
		instanceMap[i] = nil
	}

	for _, r := range accSum {
		for _, i := range r.Instances {
			_, ok := instanceMap[i.ID]
			if ok {
				instanceMap[i.ID] = &i
				if _, ok := instanceRegionMap[i.Region]; !ok {
					instanceRegionMap[i.Region] = []string{}
				}
				instanceRegionMap[i.Region] = append(instanceRegionMap[i.Region], i.ID)
			}
		}
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		fmt.Println("cannot get value of force flag:", err)
		return
	}
	for k, v := range instanceRegionMap {
		if !force {
			fmt.Printf(`Are you sure you want to terminate instances %v in region %s?
	Only 'yes' will be accepted to approve

	Enter a value: `, v, k)
			reader := bufio.NewReader(os.Stdin)
			text, _ := reader.ReadString('\n')
			text = strings.Replace(text, "\n", "", -1)
			if text != "yes" {
				continue
			}
		}
		err := aws.TerminateInstances(k, v)
		if err != nil {
			fmt.Printf("%s: error terminating instances %v: %s\n", k, v, err)
		} else {
			fmt.Printf("%s: successfully terminated the following instances %v\n", k, v)
		}
	}

	for k, v := range instanceMap {
		if v == nil {
			fmt.Println("instance", k, "could not be found")
		}
	}
}
