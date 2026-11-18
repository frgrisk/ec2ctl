package aws

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

const (
	// InstanceStart is the action to start an instance
	InstanceStart string = "start"
	// InstanceStop is the action to stop an instance
	InstanceStop string = "stop"
	// InstanceStatus is the action to query status of instance
	InstanceStatus string = "status"
	// InstanceHibernate is the action to hibernate an instance
	InstanceHibernate string = "hibernate"
	// DryRunOperation is the error code for dry run operation
	DryRunOperation string = "DryRunOperation"
)

// Instance is a struct to hold instance characteristics
type Instance struct {
	Name             string
	ID               string
	Status           types.InstanceStateName
	Type             types.InstanceType
	Lifecycle        string
	Environment      string
	IP               string
	SpotInstanceType types.SpotInstanceType
	Region           string
	AZ               string
	Hibernation      bool
}

// GetDeployedInstances retrieves the status of all deployed instances in a given region
func GetDeployedInstances(
	ctx context.Context,
	region string,
	tags map[string]string,
	action string,
	instanceIDs []string,
) (RegionSummary, error) {
	var rSummary RegionSummary
	rSummary.Region = region

	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return rSummary, err
	}

	svc := ec2.NewFromConfig(cfg)

	// Filter by state type
	var stateFilter types.Filter
	switch action {
	case InstanceStop:
		stateFilter = types.Filter{
			Name: new("instance-state-name"),
			Values: []string{
				string(types.InstanceStateNameRunning),
			},
		}
	case InstanceStart:
		stateFilter = types.Filter{
			Name: new("instance-state-name"),
			Values: []string{
				string(types.InstanceStateNameStopped),
			},
		}
	default:
		stateFilter = types.Filter{
			Name: new("instance-state-name"),
			Values: []string{
				string(types.InstanceStateNamePending),
				string(types.InstanceStateNameRunning),
				string(types.InstanceStateNameShuttingDown),
				string(types.InstanceStateNameStopping),
				string(types.InstanceStateNameStopped),
			},
		}
	}

	filters := []types.Filter{stateFilter}

	// Filter by tag type
	for tagKey, tagVal := range tags {
		newTagFilter := types.Filter{
			Name: new("tag:" + tagKey),
			Values: []string{
				tagVal,
			},
		}
		filters = append(filters, newTagFilter)
	}

	// Filter by instanceIDs
	if len(instanceIDs) != 0 {
		idFilter := types.Filter{
			Name:   new("instance-id"),
			Values: instanceIDs,
		}
		filters = append(filters, idFilter)
	}

	input := &ec2.DescribeInstancesInput{
		Filters: filters,
	}

	var reservations []types.Reservation
	pages := ec2.NewDescribeInstancesPaginator(svc, input)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return rSummary, err
		}
		reservations = append(reservations, page.Reservations...)
	}

	// Placement is already included in DescribeInstances. Only request spot details
	// when matching instances need them; on-demand fleets need no spot permission.
	var spotRequests []types.SpotInstanceRequest
	needsSpot := false
	for _, reservation := range reservations {
		for _, instance := range reservation.Instances {
			needsSpot = needsSpot || instance.SpotInstanceRequestId != nil
		}
	}
	if needsSpot {
		pages := ec2.NewDescribeSpotInstanceRequestsPaginator(svc, &ec2.DescribeSpotInstanceRequestsInput{})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				return rSummary, err
			}
			spotRequests = append(spotRequests, page.SpotInstanceRequests...)
		}
	}

	var instances []Instance

	for _, res := range reservations {
		for _, inst := range res.Instances {
			var instance Instance
			if inst.InstanceId != nil {
				instance.ID = *inst.InstanceId
			}
			if inst.State != nil {
				instance.Status = inst.State.Name
			}
			instance.Type = inst.InstanceType
			if inst.PrivateIpAddress != nil {
				instance.IP = *inst.PrivateIpAddress
			}
			if inst.HibernationOptions != nil && inst.HibernationOptions.Configured != nil {
				instance.Hibernation = *inst.HibernationOptions.Configured
			}
			instance.Region = region
			if inst.Placement != nil && inst.Placement.AvailabilityZone != nil {
				instance.AZ = *inst.Placement.AvailabilityZone
			}
			instance.SpotInstanceType = ""
			if inst.InstanceLifecycle == "" {
				instance.Lifecycle = string(types.InstanceLifecycleOnDemand)
			} else {
				instance.Lifecycle = string(inst.InstanceLifecycle)
				if inst.InstanceLifecycle == types.InstanceLifecycleTypeSpot && inst.SpotInstanceRequestId != nil {
					instance.SpotInstanceType = getSpotRequestType(spotRequests, inst.SpotInstanceRequestId)
				}
			}

			if inst.StateReason != nil && inst.StateReason.Code != nil && inst.State != nil {
				if *inst.StateReason.Code == "Client.UserInitiatedHibernate" && inst.State.Name == types.InstanceStateNameStopped {
					instance.Status = "hibernated"
				}
			}

			instance.Name = ""
			instance.Environment = ""
			for _, tag := range inst.Tags {
				if tag.Key == nil || tag.Value == nil {
					continue
				}
				switch *tag.Key {
				case "Name":
					instance.Name = *tag.Value
				case "Environment":
					instance.Environment = *tag.Value
				}
			}
			instances = append(instances, instance)
		}
	}

	slices.SortStableFunc(instances, func(a, b Instance) int {
		return cmp.Or(cmp.Compare(a.Environment, b.Environment), cmp.Compare(a.Name, b.Name))
	})

	rSummary.Instances = instances

	return rSummary, nil
}

