//go:build unix && integration

package cmd_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/cmd"
)

const (
	accessibleMode  = "accessible"
	terminateAction = "terminate"
)

// TestCLIProcess runs the real entry point in an isolated process, including its exit status.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("EC2CTL_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"ec2ctl"}, os.Args[i+1:]...)
			cmd.Execute()
			return
		}
	}
	t.Fatal("missing command arguments")
}

func cliProcess(t *testing.T, emulatorEnv []string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), emulatorTimeout)
	t.Cleanup(cancel)
	configDir := t.TempDir()
	argv := append([]string{"-test.run=^TestCLIProcess$", "--", "--config", filepath.Join(configDir, "ec2ctl.yaml")}, args...)
	process := exec.CommandContext(ctx, executable, argv...)
	process.Env = append([]string{
		"EC2CTL_TEST_PROCESS=1", "GLAMOUR_STYLE=dark", "TERM=xterm-256color",
	}, emulatorEnv...)
	process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return process
}

func TestLifecycleRequiresForceWithoutTerminal(t *testing.T) {
	emulator := newEmulator(t)
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, mode := range []string{"default", accessibleMode, "dumb", "force-false"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				instanceID := emulator.instance(t, action)
				emulator.requests.Store(0)
				process := cliProcess(t, emulator.env, action, "--regions", "us-east-1", instanceID)
				switch mode {
				case accessibleMode:
					process.Env = append(process.Env, "ACCESSIBLE=1")
					process.Stdin = strings.NewReader("yes\n")
				case "dumb":
					process.Env = append(process.Env, "TERM=dumb")
				case "force-false":
					process.Args = append(process.Args, "--force=false")
				}
				output, err := process.CombinedOutput()
				if err == nil || process.ProcessState == nil || process.ProcessState.ExitCode() != 1 {
					t.Fatalf("exit = %v, want status 1; output: %s", err, output)
				}
				if !strings.Contains(string(output), "--force") {
					t.Errorf("refusal must name --force; output: %s", output)
				}
				if count := emulator.requests.Load(); count != 0 {
					t.Errorf("refusal sent %d AWS requests, want none", count)
				}
				emulator.assertState(t, instanceID, expectedState(action, false))
			})
		}
	}
}

func TestLifecycleForceWithoutTerminal(t *testing.T) {
	emulator := newEmulator(t)
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, flag := range []string{"--force", "-f"} {
			t.Run(action+"/"+flag, func(t *testing.T) {
				instanceID := emulator.instance(t, action)
				emulator.requests.Store(0)
				process := cliProcess(t, emulator.env, action, flag, "--regions", "us-east-1", instanceID)
				output, err := process.CombinedOutput()
				if err != nil {
					t.Fatalf("forced command failed: %v; output: %s", err, output)
				}
				if strings.Contains(string(output), "proceed?") || strings.Contains(string(output), "Are you sure") {
					t.Errorf("forced command prompted: %s", output)
				}
				emulator.assertState(t, instanceID, expectedState(action, true))
			})
		}
	}
}

func TestLifecyclePromptsWithTerminal(t *testing.T) {
	emulator := newEmulator(t)
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, answer := range []struct {
			name      string
			keys      string
			confirmed bool
		}{
			{name: "decline", keys: "n\r"},
			{name: "confirm", keys: "y\r", confirmed: true},
			{name: accessibleMode, keys: "yes\n", confirmed: true},
		} {
			t.Run(action+"/"+answer.name, func(t *testing.T) {
				instanceID := emulator.instance(t, action)
				emulator.requests.Store(0)
				process := cliProcess(t, emulator.env, action, "--regions", "us-east-1", instanceID)
				// A screen terminal uses fixed colors instead of querying a terminal emulator.
				process.Env = append(process.Env, "TERM=screen-256color")
				if answer.name == accessibleMode {
					process.Env = append(process.Env, "ACCESSIBLE=1")
				}
				terminal, err := pty.StartWithSize(process, &pty.Winsize{Rows: 40, Cols: 240})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = terminal.Close() })
				var output strings.Builder
				prompted := false
				done := make(chan struct{})
				go func() {
					defer close(done)
					buffer := make([]byte, 4096)
					for {
						n, readErr := terminal.Read(buffer)
						output.Write(buffer[:n])
						text := output.String()
						if !prompted && (strings.Contains(text, "Would you like to proceed?") || strings.Contains(text, "Are you sure")) {
							prompted = true
							_, _ = terminal.WriteString(answer.keys)
						}
						if readErr != nil {
							return
						}
					}
				}()
				err = process.Wait()
				_ = terminal.Close()
				<-done
				if err != nil || !prompted {
					t.Fatalf("prompted = %t, exit = %v; output: %s", prompted, err, output.String())
				}
				emulator.assertState(t, instanceID, expectedState(action, answer.confirmed))
			})
		}
	}
}
