package aws

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/charmbracelet/huh"
)

// RegionSummary is a structure holding deployed instances in a given region
type RegionSummary struct {
	Region    string
	Instances []Instance
}

// AccountSummary is a structure holding a slice of regions summaries across an entire account
type AccountSummary []RegionSummary

// Print prints the summary of instances in an account in tabular format
func (u AccountSummary) Print() {
	u.PrintWide(false)
}

// Prompt prompts user for confirmation
func (u AccountSummary) Prompt(action string) (AccountSummary, error) {
	// If no region summary in account summary, means no matching instances, return err
	if len(u) == 0 {
		fmt.Println("No instances are available for " + action + " command.")
		return AccountSummary{}, nil
	}
	// If region summary exists in account summary, means there are matching instances, return as table
	u.Print()
	var confirm bool
	prompt := huh.NewConfirm().
		Title("This action will " + action + " the instances above matching the filter. Would you like to proceed?").
		Value(&confirm).
		Affirmative("Yes").
		Negative("No")
	var err error
	if os.Getenv("ACCESSIBLE") != "" || os.Getenv("TERM") == "dumb" {
		err = prompt.RunAccessible(os.Stdout, os.Stdin)
	} else {
		err = prompt.Run()
	}
	if err != nil {
		return nil, fmt.Errorf("confirmation cancelled: %w", err)
	}

	// If user acknowledges, return account summary associated
	if confirm {
		return u, nil
	}
	// Else, return empty
	return AccountSummary{}, nil
}

// GetInstanceRegion returns the region of an instance given an account summary
func GetInstanceRegion(accSum AccountSummary, id string) (string, error) {
	for _, region := range accSum {
		if slices.ContainsFunc(region.Instances, func(instance Instance) bool {
			return instance.ID == id
		}) {
			return region.Region, nil
		}
	}
	return "", errors.New("instance not found")
}

// Print prints the summary of instances in a given region in tabular format
func (u RegionSummary) Print() {
	WriteTable(u.Region, u.Instances)
}

// GetRegions is a function to retrieve all active regions in an account
func GetRegions() ([]string, error) {
	var regions []string
	ctx := context.TODO()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg.DefaultsMode = aws.DefaultsModeCrossRegion
	cfg.Region = cmp.Or(cfg.Region, "us-east-1")
	svc := ec2.NewFromConfig(cfg)
	input := &ec2.DescribeRegionsInput{
		Filters: []types.Filter{
			{
				Name: new("opt-in-status"),
				Values: []string{
					"opted-in",
					"opt-in-not-required",
				},
			},
		},
	}

	result, err := svc.DescribeRegions(ctx, input)
	if err != nil {
		return nil, err
	}

	for _, r := range result.Regions {
		regions = append(regions, *r.RegionName)
	}

	return regions, nil
}

// Helper function to extract instance IDs from a slice of instances
func IDs(instances []Instance) []string {
	ids := make([]string, len(instances))
	for i, instance := range instances {
		ids[i] = instance.ID
	}
	return ids
}
