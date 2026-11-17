//go:build unix

package terminal

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
)

// Keep the actual file descriptor for capability detection while capturing
// writes in memory. No replacement of process-wide streams is needed.
type terminalBuffer struct {
	file *os.File
	bytes.Buffer
}

func (b *terminalBuffer) Fd() uintptr { return b.file.Fd() }

const (
	normalMode       = "normal"
	redirectedErrors = "redirected-errors"
	redirectedOutput = "redirected-output"
)

func TestTerminalPresentationPolicy(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 40}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{normalMode, "no-color", "accessible", "dumb", redirectedOutput, redirectedErrors} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("TERM", "screen-256color")
			t.Setenv("ACCESSIBLE", "")
			t.Setenv("NO_COLOR", "")
			switch mode {
			case "no-color":
				t.Setenv("NO_COLOR", "1")
			case "accessible":
				t.Setenv("ACCESSIBLE", "1")
			case "dumb":
				t.Setenv("TERM", "dumb")
			}
			output := &terminalBuffer{file: slave}
			diagnostics := &terminalBuffer{file: slave}
			ui := New(slave, output, diagnostics)
			if mode == redirectedOutput {
				ui = New(slave, &output.Buffer, diagnostics)
			}
			if mode == redirectedErrors {
				ui = New(slave, output, &diagnostics.Buffer)
			}
			if err := ui.RequireConfirmation("stop", false); err != nil {
				t.Fatalf("terminal input refused: %v", err)
			}

			if err := New(strings.NewReader("yes\n"), output, diagnostics).RequireConfirmation("stop", false); err == nil {
				t.Fatal("terminal output bypassed nonterminal input refusal")
			}
			ui.Fleet(aws.AccountSummary{{Region: testRegion, Instances: []aws.Instance{{Name: "test", Status: runningState, ID: testInstanceID}}}}, false)
			wantColor := mode == normalMode || mode == redirectedErrors
			if strings.Contains(output.String(), "\x1b") != wantColor {
				t.Fatalf("color policy: %q", output.String())
			}
			if mode != redirectedOutput && !strings.Contains(output.String(), "  IP") {
				t.Fatalf("narrow output should use cards: %q", output.String())
			}
			// All disabled modes must run the action exactly once and write no animation.
			if mode != normalMode {
				calls := 0
				if err := ui.Progress("scanning", func() error { calls++; return nil }); err != nil || calls != 1 || diagnostics.Len() != 0 {
					t.Fatalf("progress calls=%d err=%v diagnostics=%q", calls, err, diagnostics.String())
				}
			}
		})
	}
}

// Answer after the real interactive renderer displays the question. Keep the
// buffer named so io.Copy cannot bypass Write through a promoted ReaderFrom.
type confirmationTerminal struct {
	file     *os.File
	input    *os.File
	buffer   bytes.Buffer
	answered bool
}

func (w *confirmationTerminal) Fd() uintptr { return w.file.Fd() }

func (w *confirmationTerminal) Write(data []byte) (int, error) {
	n, err := w.buffer.Write(data)
	if !w.answered && strings.Contains(w.buffer.String(), "Proceed with selection?") {
		w.answered = true
		_, err = w.input.WriteString("y\r")
	}
	return n, err
}

func TestConfirmHidesKeyHelp(t *testing.T) {
	t.Setenv("TERM", "screen-256color")
	t.Setenv("ACCESSIBLE", "")
	t.Setenv("NO_COLOR", "")
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	diagnostics := &confirmationTerminal{file: slave, input: master}
	confirmed, err := New(slave, &output, diagnostics).Confirm("Proceed with selection?", "Review the selected instances.")
	if err != nil || !confirmed || !diagnostics.answered {
		t.Fatalf("confirmed=%t answered=%t err=%v output=%q", confirmed, diagnostics.answered, err, diagnostics.buffer.String())
	}
	text := strings.Join(strings.Fields(ansi.Strip(diagnostics.buffer.String())), " ")
	if !strings.Contains(text, "Proceed with selection?") || !strings.Contains(text, "Review the selected instances.") {
		t.Fatalf("missing interactive prompt content: %q", text)
	}
	// Huh's single-field Run hides help. These are the pinned Confirm key-help
	// entries that a bare NewForm would add beneath the question and buttons.
	if strings.Contains(text, "enter submit") || strings.Contains(text, "←/→ toggle") {
		t.Fatalf("confirmation unexpectedly displays key-help footer: %q", text)
	}
	if output.Len() != 0 {
		t.Fatalf("interactive prompt wrote stdout: %q", output.String())
	}
}
