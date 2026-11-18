//go:build unix

package terminal

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

func TestHelpWidthAndPolicy(t *testing.T) {
	const accessiblePolicy = "accessible"
	const dumbPolicy = "dumb"
	const noColorPolicy = "no-color"
	const example = "  ec2ctl modify --regions us-east-1 --type t3.small i-0123456789abcdef0"
	sections := []HelpSection{{Title: "Flags:", Text: "Readable prose with 日本語 that wraps at the output terminal width.\n\n  source <(ec2ctl completion bash)", Rows: [][2]string{{"--regions strings", "comma-separated regions to operate in with all words preserved"}, {"-o, --output string", "status output format with a continuation\nsecond paragraph"}, {"--tag stringToString", "query by tags (e.g. Environment=dev,Name=dev.example.com)"}}}, {Title: "Examples:", Literal: "  # A task label that is long enough to wrap at forty columns\n" + example}}
	for _, width := range []uint16{40, 80, 120} {
		for _, policy := range []string{"color", noColorPolicy, accessiblePolicy, dumbPolicy} {
			t.Run(fmt.Sprintf("%d/%s", width, policy), func(t *testing.T) {
				_, out := fleetTerminal(t, width, true)
				switch policy {
				case noColorPolicy:
					t.Setenv("NO_COLOR", "1")
				case accessiblePolicy:
					t.Setenv("ACCESSIBLE", "1")
				case dumbPolicy:
					t.Setenv("TERM", "dumb")
				}
				// Presentation follows output even when input is redirected.
				ui := New(strings.NewReader(""), out, &bytes.Buffer{})
				ui.Help(sections)
				text := ansi.Strip(out.String())
				if strings.Contains(out.String(), "\x1b") != (policy == "color") {
					t.Fatalf("color policy: %q", out.String())
				}
				if !strings.Contains(text, "\n\n  source <(ec2ctl completion bash)\n") {
					t.Fatal("embedded completion syntax lost indentation")
				}
				if !strings.Contains(text, example+"\n") {
					t.Fatal("example syntax was split")
				}
				for line := range strings.SplitSeq(text, "\n") {
					if line != example && ansi.StringWidth(line) > int(width) {
						t.Errorf("over-width prose: %q", line)
					}
				}
				if strings.Count(text, "-o, --output string") != 1 {
					t.Fatal("multiline description duplicated label")
				}
				if width == 40 && !strings.Contains(text, "  --regions strings\n    ") {
					t.Fatal("narrow rows did not stack")
				}
				out.Reset()
				if err := pty.Setsize(out.file, &pty.Winsize{Rows: 40, Cols: 40}); err != nil {
					t.Fatal(err)
				}
				ui.Help(sections)
				if !strings.Contains(ansi.Strip(out.String()), "  --regions strings\n    ") {
					t.Fatal("help ignored terminal resize")
				}
			})
		}
	}
}

func TestHelpRedirectedOutputIgnoresInputWidth(t *testing.T) {
	_, tty := fleetTerminal(t, 40, true)
	sections := []HelpSection{{Title: "Help:", Text: strings.Repeat("readable prose ", 8)}}
	var fromTTY, fromPipe, diagnostics bytes.Buffer
	New(tty.file, &fromTTY, &diagnostics).Help(sections)
	New(strings.NewReader(""), &fromPipe, &diagnostics).Help(sections)
	if fromTTY.String() != fromPipe.String() || strings.Contains(fromTTY.String(), "\x1b") || diagnostics.Len() != 0 {
		t.Fatal("redirected output depends on input")
	}
	hasWideLine := false
	for line := range strings.SplitSeq(fromTTY.String(), "\n") {
		if len(line) > 40 {
			hasWideLine = true
		}
		if len(line) > 80 {
			t.Fatalf("unstable pipe width: %q", line)
		}
	}
	if !hasWideLine {
		t.Fatal("redirected help inherited narrow input width")
	}
}
