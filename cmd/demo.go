package cmd

import (
	"encoding/json/v2"
	"fmt"

	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/cmd/types"
	"github.com/spf13/cobra"
)

const (
	demoAsiaRegion = "ap-southeast-1"
	demoUSRegion   = "us-east-1"
	demoLifecycle  = "on-demand"
)

// Synthetic data makes the fleet view discoverable without an AWS account.
var demoCmd = &cobra.Command{
	Use:     "demo",
	Short:   "Explore a sample fleet without connecting to AWS",
	Args:    cobra.NoArgs,
	Example: "  ec2ctl demo\n  ec2ctl demo --wide\n  ec2ctl demo -o json",
	RunE: func(cmd *cobra.Command, _ []string) error {
		summary := aws.AccountSummary{
			{Region: demoAsiaRegion, Instances: []aws.Instance{
				{Name: "api-dev", ID: "i-00000000000000001", Status: "running", Type: "t3.small", IP: "10.0.1.12", Environment: "dev", Region: demoAsiaRegion, AZ: "ap-southeast-1a", Lifecycle: demoLifecycle},
				{Name: "worker-dev", ID: "i-00000000000000002", Status: "stopped", Type: "t3.medium", IP: "10.0.2.24", Environment: "dev", Region: demoAsiaRegion, AZ: "ap-southeast-1b", Lifecycle: demoLifecycle, Hibernation: true},
			}},
			{Region: demoUSRegion, Instances: []aws.Instance{
				{Name: "api-prod", ID: "i-00000000000000003", Status: "running", Type: "m7i.large", IP: "10.1.1.10", Environment: "prod", Region: demoUSRegion, AZ: "us-east-1a", Lifecycle: demoLifecycle},
				{Name: "batch-worker", ID: "i-00000000000000004", Status: "pending", Type: "c7i.xlarge", IP: "10.1.2.18", Environment: "prod", Region: demoUSRegion, AZ: "us-east-1b", Lifecycle: "spot", SpotInstanceType: "one-time"},
			}},
		}
		if output == types.JSON {
			if err := json.MarshalWrite(cmd.OutOrStdout(), summary); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout())
			return err
		}
		ui := commandTerminal(cmd)
		ui.Printf("Sample fleet · no AWS requests\n")
		ui.Fleet(summary, wide)
		return nil
	},
}

func init() {
	demoCmd.Flags().BoolVarP(&wide, "wide", "w", false, "Show all sample instance details")
	rootCmd.AddCommand(demoCmd)
}
