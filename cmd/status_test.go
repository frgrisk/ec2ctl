package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/spf13/cobra"
)

const (
	cancellationWait        = 2 * time.Second
	inventoryDescribeAction = "DescribeInstances"
	describeSpotAction      = "DescribeSpotInstanceRequests"
	mockCredential          = "test"
	mockTrue                = "true"
)

func mockCommandEC2(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for key, value := range map[string]string{
		"AWS_ENDPOINT_URL_EC2":                server.URL,
		"AWS_ACCESS_KEY_ID":                   mockCredential,
		"AWS_SECRET_ACCESS_KEY":               mockCredential,
		"AWS_SESSION_TOKEN":                   "",
		"AWS_PROFILE":                         "",
		"AWS_REGION":                          cliTestRegion,
		"AWS_EC2_METADATA_DISABLED":           mockTrue,
		"AWS_CONFIG_FILE":                     filepath.Join(t.TempDir(), "config"),
		"AWS_SHARED_CREDENTIALS_FILE":         filepath.Join(t.TempDir(), "credentials"),
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "false",
	} {
		t.Setenv(key, value)
	}
	previousRegions := regions
	t.Cleanup(func() { regions = previousRegions })
	regions = []string{cliTestRegion}
}

func TestCommandsCancelAWSRequests(t *testing.T) {
	for _, tc := range []struct {
		name          string
		command       *cobra.Command
		blockedAction string
		hasSecondPage bool
		isMutation    bool
		isRegional    bool
	}{
		{name: "discovery", command: statusCmd, blockedAction: "DescribeRegions"},
		{name: "inventory", command: statusCmd, blockedAction: inventoryDescribeAction},
		{name: "regional-inventory", command: statusCmd, blockedAction: inventoryDescribeAction, isRegional: true},
		{name: "pagination", command: statusCmd, blockedAction: inventoryDescribeAction, hasSecondPage: true},
		{name: "spot-inventory", command: statusCmd, blockedAction: describeSpotAction},
		{name: "spot-pagination", command: statusCmd, blockedAction: describeSpotAction, hasSecondPage: true},
		{name: "start-dry-run", command: startCmd, blockedAction: "StartInstances"},
		{name: "start-mutation", command: startCmd, blockedAction: "StartInstances", isMutation: true},
		{name: "stop-dry-run", command: stopCmd, blockedAction: "StopInstances"},
		{name: "stop-mutation", command: stopCmd, blockedAction: "StopInstances", isMutation: true},
		{name: "modify-dry-run", command: modifyCmd, blockedAction: "ModifyInstanceAttribute"},
		{name: "modify-mutation", command: modifyCmd, blockedAction: "ModifyInstanceAttribute", isMutation: true},
		{name: "terminate", command: terminateCmd, blockedAction: "TerminateInstances"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{}, 2)
			released := make(chan struct{})
			requestCancelled := make(chan struct{}, 2)
			var requests atomic.Int32
			mockCommandEC2(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				action := r.Form.Get("Action")
				shouldBlock := action == tc.blockedAction
				if tc.hasSecondPage {
					shouldBlock = shouldBlock && r.Form.Get("NextToken") == "page2"
				}
				if tc.isMutation {
					shouldBlock = shouldBlock && r.Form.Get("DryRun") != mockTrue
				}
				if shouldBlock {
					started <- struct{}{}
					select {
					case <-r.Context().Done():
						requestCancelled <- struct{}{}
					case <-released:
						fmt.Fprint(w, `<Response/>`)
					}
					return
				}
				if action == inventoryDescribeAction {
					nextToken := ""
					if tc.hasSecondPage && tc.blockedAction == inventoryDescribeAction {
						nextToken = "<nextToken>page2</nextToken>"
					}
					spot := ""
					if tc.blockedAction == describeSpotAction {
						spot = "<instanceLifecycle>spot</instanceLifecycle><spotInstanceRequestId>sir-test</spotInstanceRequestId>"
					}
					fmt.Fprintf(
						w,
						`<DescribeInstancesResponse>%s<reservationSet><item><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId>%s<instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`,
						nextToken,
						spot,
					)
					return
				}
				if action == describeSpotAction {
					fmt.Fprint(w, `<DescribeSpotInstanceRequestsResponse><nextToken>page2</nextToken></DescribeSpotInstanceRequestsResponse>`)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code><Message>dry run</Message></Error></Errors></Response>`)
			})
			// Release a broken implementation before httptest.Server.Close waits.
			isJoined := false
			done := make(chan error, 1)
			t.Cleanup(func() {
				close(released)
				if !isJoined {
					for range done {
					}
				}
			})
			requestCount := 1
			if tc.isRegional {
				regions = []string{cliTestRegion, "us-west-2"}
				requestCount = len(regions)
			}
			if tc.name == "discovery" {
				regions = nil
			}
			command := &cobra.Command{Use: "runtime-test", RunE: tc.command.RunE, SilenceUsage: true, SilenceErrors: true}
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.Flags().Bool("force", true, "")
			command.Flags().String("type", "t3.small", "")
			command.SetArgs([]string{"i-0123456789abcdef0"})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() {
				defer close(done)
				done <- command.ExecuteContext(ctx)
			}()
			for range requestCount {
				select {
				case <-started:
				case <-time.After(cancellationWait):
					t.Fatal("AWS request did not start")
				}
			}
			count := requests.Load()
			cancel()
			select {
			case err := <-done:
				isJoined = true
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want context cancellation", err)
				}
			case <-time.After(cancellationWait):
				t.Fatal("command did not join its AWS work after cancellation")
			}
			for range requestCount {
				select {
				case <-requestCancelled:
				case <-time.After(cancellationWait):
					t.Fatal("HTTP request outlived the cancelled command")
				}
			}
			if got := requests.Load(); got != count {
				t.Fatalf("cancellation sent more requests: got %d, want %d", got, count)
			}
		})
	}
}

// A region failure must not cancel sibling work or hide other region errors.
func TestRegionalErrorPolicy(t *testing.T) {
	const west = "us-west-2"
	for _, action := range []string{aws.InstanceStatus, aws.InstanceStart} {
		t.Run(action, func(t *testing.T) {
			westStarted := make(chan struct{})
			mockCommandEC2(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				isWest := strings.Contains(r.Header.Get("Authorization"), "/"+west+"/ec2/")
				if r.Form.Get("Action") == inventoryDescribeAction && action == aws.InstanceStart {
					fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId><instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
					return
				}
				if isWest {
					if action == aws.InstanceStart && r.Form.Get("DryRun") == mockTrue {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code></Error></Errors></Response>`)
						return
					}
					close(westStarted)
					if action == aws.InstanceStart {
						fmt.Fprint(w, `<StartInstancesResponse><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId><previousState><name>stopped</name></previousState><currentState><name>pending</name></currentState></item></instancesSet></StartInstancesResponse>`)
						return
					}
				} else {
					select {
					case <-westStarted:
					case <-r.Context().Done():
						return
					}
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>denied</Message></Error></Errors></Response>`)
			})
			regions = []string{west, cliTestRegion, west}
			command := &cobra.Command{Use: "runtime-test", RunE: statusCmd.RunE, SilenceUsage: true, SilenceErrors: true}
			if action == aws.InstanceStart {
				command.RunE = startCmd.RunE
			}
			var output strings.Builder
			command.SetOut(&output)
			command.SetErr(io.Discard)
			command.SetArgs([]string{})
			command.Flags().Bool("force", true, "")
			ctx, cancel := context.WithTimeout(t.Context(), cancellationWait)
			defer cancel()
			err := command.ExecuteContext(ctx)
			if err == nil {
				t.Fatal("regional failure returned success")
			}
			if !strings.Contains(err.Error(), cliTestRegion+":") {
				t.Fatalf("missing east region error: %v", err)
			}
			if action == aws.InstanceStatus {
				if !strings.Contains(err.Error(), west+":") {
					t.Fatalf("missing west region error: %v", err)
				}
				if output.Len() != 0 {
					t.Fatalf("failed inventory printed success: %s", output.String())
				}
			} else if !strings.Contains(output.String(), west+"  i-0123456789abcdef0  stopped -> pending") {
				t.Fatalf("successful region result lost: %s; error: %v", output.String(), err)
			}
		})
	}
}