// StartStopInstance starts or stops an AWS Instance
func StartStopInstance(
	ctx context.Context,
	region string,
	action string,
	instanceIDs []string,
) ([]types.InstanceStateChange, error) {
	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return nil, err
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	switch action {
	case InstanceStart:
		// We set DryRun to true to check to see if the instance exists, and we have the
		// necessary permissions to monitor the instance.
		input := &ec2.StartInstancesInput{
			InstanceIds: instanceIDs,
			DryRun:      new(true),
		}
		result, err := svc.StartInstances(ctx, input)
		// If the error code is `DryRunOperation` it means we have the necessary
		// permissions to Start this instance
		if err != nil {
			if ae, ok := errors.AsType[smithy.APIError](err); ok && ae.ErrorCode() == DryRunOperation {
				// Let's now set dry run to be false. This will allow us to start the instances
				input.DryRun = new(false)
				result, err = svc.StartInstances(ctx, input)
			}
		}
		if err != nil {
			return nil, err
		}
		return result.StartingInstances, nil

	case InstanceStop, InstanceHibernate:
		// Turn instances off
		input := &ec2.StopInstancesInput{
			InstanceIds: instanceIDs,
			DryRun:      new(true),
		}
		if action == InstanceHibernate {
			input.Hibernate = new(true)
		}
		result, err := svc.StopInstances(ctx, input)
		if err != nil {
			if ae, ok := errors.AsType[smithy.APIError](err); ok && ae.ErrorCode() == DryRunOperation {
				// Let's now set dry run to be false. This will allow us to start the instances
				input.DryRun = new(false)
				result, err = svc.StopInstances(ctx, input)
			}
		}
		if err != nil {
			return nil, err
		}
		return result.StoppingInstances, nil
	default:
		return nil, errors.New("invalid action")
	}
}

// ModifyInstanceType modifies an AWS Instance type
func ModifyInstanceType(
	ctx context.Context,
	region string,
	instanceType string,
	instanceID string,
) (err error) {
	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return err
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	// This modifies the instance type of the specified instance
	input := &ec2.ModifyInstanceAttributeInput{
		InstanceId: new(instanceID),
		InstanceType: &types.AttributeValue{
			Value: new(instanceType),
		},
		DryRun: new(true),
	}

	_, err = svc.ModifyInstanceAttribute(ctx, input)
	// If the error code is `DryRunOperation` it means we have the necessary
	// permissions to Start this instance
	if err != nil {
		if ae, ok := errors.AsType[smithy.APIError](err); ok && ae.ErrorCode() == DryRunOperation {
			// Let's now set dry run to be false. This will allow us to start the instances
			input.DryRun = new(false)
			_, err = svc.ModifyInstanceAttribute(ctx, input)
		}
	}

	return
}

func TerminateInstances(ctx context.Context, region string, instances []string) (err error) {
	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return err
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	_, err = svc.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instances,
	})
	return
}

func getSpotRequestType(requests []types.SpotInstanceRequest, id *string) types.SpotInstanceType {
	if i := slices.IndexFunc(requests, func(request types.SpotInstanceRequest) bool {
		return request.SpotInstanceRequestId != nil && id != nil && *request.SpotInstanceRequestId == *id
	}); i >= 0 {
		return requests[i].Type
	}
	return ""
}
