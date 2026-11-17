package cmd

import (
	"encoding/json/jsontext"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/frgrisk/ec2ctl/adapter/aws"
)

const (
	cliTestRegion           = "us-east-1"
	regionsFlag             = "--regions"
	modifyAction            = "modify"
	actionFailure           = "action"
	terminateName           = "terminate"
	describeInstancesAction = "DescribeInstances"
	dryRunEnabled           = "true"
)

func TestUsabilityProcess(t *testing.T) {
	if os.Getenv("EC2CTL_USABILITY_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"ec2ctl"}, os.Args[i+1:]...)
			Execute()
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI arguments")
}

func cliCommand(t *testing.T, endpoint string, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "ec2ctl.yaml")
	if err := os.WriteFile(config, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	argv := append([]string{"-test.run=^TestUsabilityProcess$", "--", "--config", config}, args...)
	process := exec.CommandContext(t.Context(), executable, argv...)
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "AWS_") && !strings.HasPrefix(setting, "EC2CTL_") {
			process.Env = append(process.Env, setting)
		}
	}
	process.Env = append(process.Env, "EC2CTL_USABILITY_PROCESS=1", "AWS_ENDPOINT_URL_EC2="+endpoint,
		"AWS_ACCESS_KEY_ID=test", "AWS_SECRET_ACCESS_KEY=test", "AWS_EC2_METADATA_DISABLED=true", "AWS_REGION=us-east-1",
		"AWS_CONFIG_FILE="+filepath.Join(dir, "config"), "AWS_SHARED_CREDENTIALS_FILE="+filepath.Join(dir, "credentials"),
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=false", "ACCESSIBLE=1")
	return process
}

func runCLI(t *testing.T, endpoint string, args ...string) (stdoutText, stderrText string, runErr error) {
	t.Helper()
	process := cliCommand(t, endpoint, args...)

	var stdout, stderr strings.Builder
	process.Stdout, process.Stderr = &stdout, &stderr
	process.Stdin = strings.NewReader("yes\n")
	err := process.Run()
	return stdout.String(), stderr.String(), err
}

func TestInvalidArgumentsNeverReachAWS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	for _, action := range []string{"status", aws.InstanceStart, aws.InstanceStop, terminateName, modifyAction} {
		args := []string{action, regionsFlag, cliTestRegion, "not-an-id"}
		if action == modifyAction {
			args = append(args, "--type", "t3.small")
		}
		_, stderr, err := runCLI(t, server.URL, args...)
		if err == nil || !strings.Contains(stderr, "not a valid instance ID") {
			t.Errorf("%s: error=%v, stderr=%s", action, err, stderr)
		}
	}
	for _, action := range []string{terminateName, modifyAction} {
		_, stderr, err := runCLI(t, server.URL, action, regionsFlag, cliTestRegion)
		if err == nil || !strings.Contains(stderr, "at least one instance ID") {
			t.Errorf("%s accepted missing IDs: %v %s", action, err, stderr)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid arguments sent AWS requests")
	}
}

func TestInstanceIDValidation(t *testing.T) {
	for _, id := range []string{"i-0123abcd", "i-0123456789abcdef0"} {
		if err := validateInstanceArgs([]string{id}); err != nil {
			t.Errorf("valid ID %s: %v", id, err)
		}
	}
	for _, id := range []string{"i-zzzzzzzz", "i-||||||||", "xx0123456789abcdef0", "i-0123456789abcdef0suffix", "i-012345678"} {
		if err := validateInstanceArgs([]string{id}); err == nil {
			t.Errorf("accepted %s", id)
		}
	}
}

func TestEmptyStatusJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet/></DescribeInstancesResponse>`)
	}))
	t.Cleanup(server.Close)
	stdout, stderr, err := runCLI(t, server.URL, "status", regionsFlag, cliTestRegion, "-o", "json")
	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestAWSFailuresExitNonzero(t *testing.T) {
	for _, failure := range []string{"query", actionFailure} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				if failure == actionFailure && r.Form.Get("Action") == "DescribeInstances" {
					fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId><instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>denied</Message></Error></Errors></Response>`)
			}))
			t.Cleanup(server.Close)
			args := []string{"status", regionsFlag, cliTestRegion, "-o", "json"}
			if failure == actionFailure {
				args = []string{aws.InstanceStart, regionsFlag, cliTestRegion, "--force"}
			}
			stdout, stderr, err := runCLI(t, server.URL, args...)
			if err == nil || !strings.Contains(stderr, "UnauthorizedOperation") {
				t.Fatalf("stdout=%q stderr=%q err=%v", stdout, stderr, err)
			}
			if failure == "query" && stdout != "" {
				t.Fatalf("failed JSON query wrote success output: %q", stdout)
			}
		})
	}
}

func TestDemoNeedsNoAWS(t *testing.T) {
	stdout, stderr, err := runCLI(t, "http://127.0.0.1:1", "demo", "-o", "json")
	if err != nil || !jsontext.Value(stdout).IsValid() || !strings.Contains(stdout, "api-dev") {
		t.Fatalf("stdout=%s stderr=%s err=%v", stdout, stderr, err)
	}
}

