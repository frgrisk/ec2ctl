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
	Args: func(cmd *cobra.Command, args []string) error {
		return validateInstanceArgs(args)
	},
	Example: "ec2ctl modify --type r6g.xlarge i-04f95703166d053ed",
	Run:     modifyInstances,
}

func init() {
	rootCmd.AddCommand(modifyCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// modifyCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	modifyCmd.Flags().String("type", "", "Instance type to change the instance(s) to.")
	modifyCmd.MarkFlagRequired("type")
}

func modifyInstances(cmd *cobra.Command, instances []string) {
	// Get account summary based on regions and tags specified
	accSum := getAccountSummary(regions, tags, "", instances)

	instanceMap := make(map[string]*aws.Instance, 0)

	for _, i := range instances {
		instanceMap[i] = nil
	}

	for _, r := range accSum {
		for _, i := range r.Instances {
			_, ok := instanceMap[i.ID]
			if ok {
				instanceMap[i.ID] = &i
			}
		}
	}

	t, err := cmd.Flags().GetString("type")
	if err != nil {
		fmt.Println("error parsing type flag:", err)
		return
	}

	for k, v := range instanceMap {
		if v == nil {
			fmt.Printf("instance %s not found\n", k)
			continue
		}
		err := aws.ModifyInstanceType(v.Region, t, k)
		if err != nil {
			fmt.Printf("error modifying instance %s: %v\n", k, err)
			return
		}
	}
}
