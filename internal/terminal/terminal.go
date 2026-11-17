// Package terminal owns human interaction with the CLI's input, output and error
// streams. AWS discovery and lifecycle decisions stay with its callers.
package terminal

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"golang.org/x/term"
)

// Terminal binds terminal policy to the streams used by one command.
// Input capability controls confirmation safety independently of presentation.
type Terminal struct {
	input          io.Reader
	output         io.Writer
	errors         io.Writer
	isInputTTY     bool
	isColorEnabled bool
	canAnimate     bool
	isAccessible   bool
}

// New captures stream capabilities and environment policy for a command.
// Readers/writers without a file descriptor are treated as redirected streams.
func New(input io.Reader, output, errors io.Writer) *Terminal {
	accessible := os.Getenv("ACCESSIBLE") != "" || os.Getenv("TERM") == "dumb"
	plain := accessible || os.Getenv("NO_COLOR") != ""
	outputTTY := isTerminal(output)
	return &Terminal{
		input:          input,
		output:         output,
		errors:         errors,
		isInputTTY:     isTerminal(input),
		isAccessible:   accessible,
		isColorEnabled: outputTTY && !plain,
		canAnimate:     outputTTY && isTerminal(errors) && !plain,
	}
}

func isTerminal(stream any) bool {
	fd, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(fd.Fd()))
}

// RequireConfirmation rejects unsafe noninteractive lifecycle calls. Call it
// before discovery, so refusal cannot send even a read-only AWS request.
func (t *Terminal) RequireConfirmation(action string, force bool) error {
	if !force && !t.isInputTTY {
		return fmt.Errorf("cannot confirm %s without terminal input; use --force (-f) to skip confirmation", action)
	}
	return nil
}

// Confirm asks one question. Callers own selection and confirmation grouping.
// Accessible prompts use stdout; interactive forms use stderr, as before.
func (t *Terminal) Confirm(title, description string) (bool, error) {
	var confirmed bool
	prompt := huh.NewConfirm().Title(title).Description(description).
		Value(&confirmed).Affirmative("Yes").Negative("No")
	var err error
	if t.isAccessible {
		err = prompt.RunAccessible(t.output, t.input)
	} else {
		// Match huh.Run's single-field presentation while binding command streams.
		err = huh.NewForm(huh.NewGroup(prompt)).WithShowHelp(false).
			WithInput(t.input).WithOutput(t.errors).Run()
	}
	if err != nil {
		return false, fmt.Errorf("confirmation cancelled: %w", err)
	}
	return confirmed, nil
}

// Fleet displays the summary using the output stream's width and color policy.
func (t *Terminal) Fleet(summary aws.AccountSummary, wide bool) {
	width := defaultTableWidth
	if fd, ok := t.output.(interface{ Fd() uintptr }); ok {
		if w, _, err := term.GetSize(int(fd.Fd())); err == nil && w > 0 {
			width = w
		}
	}
	renderFleet(
		t.output,
		summary,
		width,
		t.isColorEnabled,
		wide,
	)
}

// Printf writes human-readable command results to the command's output stream.
func (t *Terminal) Printf(format string, args ...any) {
	fmt.Fprintf(t.output, format, args...)
}

// Progress runs action once, with animation only when both output streams are
// terminals and the environment permits it. Animation never goes to stdout.
func (t *Terminal) Progress(title string, action func() error) error {
	if !t.canAnimate {
		return action()
	}
	return spinner.New().Title(title).Output(t.errors).
		ActionWithErr(func(_ context.Context) error { return action() }).Run()
}