func TestLifecycleRefusalNeverReachesAWS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateName} {
		t.Run(action, func(t *testing.T) {
			stdout, stderr, err := runCLI(
				t,
				server.URL,
				action,
				regionsFlag,
				cliTestRegion,
				"i-0123456789abcdef0",
			)
			isRefused := err != nil && strings.Contains(stderr, "--force")
			if !isRefused || stdout != "" {
				t.Fatalf(
					"stdout=%q stderr=%q err=%v",
					stdout,
					stderr,
					err,
				)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("refusal sent %d AWS requests", requests.Load())
	}
}

func TestDemoUsesCommandOutput(t *testing.T) {
	var output, diagnostics strings.Builder
	demoCmd.SetOut(&output)
	demoCmd.SetErr(&diagnostics)
	t.Cleanup(func() { demoCmd.SetOut(nil); demoCmd.SetErr(nil) })
	if err := demoCmd.RunE(demoCmd, nil); err != nil {
		t.Fatal(err)
	}
	hasExpectedOutput := strings.Contains(output.String(), "Sample fleet") &&
		strings.Contains(output.String(), "EC2 FLEET")
	if !hasExpectedOutput || diagnostics.Len() != 0 {
		t.Fatalf("output=%q diagnostics=%q", output.String(), diagnostics.String())
	}
}

func TestExplicitInstancesValidateBeforeMutation(t *testing.T) {
	const validID = "i-00000000000000001"
	const missingID = "i-00000000000000002"
	for _, action := range []string{terminateName, modifyAction} {
		t.Run(action, func(t *testing.T) {
			var queries, mutations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				if r.Form.Get("Action") == describeInstancesAction {
					queries.Add(1)
					fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>`+
						validID+`</instanceId><instanceState><name>stopped</name></instanceState></item>`+
						`</instancesSet></item></reservationSet></DescribeInstancesResponse>`)
					return
				}
				// Count every operation attempt, including dry runs.
				mutations.Add(1)
				fmt.Fprint(w, `<Response><return>true</return></Response>`)
			}))
			t.Cleanup(server.Close)
			args := []string{action, regionsFlag, cliTestRegion, validID, missingID}
			if action == terminateName {
				args = append(args, "--force")
			} else {
				args = append(args, "--type", "t3.small")
			}
			stdout, stderr, err := runCLI(t, server.URL, args...)
			if err == nil {
				t.Fatalf("incomplete target set succeeded: %s", stdout)
			}
			if !strings.Contains(stderr, "instance "+missingID+" not found; check --regions and --tag") {
				t.Fatalf("unexpected error: %s", stderr)
			}
			if queries.Load() == 0 {
				t.Fatal("command did not query inventory")
			}
			if count := mutations.Load(); count != 0 {
				t.Fatalf("incomplete target set sent %d operation requests", count)
			}
			if stdout != "" {
				t.Fatalf("incomplete target set wrote success output: %q", stdout)
			}
		})
	}
}

func TestExplicitInstancesPreserveMutationOrder(t *testing.T) {
	const firstID = "i-00000000000000001"
	const secondID = "i-00000000000000002"
	for _, action := range []string{terminateName, modifyAction} {
		t.Run(action, func(t *testing.T) {
			var mu sync.Mutex
			operations := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				if r.Form.Get("Action") == describeInstancesAction {
					fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet>`+
						`<item><instanceId>`+firstID+`</instanceId></item>`+
						`<item><instanceId>`+secondID+`</instanceId></item>`+
						`</instancesSet></item></reservationSet></DescribeInstancesResponse>`)
					return
				}
				if r.Form.Get("DryRun") == dryRunEnabled {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code>`+
						`<Message>dry run</Message></Error></Errors></Response>`)
					return
				}
				ids := []string{}
				if id := r.Form.Get("InstanceId"); id != "" {
					ids = append(ids, id)
				}
				for i := 1; r.Form.Has(fmt.Sprintf("InstanceId.%d", i)); i++ {
					ids = append(ids, r.Form.Get(fmt.Sprintf("InstanceId.%d", i)))
				}
				mu.Lock()
				operations = append(operations, strings.Join(ids, ","))
				mu.Unlock()
				fmt.Fprint(w, `<Response><return>true</return></Response>`)
			}))
			t.Cleanup(server.Close)
			args := []string{action, regionsFlag, cliTestRegion, secondID, firstID, secondID}
			want := []string{secondID, firstID, secondID}
			if action == terminateName {
				args = append(args, "--force")
				want = []string{firstID + "," + secondID}
			} else {
				args = append(args, "--type", "t3.small")
			}
			stdout, stderr, err := runCLI(t, server.URL, args...)
			if err != nil {
				t.Fatalf("command failed: %v; stderr=%s", err, stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(operations, want) {
				t.Fatalf("operations=%v want=%v", operations, want)
			}
			if !strings.Contains(stdout, cliTestRegion) {
				t.Fatalf("missing region in output: %s", stdout)
			}
		})
	}
}
