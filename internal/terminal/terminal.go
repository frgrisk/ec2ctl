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
	"github.com/muesli/cancelreader"
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
func (t *Terminal) Confirm(ctx context.Context, title, description string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("confirmation cancelled: %w", err)
	}
	var confirmed bool
	prompt := huh.NewConfirm().Title(title).Description(description).
		Value(&confirmed).Affirmative("Yes").Negative("No")
	var err error
	if t.isAccessible {
		reader, readErr := cancelreader.NewReader(t.input)
		if readErr != nil {
			return false, fmt.Errorf("confirmation input: %w", readErr)
		}
		defer reader.Close()
		cancelled := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			reader.Cancel()
			close(cancelled)
		})
		defer func() {
			if !stop() {
				<-cancelled
			}
		}()
		err = prompt.RunAccessible(t.output, reader)
	} else {
		// Match huh.Run's single-field presentation while binding command streams.
		err = huh.NewForm(huh.NewGroup(prompt)).WithShowHelp(false).
			WithInput(t.input).WithOutput(t.errors).RunWithContext(ctx)
	}
	if ctx.Err() != nil {
		err = ctx.Err()
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
func (t *Terminal) Progress(ctx context.Context, title string, action func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.canAnimate {
		return action(ctx)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var actionErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		actionErr = action(ctx)
	}()
	err := spinner.New().Title(title).Output(t.errors).Context(ctx).
		ActionWithErr(func(context.Context) error {
			<-done
			return actionErr
		}).Run()
	// The spinner can exit on a signal before its action returns. Cancel the
	// action and join it before callers read its results or leave the command.
	cancel()
	<-done
	if actionErr != nil {
		return actionErr
	}
	return err
}
