package cmd

import (
	"context"
	"os"

	"github.com/charmbracelet/huh/spinner"
	"golang.org/x/term"
)

// Animation belongs on a terminal, never in JSON or redirected output.
func progress(title string, action func() error) error {
	if !term.IsTerminal(int(os.Stderr.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("ACCESSIBLE") != "" || os.Getenv("TERM") == "dumb" || os.Getenv("NO_COLOR") != "" {
		return action()
	}
	return spinner.New().Title(title).Output(os.Stderr).
		ActionWithErr(func(_ context.Context) error { return action() }).Run()
}
