//go:build unix

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
)

func TestHelpExamplesAtFortyColumns(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{name: "root", want: []string{
			"ec2ctl status --tag Environment=dev",
			"ec2ctl status --output json",
			"ec2ctl start --regions us-east-1 --tag Environment=dev",
		}},
		{name: modifyCmd.Name(), args: []string{modifyCmd.Name()}, want: []string{
			"ec2ctl modify --regions us-east-1 --type t3.small i-0123456789abcdef0",
		}},
		{name: statusCmd.Name(), args: []string{statusCmd.Name()}, want: []string{
			"ec2ctl status --regions us-east-1,ap-southeast-1",
			"ec2ctl status --tag Environment=dev",
			"ec2ctl status i-0123456789abcdef0 --wide",
			"ec2ctl status -o json",
		}},
		{name: startCmd.Name(), args: []string{startCmd.Name()}, want: []string{
			"ec2ctl start --regions us-east-1 --tag Environment=dev",
			"ec2ctl start --regions us-east-1 i-0123456789abcdef0",
			"ec2ctl start --regions us-east-1 --tag Environment=dev --force",
		}},
		{name: stopCmd.Name(), args: []string{stopCmd.Name()}, want: []string{
			"ec2ctl stop --regions us-east-1 --tag Environment=dev",
			"ec2ctl stop --regions us-east-1 i-0123456789abcdef0",
			"ec2ctl stop --regions us-east-1 --tag Environment=dev --force",
		}},
		{name: terminateCmd.Name(), args: []string{terminateCmd.Name()}, want: []string{
			"ec2ctl terminate --regions us-east-1 i-0123456789abcdef0",
		}},
		{name: demoCmd.Name(), args: []string{demoCmd.Name()}, want: []string{
			"ec2ctl demo", "ec2ctl demo --wide", "ec2ctl demo -o json",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := captureHelpPTY(t, func(output *os.File) error {
				args := append(slices.Clone(tc.args), "--help")
				process := cliCommand(t, "http://127.0.0.1:1", args...)
				var diagnostics strings.Builder
				process.Stdout, process.Stderr = output, &diagnostics
				if err := process.Run(); err != nil {
					return fmt.Errorf("help failed: %w; %s", err, diagnostics.String())
				}
				if diagnostics.Len() != 0 {
					return fmt.Errorf("help wrote diagnostics: %s", diagnostics.String())
				}
				return nil
			})
			assertHelpExampleArgv(t, text, tc.want)
		})
	}
}

func TestQuotedHelpExamplesAtFortyColumns(t *testing.T) {
	const example = `  # Quoted values and long IDs
  ec2ctl status --tag \
    'Name=api dev,Environment=dev' \
    i-0123456789abcdef0 \
    i-00000000000000001

  # A quoted configuration path
  ec2ctl \
    --config \
    "/tmp/config with spaces.yaml" \
    status --tag "Name=api dev"`
	t.Setenv("NO_COLOR", "1")
	text := captureHelpPTY(t, func(output *os.File) error {
		command := &cobra.Command{Use: rootCmd.Name(), Example: example}
		command.SetOut(output)
		command.SetHelpFunc(commandHelp)
		return command.Help()
	})
	assertHelpExampleArgv(t, text, []string{
		"ec2ctl status --tag 'Name=api dev,Environment=dev' i-0123456789abcdef0 i-00000000000000001",
		`ec2ctl --config "/tmp/config with spaces.yaml" status --tag "Name=api dev"`,
	})
}

func captureHelpPTY(t *testing.T, action func(*os.File) error) string {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 80, Cols: 40}); err != nil {
		t.Fatal(err)
	}
	captured := make(chan []byte, 1)
	go func() {
		// PTY masters end with EOF on macOS or EIO on Linux after the slave closes.
		data, _ := io.ReadAll(master)
		captured <- data
	}()
	actionErr := action(slave)
	_ = slave.Close()
	text := strings.ReplaceAll(string(<-captured), "\r\n", "\n")
	if actionErr != nil {
		t.Fatal(actionErr)
	}
	return ansi.Strip(text)
}

func assertHelpExampleArgv(t *testing.T, text string, want []string) {
	t.Helper()
	_, examples, found := strings.Cut(text, "Examples:\n")
	if !found {
		t.Fatal("help omitted examples")
	}
	var commands []string
	var command strings.Builder
	for line := range strings.SplitSeq(examples, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if width := ansi.StringWidth(line); width > 40 {
			t.Errorf("example line is %d columns: %q", width, line)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		command.WriteString(line + "\n")
		if !strings.HasSuffix(line, "\\") {
			commands = append(commands, command.String())
			command.Reset()
		}
	}
	if command.Len() != 0 {
		t.Fatal("example ends with an unfinished continuation")
	}
	if len(commands) != len(want) {
		t.Fatalf("example count=%d want=%d: %s", len(commands), len(want), text)
	}
	for i, example := range commands {
		got, expected := helpShellArgv(t, example), helpShellArgv(t, want[i])
		if !slices.Equal(got, expected) {
			t.Errorf("example argv changed: %q want %q", got, expected)
		}
	}
}

func helpShellArgv(t *testing.T, example string) []string {
	t.Helper()
	// Execute only this local function, never the ec2ctl binary or an AWS action.
	script := "ec2ctl() { printf '%s\\000' ec2ctl \"$@\"; }\n" + example
	process := exec.CommandContext(t.Context(), "/bin/sh", "-c", script)
	data, err := process.Output()
	if err != nil {
		t.Fatalf("invalid shell example %q: %v", example, err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}
