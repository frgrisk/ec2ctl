package aws

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"golang.org/x/term"
)

const minRenderedTableWidth = 200

// RegionSummary is a structure holding deployed instances in a given region
type RegionSummary struct {
	Region    string
	Instances []Instance
}

// AccountSummary is a structure holding a slice of regions summaries across an entire account
type AccountSummary []RegionSummary

// Print prints the summary of instances in an account in tabular format
func (u AccountSummary) Print() {
	for _, region := range u {
		region.Print()
	}
}

// Prompt prompts user for confirmation
func (u AccountSummary) Prompt(action string) AccountSummary {
	// If no region summary in account summary, means no matching instances, return err
	if len(u) == 0 {
		fmt.Println("No instances are available for " + action + " command.")
		os.Exit(0)
	}
	// If region summary exists in account summary, means there are matching instances, return as table
	for _, regionSum := range u {
		regionSum.Print()
	}
	var confirm bool
	err := huh.NewConfirm().
		Title("This action will " + action + " the instances above matching the filter. Would you like to proceed?").
		Value(&confirm).
		Affirmative("Yes").
		Negative("No").
		WithAccessible(os.Getenv("ACCESSIBLE") != "").
		Run()
	if err != nil {
		fmt.Println("cannot prompt for confirmation:", err)
		os.Exit(1)
	}

	// If user acknowledges, return account summary associated
	if confirm {
		return u
	}
	// Else, return empty
	return AccountSummary{}
}

// GetInstanceRegion returns the region of an instance given an account summary
func GetInstanceRegion(accSum AccountSummary, id string) (string, error) {
	for _, region := range accSum {
		for _, instance := range region.Instances {
			if instance.ID == id {
				return region.Region, nil
			}
		}
	}
	return "", errors.New("instance not found")
}

// Print prints the summary of instances in a given region in tabular format
func (u RegionSummary) Print() {
	WriteTable(u.Region, u.Instances)
}

// GetRegions is a function to retrieve all active regions in an account
func GetRegions() (regions []string) {
	ctx := context.TODO()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	cfg.DefaultsMode = aws.DefaultsModeCrossRegion
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	svc := ec2.NewFromConfig(cfg)
	input := &ec2.DescribeRegionsInput{
		Filters: []types.Filter{
			{
				Name: aws.String("opt-in-status"),
				Values: []string{
					"opted-in",
					"opt-in-not-required",
				},
			},
		},
	}

	result, err := svc.DescribeRegions(ctx, input)
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range result.Regions {
		regions = append(regions, *r.RegionName)
	}

	return regions
}

// Helper function to extract instance IDs from a slice of instances
func IDs(instances []Instance) []string {
	ids := make([]string, len(instances))
	for i, instance := range instances {
		ids[i] = instance.ID
	}
	return ids
}

func WriteTable(title string, data []Instance) {
	structFields := reflect.VisibleFields(reflect.TypeOf(data[0]))
	header := make([]string, 0, len(structFields))
	for _, f := range structFields {
		if f.Name == "Region" {
			continue
		}
		header = append(header, f.Name)
	}

	var md strings.Builder
	_, _ = md.WriteString(fmt.Sprintf("## %s\n", title))
	_, _ = md.WriteString(fmt.Sprintf("| %s |\n", strings.Join(header, " | ")))
	_, _ = md.WriteString(fmt.Sprintf("|%s\n", strings.Repeat(" --- |", len(header))))

	for _, o := range data {
		row := make([]string, 0, len(structFields))
		for _, f := range structFields {
			value := fmt.Sprintf("%v", reflect.ValueOf(o).FieldByName(f.Name).Interface())
			switch f.Name {
			case "Region":
				continue
			case "Name":
				row = append(row, fmt.Sprintf("**%s**", value))
			case "Status":
				switch o.Status {
				case types.InstanceStateNameRunning:
					row = append(row, fmt.Sprintf(":white_check_mark: **%s**", value))
				case types.InstanceStateNameStopped:
					row = append(row, fmt.Sprintf(":x: **%s**", value))
				case types.InstanceStateNamePending, types.InstanceStateNameStopping:
					row = append(row, fmt.Sprintf(":warning: **%s**", value))
				case types.InstanceStateNameTerminated:
					row = append(row, fmt.Sprintf(":skull: **%s**", value))
				default:
					row = append(row, value)
				}
			default:
				row = append(row, value)
			}
		}
		_, _ = md.WriteString(fmt.Sprintf("| %s |\n", strings.Join(row, " | ")))
	}
	width, _, _ := term.GetSize(int(os.Stdout.Fd()))
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
		glamour.WithTableWrap(false),
		glamour.WithWordWrap(min(width, minRenderedTableWidth)),
	)
	rendered, err := r.Render(md.String())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(rendered)
}
