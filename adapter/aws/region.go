package aws

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// RegionSummary is a structure holding deployed instances in a given region
type RegionSummary struct {
	Region    string
	Instances []Instance
}

// AccountSummary is a structure holding a slice of regions summaries across an entire account
type AccountSummary []RegionSummary

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
