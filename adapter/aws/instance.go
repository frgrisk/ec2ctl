package aws

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
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
func GetDeployedInstances(c chan RegionSummary, region string, tags map[string]string, action string, instanceIDs []string) {
	ctx := context.TODO()
	var rSummary RegionSummary
	rSummary.Region = region

	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatal(err)
	}

	svc := ec2.NewFromConfig(cfg)

	// Filter by state type
	var stateFilter types.Filter
	switch action {
	case InstanceStop:
		stateFilter = types.Filter{
			Name: aws.String("instance-state-name"),
			Values: []string{
				string(types.InstanceStateNameRunning),
			},
		}
	case InstanceStart:
		stateFilter = types.Filter{
			Name: aws.String("instance-state-name"),
			Values: []string{
				string(types.InstanceStateNameStopped),
			},
		}
	default:
		stateFilter = types.Filter{
			Name: aws.String("instance-state-name"),
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
			Name: aws.String("tag:" + tagKey),
			Values: []string{
				tagVal,
			},
		}
		filters = append(filters, newTagFilter)
	}

	// Filter by instanceIDs
	if len(instanceIDs) != 0 {
		idFilter := types.Filter{
			Name:   aws.String("instance-id"),
			Values: instanceIDs,
		}
		filters = append(filters, idFilter)
	}

	input := &ec2.DescribeInstancesInput{
		Filters: filters,
	}

	result, err := svc.DescribeInstances(ctx, input)
	if err != nil {
		fmt.Println(err.Error())
		c <- rSummary
		return
	}

	inputStatus := &ec2.DescribeInstanceStatusInput{
		Filters: []types.Filter{
			stateFilter,
		},
		IncludeAllInstances: aws.Bool(true),
	}

	resultStatus, err := svc.DescribeInstanceStatus(ctx, inputStatus)
	if err != nil {
		fmt.Println(err.Error())
		c <- rSummary
		return
	}

	spotDetail, err := svc.DescribeSpotInstanceRequests(ctx, &ec2.DescribeSpotInstanceRequestsInput{})
	if err != nil {
		fmt.Println(err.Error())
		c <- rSummary
		return
	}

	var instances []Instance
	var instance Instance

	for _, res := range result.Reservations {
		for _, inst := range res.Instances {
			if inst.InstanceId != nil {
				instance.ID = *inst.InstanceId
			}
			instance.Status = inst.State.Name
			instance.Type = inst.InstanceType
			if inst.PrivateIpAddress != nil {
				instance.IP = *inst.PrivateIpAddress
			}
			if inst.HibernationOptions != nil && inst.HibernationOptions.Configured != nil {
				instance.Hibernation = *inst.HibernationOptions.Configured
			}
			instance.Region = region
			if inst.InstanceId != nil {
				instance.AZ = getInstanceAZ(resultStatus.InstanceStatuses, inst.InstanceId)
			}
			instance.SpotInstanceType = ""
			if inst.InstanceLifecycle == "" {
				instance.Lifecycle = string(types.InstanceLifecycleOnDemand)
			} else {
				instance.Lifecycle = string(inst.InstanceLifecycle)
				if inst.InstanceLifecycle == types.InstanceLifecycleTypeSpot && inst.SpotInstanceRequestId != nil {
					instance.SpotInstanceType = getSpotRequestType(spotDetail.SpotInstanceRequests, inst.SpotInstanceRequestId)
				}
			}

			if inst.StateReason != nil {
				if *inst.StateReason.Code == "Client.UserInitiatedHibernate" && inst.State.Name == types.InstanceStateNameStopped {
					instance.Status = "hibernated"
				}
			}

			instance.Name = ""
			instance.Environment = ""
			for _, tag := range inst.Tags {
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

	sort.SliceStable(instances, func(i, j int) bool {
		if instances[i].Environment < instances[j].Environment {
			return true
		} else if instances[i].Environment > instances[j].Environment {
			return false
		}
		return instances[i].Name < instances[j].Name
	})

	rSummary.Instances = instances

	c <- rSummary
}

// StartStopInstance starts or stops an AWS Instance
func StartStopInstance(region string, action string, instanceIDs []string) ([]types.InstanceStateChange, error) {
	ctx := context.TODO()
	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	switch action {
	case InstanceStart:
		// We set DryRun to true to check to see if the instance exists, and we have the
		// necessary permissions to monitor the instance.
		input := &ec2.StartInstancesInput{
			InstanceIds: instanceIDs,
			DryRun:      aws.Bool(true),
		}
		result, err := svc.StartInstances(ctx, input)
		// If the error code is `DryRunOperation` it means we have the necessary
		// permissions to Start this instance
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == DryRunOperation {
					// Let's now set dry run to be false. This will allow us to start the instances
					input.DryRun = aws.Bool(false)
					result, err = svc.StartInstances(ctx, input)
				}
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
			DryRun:      aws.Bool(true),
		}
		if action == InstanceHibernate {
			input.Hibernate = aws.Bool(true)
		}
		result, err := svc.StopInstances(ctx, input)
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == DryRunOperation {
					// Let's now set dry run to be false. This will allow us to start the instances
					input.DryRun = aws.Bool(false)
					result, err = svc.StopInstances(ctx, input)
				}
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
func ModifyInstanceType(region string, instanceType string, instanceID string) (err error) {
	ctx := context.TODO()

	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	// This modifies the instance type of the specified instance
	input := &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(instanceID),
		InstanceType: &types.AttributeValue{
			Value: aws.String(instanceType),
		},
		DryRun: aws.Bool(true),
	}

	_, err = svc.ModifyInstanceAttribute(ctx, input)
	// If the error code is `DryRunOperation` it means we have the necessary
	// permissions to Start this instance
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == DryRunOperation {
				// Let's now set dry run to be false. This will allow us to start the instances
				input.DryRun = aws.Bool(false)
				_, err = svc.ModifyInstanceAttribute(ctx, input)
			}
		}
	}

	return
}

func TerminateInstances(region string, instances []string) (err error) {
	ctx := context.TODO()

	// Config sources can be passed to LoadDefaultConfig, these sources can implement
	// one or more provider interfaces. These sources take priority over the standard
	// environment and shared configuration values.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Create new EC2 client
	svc := ec2.NewFromConfig(cfg)

	_, err = svc.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instances,
	})
	return
}

func getSpotRequestType(requests []types.SpotInstanceRequest, id *string) types.SpotInstanceType {
	for _, request := range requests {
		if *request.SpotInstanceRequestId == *id {
			return request.Type
		}
	}
	return ""
}

func getInstanceAZ(statuses []types.InstanceStatus, id *string) string {
	for _, instance := range statuses {
		if *instance.InstanceId == *id {
			return *instance.AvailabilityZone
		}
	}
	return ""
}
