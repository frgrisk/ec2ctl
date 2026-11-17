package cmd

import (
	"slices"
	"testing"

	"github.com/frgrisk/ec2ctl/adapter/aws"
)

func TestResolveExplicitInstances(t *testing.T) {
	t.Parallel()
	const west = "us-west-2"
	first := aws.Instance{ID: "i-first", Region: west, Name: "original"}
	second := aws.Instance{ID: "i-second", Region: west}
	third := aws.Instance{ID: "i-third", Region: cliTestRegion}
	last := aws.Instance{ID: first.ID, Region: cliTestRegion, Name: "last match"}
	summary := aws.AccountSummary{
		{Region: "summary region is not the instance region", Instances: []aws.Instance{
			second, first, first, {ID: "i-unrequested", Region: west},
		}},
		{Region: cliTestRegion, Instances: []aws.Instance{third, last}},
	}
	for _, test := range []struct {
		name        string
		summary     aws.AccountSummary
		requested   []string
		wantOrdered []aws.Instance
		wantRegions []regionTargets
	}{
		{
			name:        "complete targets preserve both ordering and duplicate policies",
			summary:     summary,
			requested:   []string{third.ID, second.ID, first.ID, second.ID},
			wantOrdered: []aws.Instance{third, second, last, second},
			wantRegions: []regionTargets{
				{region: cliTestRegion, ids: []string{third.ID, first.ID}},
				{region: west, ids: []string{second.ID, first.ID, first.ID}},
			},
		},
		{
			name:        "empty selection does not select inventory",
			summary:     summary,
			requested:   []string{},
			wantOrdered: []aws.Instance{},
			wantRegions: []regionTargets{},
		},
		{
			name:        "empty inventory and selection",
			summary:     aws.AccountSummary{},
			requested:   []string{},
			wantOrdered: []aws.Instance{},
			wantRegions: []regionTargets{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			targets, err := resolveExplicitInstances(test.summary, test.requested)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(targets.ordered, test.wantOrdered) {
				t.Errorf("ordered=%v want=%v", targets.ordered, test.wantOrdered)
			}
			if len(targets.byRegion) != len(test.wantRegions) {
				t.Fatalf("regions=%v want=%v", targets.byRegion, test.wantRegions)
			}
			for i, want := range test.wantRegions {
				got := targets.byRegion[i]
				if got.region != want.region {
					t.Errorf("region=%q want=%q", got.region, want.region)
				}
				if !slices.Equal(got.ids, want.ids) {
					t.Errorf("ids=%v want=%v", got.ids, want.ids)
				}
			}
		})
	}
}

func TestResolveExplicitInstancesRejectsIncompleteSelection(t *testing.T) {
	t.Parallel()
	const found = "i-found"
	const missing = "i-missing"
	const otherMissing = "i-other-missing"
	summary := aws.AccountSummary{{Instances: []aws.Instance{{ID: found, Region: cliTestRegion}}}}
	for _, test := range []struct {
		name      string
		wantID    string
		summary   aws.AccountSummary
		requested []string
	}{
		{
			name:      "empty inventory",
			summary:   aws.AccountSummary{},
			requested: []string{found},
			wantID:    found,
		},
		{
			name:      "missing before found",
			summary:   summary,
			requested: []string{missing, found},
			wantID:    missing,
		},
		{
			name:      "missing after found",
			summary:   summary,
			requested: []string{found, missing},
			wantID:    missing,
		},
		{
			name:      "first missing follows argument order despite repeats",
			summary:   summary,
			requested: []string{found, found, otherMissing, missing, found},
			wantID:    otherMissing,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			targets, err := resolveExplicitInstances(test.summary, test.requested)
			if err == nil {
				t.Fatal("incomplete selection succeeded")
			}
			if want := "instance " + test.wantID + " not found; check --regions and --tag"; err.Error() != want {
				t.Errorf("error=%q want=%q", err, want)
			}
			if targets != nil {
				t.Errorf("incomplete selection returned partial targets: %+v", targets)
			}
		})
	}
}
