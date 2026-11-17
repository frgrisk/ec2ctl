package terminal

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/frgrisk/ec2ctl/adapter/aws"
)

const (
	defaultTableWidth = 120
	compactTableWidth = 104
	maxNameWidth      = 36
	fixedColumnWidth  = 81
	stateColumn       = 2
)

func countLabel(count int, noun string) string {
	if count != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", count, noun)
}

func paint(value, code string, color bool) string {
	if !color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

// Tag values are untrusted display text: remove terminal controls and line breaks.
func display(value string) string {
	return cmp.Or(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(value)), "—")
}

func stateColor(state string) string {
	switch state {
	case "running":
		return "32"
	case "stopped", "hibernated":
		return "90"
	case "pending", "stopping":
		return "33"
	case "terminated", "shutting-down":
		return "31"
	default:
		return "36"
	}
}

func renderFleet(w io.Writer, summary aws.AccountSummary, width int, color, wide bool) {
	total := 0
	counts := make(map[string]int)
	for _, region := range summary {
		total += len(region.Instances)
		for _, instance := range region.Instances {
			counts[string(instance.Status)]++
		}
	}
	line := func(value string) { fmt.Fprintln(w, ansi.Hardwrap(value, max(1, width), true)) }
	line(paint("EC2 FLEET", "1;36", color))
	line(countLabel(total, "instance") + " · " + countLabel(len(summary), "region"))
	states := make([]string, 0, len(counts))
	for _, state := range slices.Sorted(maps.Keys(counts)) {
		states = append(states, paint(fmt.Sprintf("%d %s", counts[state], display(state)), stateColor(state), color))
	}
	line(strings.Join(states, "  ·  "))
	for _, region := range summary {
		line("")
		line(paint(display(region.Region)+" / "+countLabel(len(region.Instances), "instance"), "1;36", color))
		if width >= compactTableWidth && !wide {
			widths := []int{min(maxNameWidth, width-fixedColumnWidth), 19, 13, 12, 15, 12}
			row := func(values []string, heading bool) {
				cells := make([]string, len(values))
				for i, value := range values {
					value = ansi.Truncate(display(value), widths[i], "…")
					cell := value + strings.Repeat(" ", max(0, widths[i]-ansi.StringWidth(value)))
					if heading {
						cell = paint(cell, "1;90", color)
					} else if i == stateColumn {
						cell = paint(cell, stateColor(values[i]), color)
					}
					cells[i] = cell
				}
				fmt.Fprintln(w, strings.TrimRight(strings.Join(cells, "  "), " "))
			}
			row([]string{"NAME", "INSTANCE ID", "STATE", "TYPE", "PRIVATE IP", "ENV"}, true)
			for _, instance := range region.Instances {
				row([]string{instance.Name, instance.ID, string(instance.Status), string(instance.Type), instance.IP, instance.Environment}, false)
			}
			continue
		}
		for _, instance := range region.Instances {
			line("")
			line(paint(display(instance.Name), "1", color))
			line("  " + display(instance.ID))
			line("  " + paint(display(string(instance.Status)), stateColor(string(instance.Status)), color) + " · " + display(string(instance.Type)))
			line("  IP   " + display(instance.IP))
			line("  Env  " + display(instance.Environment))
			if wide {
				line("  AZ   " + display(instance.AZ))
				line("  Lifecycle  " + display(instance.Lifecycle))
				line("  Spot type  " + display(string(instance.SpotInstanceType)))
				line(fmt.Sprintf("  Hibernation enabled  %t", instance.Hibernation))
			}
		}
	}
	fmt.Fprintln(w)
}
