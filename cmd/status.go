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
	"encoding/json"
	"fmt"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/cmd/types"

	"github.com/spf13/cobra"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "List available instances and their statuses",
	Long: `This command lists all available instances and their statuses.

Examples:
  # Query all regions
  ec2ctl status
  # Query specific regions
  ec2ctl status --regions us-east-1,ap-southeast-1
`,
	Run: func(cmd *cobra.Command, args []string) {
		// If a region subset is not specified, query all regions
		accSum := getAccountSummary(regions)

		switch output {
		case types.JSON:
			jsonBytes, err := json.Marshal(accSum)
			if err != nil {
				fmt.Println("Error:", err)
				return
			}
			fmt.Println(string(jsonBytes))
		case types.Table:
			accSum.Print()
		}
	},
}

func getAccountSummary(regions []string) (accSum aws.AccountSummary) {
	if len(regions) == 0 {
		regions = aws.GetRegions()
	}

	c := make(chan aws.RegionSummary)
	for _, r := range regions {
		go aws.GetDeployedInstances(r, c)
	}
	var regSum aws.RegionSummary

	for range regions {
		regSum = <-c
		if len(regSum.Instances) > 0 {
			accSum = append(accSum, regSum)
		}
	}
	return
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
