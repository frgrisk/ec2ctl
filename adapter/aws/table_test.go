package aws

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const testRegion = "us-east-1"

func TestFleetLayout(t *testing.T) {
	summary := AccountSummary{{Region: testRegion, Instances: []Instance{{
		Name: "a very long instance name with 日本語 and extra text", ID: "i-0123456789abcdef0", Status: "running",
		Type: "t3.small", IP: "10.0.1.12", Environment: "development", AZ: "us-east-1a", Lifecycle: "on-demand", Hibernation: true,
	}}}}
	for _, width := range []int{40, 80, 104, 120} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/wide=%t", width, wide), func(t *testing.T) {
				var out bytes.Buffer
				renderFleet(&out, summary, width, false, wide)
				text := out.String()
				for _, want := range []string{"EC2 FLEET", "1 instance", "1 running", testRegion, "i-0123456789abcdef0", "10.0.1.12"} {
					if !strings.Contains(text, want) {
						t.Errorf("missing %q: %s", want, text)
					}
				}
				for line := range strings.SplitSeq(text, "\n") {
					if got := ansi.StringWidth(line); got > width {
						t.Errorf("line width %d exceeds %d: %s", got, width, line)
					}
				}
				if strings.Contains(text, "\x1b") {
					t.Error("plain output contains terminal escapes")
				}
				if wide && !strings.Contains(text, "Hibernation enabled  true") {
					t.Error("wide output missing details")
				}
			})
		}
	}
}

func TestFleetSanitizesTagValues(t *testing.T) {
	summary := AccountSummary{{Region: testRegion, Instances: []Instance{{Name: "bad\x1b[31mname\nnext\tfield", Status: "running"}}}}
	var out bytes.Buffer
	renderFleet(&out, summary, 120, false, false)
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "badname next field") {
		t.Fatalf("unsafe output: %q", out.String())
	}
	out.Reset()
	renderFleet(&out, summary, 120, true, false)
	if !strings.Contains(out.String(), "\x1b[32m") {
		t.Error("running instances should be green in color mode")
	}
}
