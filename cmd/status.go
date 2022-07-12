/*
Copyright © 2021 FRG

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "List available instances and their statuses",
	Long:  `This command lists all available instances and their statuses.`,
	Example: `
  # Query all regions
  ec2ctl status
  # Query specific regions
  ec2ctl status --regions us-east-1,ap-southeast-1
`,
	Run: func(cmd *cobra.Command, args []string) {
		p := tea.NewProgram(initialModel(regions))
		if err := p.Start(); err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
	},
}

// Style definitions.
var (

	// General.

	subtle    = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}

	divider = lipgloss.NewStyle().
		SetString("•").
		Padding(0, 1).
		Foreground(subtle).
		String()

	url = lipgloss.NewStyle().Foreground(special).Render

	// Tabs.

	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}

	tabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	tab = lipgloss.NewStyle().
		Border(tabBorder, true).
		BorderForeground(highlight).
		Padding(0, 1)

	activeTab = tab.Copy().Border(activeTabBorder, true)

	tabGap = tab.Copy().
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false)
)

func getAccountSummary(regions []string) (accSum aws.AccountSummary) {
	if len(regions) == 0 {
		regions = aws.GetRegions()
	}

	c := make(chan aws.RegionSummary)
	for _, r := range regions {
		go aws.GetDeployedInstances(r, c)
	}
	var regSum aws.RegionSummary

	for range regions {
		regSum = <-c
		if len(regSum.Instances) > 0 {
			accSum = append(accSum, regSum)
		}
	}
	return
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type model struct {
	summary  aws.AccountSummary
	cursor   struct{ region, instance int }
	selected map[struct{ region, instance int }]struct{}
	spinner  spinner.Model
}

func initialModel(regions []string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return model{
		// Our shopping list is a grocery list
		summary: getAccountSummary(regions),

		// A map which indicates which choices are selected. We're using
		// the  map like a mathematical set. The keys refer to the indexes
		// of the `choices` slice, above.
		selected: make(map[struct{ region, instance int }]struct{}),
		spinner:  s,
	}
}

func (m model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	var cmd tea.Cmd

	switch msg := msg.(type) {

	// Is it a key press?
	case tea.KeyMsg:

		// Cool, what was the actual key pressed?
		switch msg.String() {

		// These keys should exit the program.
		case "ctrl+c", "q":
			return m, tea.Quit

		// The "up" and "k" keys move the cursor up
		case "up", "k":
			if m.cursor.instance > 0 {
				m.cursor.instance--
			}

		// The "down" and "j" keys move the cursor down
		case "down", "j":
			if m.cursor.instance < len(m.summary[m.cursor.region].Instances)-1 {
				m.cursor.instance++
			}

		// The "left" and "k" keys move the cursor up
		case "left", "h", "shift+tab":
			m.cursor.instance = 0
			if m.cursor.region > 0 {
				m.cursor.region--
			} else {
				m.cursor.region = len(m.summary) - 1
			}

		// The "right" and "j" keys move the cursor down
		case "right", "l", "tab":
			m.cursor.instance = 0
			if m.cursor.region < len(m.summary)-1 {
				m.cursor.region++
			} else {
				m.cursor.region = 0
			}

		// The "enter" key and the spacebar (a literal space) toggle
		// the selected state for the item that the cursor is pointing at.
		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}

		case "r":
			var regions []string
			for _, r := range m.summary {
				regions = append(regions, r.Region)
			}
			refresh := initialModel(regions)
			m.summary = refresh.summary
			m.selected = refresh.selected
		}
		tea.Batch()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, cmd
}

func (m model) View() string {
	// The header
	s := ""

	width, _, _ := term.GetSize(int(os.Stdout.Fd()))
	doc := strings.Builder{}

	// Iterate over our regions
	var tabs []string
	for i, region := range m.summary {

		var t string
		// if current region is selected
		if m.cursor.region == i {
			t = activeTab.Render(region.Region)
			for j, instance := range region.Instances {
				cursor := " "
				if m.cursor.instance == j {
					cursor = ">" // cursor!
				}
				// Is this instance selected?
				checked := " " // not selected
				if _, ok := m.selected[struct{ region, instance int }{region: i, instance: j}]; ok {
					checked = "x" // selected!
				}
				// Render the row
				s += fmt.Sprintf("%s [%s] %s %s %s\n", cursor, checked, instance.Name, instance.Status, m.spinner.View())
			}
		} else {
			t = tab.Render(region.Region)
		}
		tabs = append(tabs, t)
	}

	// Tabs
	{
		row := lipgloss.JoinHorizontal(
			lipgloss.Top,
			tabs...,
		)
		gap := tabGap.Render(strings.Repeat(" ", max(0, width-lipgloss.Width(row)-2)))
		row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
		doc.WriteString(row + "\n\n" + s)
	}

	// The footer
	s += "\nPress q to quit.\n"

	// Send the UI for rendering
	return doc.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
