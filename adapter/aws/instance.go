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
	// InstanceHibernate is the action to hibernate an instance
	InstanceHibernate string = "hibernate"
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
	Hibernation      bool
	SpotInstanceType types.SpotInstanceType
	Region           string
	AZ               string
}

// GetDeployedInstances retrieves the status of all deployed instances in a given region
func GetDeployedInstances(region string, c chan RegionSummary) {

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

	svc := ec2.NewFromConfig(cfg)

	var rSummary RegionSummary
	rSummary.Region = region

	// TODO: Add support for multiple filters
	tagFilter := types.Filter{}
	stateFilter := types.Filter{
		Name: aws.String("instance-state-name"),
		Values: []string{
			string(types.InstanceStateNamePending),
			string(types.InstanceStateNameRunning),
			string(types.InstanceStateNameShuttingDown),
			string(types.InstanceStateNameStopping),
			string(types.InstanceStateNameStopped),
		},
	}

	input := &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			tagFilter,
			stateFilter,
		},
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
			instance.ID = *inst.InstanceId
			instance.Status = inst.State.Name
			instance.Type = inst.InstanceType
			instance.IP = *inst.PrivateIpAddress
			instance.Hibernation = *inst.HibernationOptions.Configured
			instance.Region = region
			instance.AZ = getInstanceAZ(resultStatus.InstanceStatuses, inst.InstanceId)
			if inst.InstanceLifecycle == "" {
				instance.Lifecycle = string(types.InstanceLifecycleOnDemand)
			} else {
				instance.Lifecycle = string(inst.InstanceLifecycle)
				if inst.InstanceLifecycle == types.InstanceLifecycleTypeSpot {
					instance.SpotInstanceType = getSpotRequestType(spotDetail.SpotInstanceRequests, inst.SpotInstanceRequestId)
				}
			}

			if inst.StateReason != nil {
				switch *inst.StateReason.Code {
				case "Client.UserInitiatedHibernate":
					if inst.State.Name == types.InstanceStateNameStopped {
						instance.Status = "hibernated"
					}
				}
			}

			for _, tag := range inst.Tags {
				if *tag.Key == "Name" {
					instance.Name = *tag.Value
				} else if *tag.Key == "Environment" {
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
func StartStopInstance(region string, action string, instanceID string) ([]types.InstanceStateChange, error) {

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
			InstanceIds: []string{
				instanceID,
			},
			DryRun: aws.Bool(true),
		}
		result, err := svc.StartInstances(ctx, input)

		// If the error code is `DryRunOperation` it means we have the necessary
		// permissions to Start this instance
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "DryRunOperation" {
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
			InstanceIds: []string{
				instanceID,
			},
			DryRun: aws.Bool(true),
		}
		if action == InstanceHibernate {
			input.Hibernate = aws.Bool(true)
		}
		result, err := svc.StopInstances(ctx, input)
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "DryRunOperation" {
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

	//This modifies the instance type of the specified instance
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
			if ae.ErrorCode() == "DryRunOperation" {
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
