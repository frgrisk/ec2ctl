//go:build unix

package cmd_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/frgrisk/ec2ctl/cmd"
)

const (
	testInstanceID  = "i-0123456789abcdef0"
	dryRunTrue      = "true"
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

type fakeEC2 struct {
	requests []url.Values
	mu       sync.Mutex
}

func (f *fakeEC2) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, r.PostForm)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/xml")
	switch action := r.Form.Get("Action"); action {
	case "DescribeInstances":
		_, _ = fmt.Fprintf(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>%s</instanceId><instanceState><code>16</code><name>running</name></instanceState><placement><availabilityZone>us-east-1a</availabilityZone></placement><tagSet><item><key>Name</key><value>test-instance</value></item></tagSet></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`, testInstanceID)
	case "DescribeInstanceStatus":
		_, _ = fmt.Fprint(w, `<DescribeInstanceStatusResponse/>`)
	case "DescribeSpotInstanceRequests":
		_, _ = fmt.Fprint(w, `<DescribeSpotInstanceRequestsResponse/>`)
	case "StartInstances", "StopInstances", "TerminateInstances":
		if r.Form.Get("DryRun") == dryRunTrue {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code><Message>dry run</Message></Error></Errors><RequestID>test</RequestID></Response>`)
			return
		}
		_, _ = fmt.Fprintf(w, `<Response><requestId>test</requestId><instancesSet><item><instanceId>%s</instanceId><currentState><code>64</code><name>stopping</name></currentState><previousState><code>16</code><name>running</name></previousState></item></instancesSet></Response>`, testInstanceID)
	default:
		http.Error(w, "unexpected action: "+action, http.StatusBadRequest)
	}
}

func cliProcess(t *testing.T, endpoint string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	configDir := t.TempDir()
	argv := append([]string{"-test.run=^TestCLIProcess$", "--", "--config", filepath.Join(configDir, "ec2ctl.yaml")}, args...)
	process := exec.CommandContext(ctx, executable, argv...)
	process.Env = []string{
		"EC2CTL_TEST_PROCESS=1", "GLAMOUR_STYLE=dark", "TERM=xterm-256color",
		"AWS_ENDPOINT_URL_EC2=" + endpoint,
		"AWS_ACCESS_KEY_ID=test-access-key", "AWS_SECRET_ACCESS_KEY=test-secret-key",
		"AWS_CONFIG_FILE=" + filepath.Join(configDir, "config"),
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(configDir, "credentials"),
		"AWS_EC2_METADATA_DISABLED=true", "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=false",
	}
	process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return process
}

func TestLifecycleRequiresForceWithoutTerminal(t *testing.T) {
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, mode := range []string{"default", accessibleMode, "dumb", "force-false"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				fake := &fakeEC2{}
				server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
				t.Cleanup(server.Close)
				process := cliProcess(t, server.URL, action, "--regions", "us-east-1", testInstanceID)
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
				fake.mu.Lock()
				defer fake.mu.Unlock()
				if len(fake.requests) != 0 {
					t.Errorf("refusal sent %d AWS requests, want none", len(fake.requests))
				}
			})
		}
	}
}

func TestLifecycleForceWithoutTerminal(t *testing.T) {
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, flag := range []string{"--force", "-f"} {
			t.Run(action+"/"+flag, func(t *testing.T) {
				fake := &fakeEC2{}
				server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
				t.Cleanup(server.Close)
				process := cliProcess(t, server.URL, action, flag, "--regions", "us-east-1", testInstanceID)
				output, err := process.CombinedOutput()
				if err != nil {
					t.Fatalf("forced command failed: %v; output: %s", err, output)
				}
				if strings.Contains(string(output), "proceed?") || strings.Contains(string(output), "Are you sure") {
					t.Errorf("forced command prompted: %s", output)
				}
				fake.mu.Lock()
				defer fake.mu.Unlock()
				wantAction := map[string]string{aws.InstanceStart: "StartInstances", aws.InstanceStop: "StopInstances", terminateAction: "TerminateInstances"}[action]
				mutations := 0
				for _, request := range fake.requests {
					if request.Get("Action") == wantAction && request.Get("DryRun") != dryRunTrue {
						mutations++
						if got := request.Get("InstanceId.1"); got != testInstanceID {
							t.Errorf("acted on instance %q, want %q", got, testInstanceID)
						}
					}
				}
				if mutations != 1 {
					t.Errorf("sent %d %s mutations, want 1; requests: %v", mutations, wantAction, fake.requests)
				}
			})
		}
	}
}

func TestLifecyclePromptsWithTerminal(t *testing.T) {
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateAction} {
		for _, answer := range []struct {
			name      string
			keys      string
			mutations int
		}{
			{name: "decline", keys: "n\r"},
			{name: "confirm", keys: "y\r", mutations: 1},
			{name: accessibleMode, keys: "yes\n", mutations: 1},
		} {
			t.Run(action+"/"+answer.name, func(t *testing.T) {
				fake := &fakeEC2{}
				server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
				t.Cleanup(server.Close)
				process := cliProcess(t, server.URL, action, "--regions", "us-east-1", testInstanceID)
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
				fake.mu.Lock()
				defer fake.mu.Unlock()
				mutations := 0
				for _, request := range fake.requests {
					if !strings.HasPrefix(request.Get("Action"), "Describe") && request.Get("DryRun") != dryRunTrue {
						mutations++
					}
				}
				if mutations != answer.mutations {
					t.Errorf("sent %d mutations, want %d; output: %s", mutations, answer.mutations, output.String())
				}
			})
		}
	}
}
