package cmd

import (
	"fmt"
	"maps"
	"slices"

	"github.com/frgrisk/ec2ctl/adapter/aws"
)

type explicitInstances struct {
	ordered  []aws.Instance
	byRegion []regionTargets
}

type regionTargets struct {
	region string
	ids    []string
}

// resolveExplicitInstances returns no targets unless every requested ID exists.
// The ordered view preserves argument order and duplicates, using the last
// inventory match for each ID. The regional view sorts regions and retains all
// matching inventory entries in inventory order, independent of argument repeats.
// Both views associate targets with Instance.Region, not RegionSummary.Region.
func resolveExplicitInstances(summary aws.AccountSummary, requested []string) (*explicitInstances, error) {
	byID := make(map[string]*aws.Instance, len(requested))
	for _, id := range requested {
		byID[id] = nil
	}
	byRegion := make(map[string][]string)
	for _, region := range summary {
		for n, instance := range region.Instances {
			if _, ok := byID[instance.ID]; !ok {
				continue
			}
			byID[instance.ID] = &region.Instances[n]
			byRegion[instance.Region] = append(byRegion[instance.Region], instance.ID)
		}
	}

	targets := &explicitInstances{
		ordered:  make([]aws.Instance, 0, len(requested)),
		byRegion: make([]regionTargets, 0, len(byRegion)),
	}
	for _, id := range requested {
		instance := byID[id]
		if instance == nil {
			return nil, fmt.Errorf("instance %s not found; check --regions and --tag", id)
		}
		targets.ordered = append(targets.ordered, *instance)
	}
	for _, region := range slices.Sorted(maps.Keys(byRegion)) {
		targets.byRegion = append(targets.byRegion, regionTargets{region: region, ids: byRegion[region]})
	}
	return targets, nil
}
