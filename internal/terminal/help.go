package terminal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

const (
	defaultHelpWidth        = 80
	helpIndent              = 2
	minHelpDescriptionWidth = 24
)

// HelpSection contains prose, literal syntax, or label/description rows.
// Literal lines stay intact so usage and examples remain shell-copyable.
type HelpSection struct {
	Title   string
	Text    string
	Literal string
	Rows    [][2]string
}

// Help renders command metadata using the output writer's terminal capabilities.
// Redirected writers use a stable width, independently of input capability.
func (t *Terminal) Help(sections []HelpSection) {
	width := defaultHelpWidth
	if fd, ok := t.output.(interface{ Fd() uintptr }); ok && isTerminal(t.output) {
		if w, _, err := term.GetSize(int(fd.Fd())); err == nil && w > 0 {
			width = w
		}
	}
	for i, section := range sections {
		if i > 0 {
			fmt.Fprintln(t.output)
		}
		if section.Title != "" {
			fmt.Fprintln(t.output, paint(section.Title, "1", t.isColorEnabled))
		}
		if section.Text != "" {
			for line := range strings.SplitSeq(section.Text, "\n") {
				// Cobra's built-in completion help embeds indented shell snippets.
				if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
					fmt.Fprintln(t.output, line)
				} else {
					t.helpText(line, "", width)
				}
			}
		}
		if section.Literal != "" {
			for line := range strings.SplitSeq(section.Literal, "\n") {
				if comment, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
					t.helpText(comment, "  # ", width)
				} else {
					fmt.Fprintln(t.output, line)
				}
			}
		}
		t.helpRows(section.Rows, width)
	}
}

func (t *Terminal) helpRows(rows [][2]string, width int) {
	labelWidth := 0
	for _, row := range rows {
		labelWidth = max(labelWidth, ansi.StringWidth(row[0]))
	}
	descriptionColumn := helpIndent + labelWidth + helpIndent
	for _, row := range rows {
		if row[1] == "" {
			fmt.Fprintln(t.output, "  "+row[0])
			continue
		}
		if width-descriptionColumn < minHelpDescriptionWidth {
			fmt.Fprintln(t.output, "  "+row[0])
			t.helpText(row[1], "    ", width)
			continue
		}
		padding := strings.Repeat(" ", labelWidth-ansi.StringWidth(row[0])+helpIndent)
		prefix := "  " + row[0] + padding
		t.helpText(row[1], prefix, width)
	}
}

// Wrap at word boundaries without splitting flag tokens or long example values.
func (t *Terminal) helpText(text, prefix string, width int) {
	if text == "" {
		return
	}
	indent := strings.Repeat(" ", ansi.StringWidth(prefix))
	if strings.HasSuffix(prefix, "# ") {
		indent = prefix
	}
	for paragraph := range strings.SplitSeq(text, "\n") {
		line := prefix
		occupied := false
		for word := range strings.FieldsSeq(paragraph) {
			separator := ""
			if occupied {
				separator = " "
			}
			if occupied && ansi.StringWidth(line+separator+word) > width {
				fmt.Fprintln(t.output, line)
				line, separator = indent, ""
			}
			line += separator + word
			occupied = true
		}
		fmt.Fprintln(t.output, strings.TrimRight(line, " "))
		prefix = indent
	}
}
