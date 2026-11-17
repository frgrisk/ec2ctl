//go:build unix

package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
)

func TestFleetLayout(t *testing.T) {
	summary := aws.AccountSummary{{Region: testRegion, Instances: []aws.Instance{{
		Name:        "a very long instance name with 日本語 and extra text",
		ID:          testInstanceID,
		Status:      runningState,
		Type:        "t3.small",
		IP:          "10.0.1.12",
		Environment: "development",
		AZ:          "us-east-1a",
		Lifecycle:   "on-demand",
		Hibernation: true,
	}}}}
	for _, width := range []uint16{40, 80, 104, 120} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/wide=%t", width, wide), func(t *testing.T) {
				ui, out := fleetTerminal(t, width, false)
				ui.Fleet(summary, wide)
				text := out.String()
				for _, want := range []string{"EC2 FLEET", "1 instance", "1 running", testRegion, testInstanceID, "10.0.1.12"} {
					if !strings.Contains(text, want) {
						t.Errorf("missing %q: %s", want, text)
					}
				}
				for line := range strings.SplitSeq(text, "\n") {
					if got := ansi.StringWidth(line); got > int(width) {
						t.Errorf(
							"line width %d exceeds %d: %s",
							got,
							width,
							line,
						)
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
	summary := aws.AccountSummary{{
		Region: testRegion,
		Instances: []aws.Instance{{
			Name:   "bad\x1b[31mname\nnext\tfield",
			Status: runningState,
		}},
	}}
	ui, out := fleetTerminal(t, 120, false)
	ui.Fleet(summary, false)
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "badname next field") {
		t.Fatalf("unsafe output: %q", out.String())
	}
	ui, out = fleetTerminal(t, 120, true)
	ui.Fleet(summary, false)
	if !strings.Contains(out.String(), "\x1b[32m") {
		t.Error("running instances should be green in color mode")
	}
}

func TestFleetAdaptsToResize(t *testing.T) {
	ui, out := fleetTerminal(t, 120, false)
	summary := aws.AccountSummary{{Region: testRegion, Instances: []aws.Instance{{ID: testInstanceID}}}}
	ui.Fleet(summary, false)
	if !strings.Contains(out.String(), "INSTANCE ID") {
		t.Fatalf("expected table: %q", out.String())
	}
	if err := pty.Setsize(out.file, &pty.Winsize{Rows: 40, Cols: 40}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	ui.Fleet(summary, false)
	if strings.Contains(out.String(), "INSTANCE ID") || !strings.Contains(out.String(), "  IP") {
		t.Fatalf("expected narrow cards after resize: %q", out.String())
	}
}

func fleetTerminal(t *testing.T, width uint16, color bool) (*Terminal, *terminalBuffer) {
	t.Helper()
	t.Setenv("TERM", "screen-256color")
	t.Setenv("ACCESSIBLE", "")
	t.Setenv("NO_COLOR", "")
	if !color {
		t.Setenv("NO_COLOR", "1")
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: width}); err != nil {
		t.Fatal(err)
	}
	output := &terminalBuffer{file: slave}
	return New(slave, output, output), output
}
