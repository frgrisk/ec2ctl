package cmd

import (
	"cmp"
	"strings"

	"github.com/frgrisk/ec2ctl/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	exploreGroup = "explore"
	changeGroup  = "change"
	toolsGroup   = "tools"
)

func commandHelp(command *cobra.Command, _ []string) {
	sections := []terminal.HelpSection{{Text: cmp.Or(command.Long, command.Short)}}
	if command.Runnable() || command.HasAvailableSubCommands() {
		var usage []string
		if command.Runnable() {
			usage = append(usage, "  "+command.UseLine())
		}
		if command.HasAvailableSubCommands() {
			usage = append(usage, "  "+command.CommandPath()+" [command]")
		}
		sections = append(sections, terminal.HelpSection{Title: "Usage:", Literal: strings.Join(usage, "\n")})
	}
	if len(command.Aliases) > 0 {
		sections = append(sections, terminal.HelpSection{Title: "Aliases:", Text: "  " + command.NameAndAliases()})
	}
	sections = append(sections, commandHelpGroups(command)...)
	if command.HasExample() {
		sections = append(sections, terminal.HelpSection{Title: "Examples:", Literal: command.Example})
	}
	if command.HasAvailableLocalFlags() {
		sections = append(sections, flagHelpSection("Flags:", command.LocalFlags()))
	}
	if command.HasAvailableInheritedFlags() {
		sections = append(sections, flagHelpSection("Global flags:", command.InheritedFlags()))
	}
	if command.HasHelpSubCommands() {
		section := terminal.HelpSection{Title: "Additional help topics:"}
		for _, child := range command.Commands() {
			if child.IsAdditionalHelpTopicCommand() {
				section.Rows = append(section.Rows, [2]string{child.CommandPath(), child.Short})
			}
		}
		sections = append(sections, section)
	}
	if command.HasAvailableSubCommands() {
		sections = append(sections, terminal.HelpSection{Text: "Use a command's --help for flags and examples.", Literal: "  " + command.CommandPath() + " [command] --help"})
	}
	commandTerminal(command).Help(sections)
}

func commandHelpGroups(command *cobra.Command) []terminal.HelpSection {
	sections := make([]terminal.HelpSection, 0, len(command.Groups())+1)
	for _, group := range command.Groups() {
		sections = append(sections, terminal.HelpSection{Title: group.Title, Rows: commandHelpRows(command, group.ID)})
	}
	if rows := commandHelpRows(command, ""); len(rows) > 0 {
		title := "Available commands:"
		if len(command.Groups()) > 0 {
			title = "Additional commands:"
		}
		sections = append(sections, terminal.HelpSection{Title: title, Rows: rows})
	}
	return sections
}

func commandHelpRows(command *cobra.Command, groupID string) [][2]string {
	var rows [][2]string
	for _, child := range command.Commands() {
		if child.GroupID == groupID && (child.IsAvailableCommand() || child.Name() == "help") {
			rows = append(rows, [2]string{child.Name(), child.Short})
		}
	}
	return rows
}

func flagHelpSection(title string, flags *pflag.FlagSet) terminal.HelpSection {
	section := terminal.HelpSection{Title: title}
	// Keep pflag's canonical spelling, value syntax, defaults and deprecations.
	// Its unwrapped table separates each label from its description by 2+ spaces.
	for line := range strings.SplitSeq(strings.TrimRight(flags.FlagUsages(), "\n"), "\n") {
		label, description, ok := strings.Cut(strings.TrimLeft(line, " "), "  ")
		if ok && strings.HasPrefix(label, "-") {
			section.Rows = append(section.Rows, [2]string{label, strings.TrimSpace(description)})
		} else if len(section.Rows) > 0 {
			section.Rows[len(section.Rows)-1][1] += "\n" + strings.TrimSpace(line)
		}
	}
	return section
}
