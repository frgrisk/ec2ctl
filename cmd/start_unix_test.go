//go:build unix

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
	"github.com/spf13/cobra"
)

type runtimeCommandWriter struct {
	file   *os.File
	writer io.Writer
}

func (w runtimeCommandWriter) Fd() uintptr { return w.file.Fd() }

func (w runtimeCommandWriter) Write(data []byte) (int, error) { return w.writer.Write(data) }

func TestStartStopCancellationKeepsRegionalResults(t *testing.T) {
	const (
		west           = "us-west-2"
		eastID         = "i-00000000000000001"
		westID         = "i-00000000000000002"
		responseSettle = 50 * time.Millisecond
		startOperation = "StartInstances"
		stopOperation  = "StopInstances"
	)
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop} {
		for _, mode := range []string{"plain", "animated"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				t.Setenv("TERM", "screen-256color")
				t.Setenv("ACCESSIBLE", "")
				t.Setenv("NO_COLOR", "")
				operation, previousState, currentState := startOperation, "stopped", "pending"
				run := startCmd.RunE
				if action == aws.InstanceStop {
					operation, previousState, currentState = stopOperation, "running", "stopping"
					run = stopCmd.RunE
				}
				westResponded := make(chan struct{})
				eastStarted := make(chan struct{})
				eastCancelled := make(chan struct{})
				release := make(chan struct{})
				mockCommandEC2(t, func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "text/xml")
					isWest := strings.Contains(r.Header.Get("Authorization"), "/"+west+"/ec2/")
					id := eastID
					if isWest {
						id = westID
					}
					if r.Form.Get("Action") == inventoryDescribeAction {
						fmt.Fprintf(
							w,
							`<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>%s</instanceId><instanceState><name>%s</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`,
							id,
							previousState,
						)
						return
					}
					if got := r.Form.Get("Action"); got != operation {
						t.Errorf("action = %q, want %q", got, operation)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if r.Form.Get("DryRun") == mockTrue {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code></Error></Errors></Response>`)
						return
					}
					if isWest {
						defer close(westResponded)
						fmt.Fprintf(
							w,
							`<Response><instancesSet><item><instanceId>%s</instanceId><previousState><name>%s</name></previousState><currentState><name>%s</name></currentState></item></instancesSet></Response>`,
							id,
							previousState,
							currentState,
						)
						return
					}
					close(eastStarted)
					select {
					case <-r.Context().Done():
						close(eastCancelled)
					case <-release:
					}
				})
				regions = []string{west, cliTestRegion}
				master, slave, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
				var output, diagnostics bytes.Buffer
				command := &cobra.Command{Use: "cancellation-test", RunE: run, SilenceErrors: true, SilenceUsage: true}
				command.SetIn(strings.NewReader(""))
				command.SetOut(&output)
				command.SetErr(&diagnostics)
				if mode == "animated" {
					command.SetOut(runtimeCommandWriter{file: slave, writer: &output})
					command.SetErr(runtimeCommandWriter{file: slave, writer: &diagnostics})
				}
				command.Flags().Bool("force", true, "")
				command.SetArgs([]string{})
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				t.Cleanup(func() {
					cancel()
					close(release)
					for range done {
					}
				})
				go func() {
					defer close(done)
					done <- command.ExecuteContext(ctx)
				}()
				for _, ready := range []<-chan struct{}{westResponded, eastStarted} {
					select {
					case <-ready:
					case <-time.After(cancellationWait):
						t.Fatal("regional mutation requests did not reach their expected states")
					}
				}
				// The server response precedes SDK decoding. Allow the successful
				// response to settle while the sibling request remains blocked.
				select {
				case err := <-done:
					t.Fatalf("command returned before cancellation with a region blocked: %v", err)
				case <-time.After(responseSettle):
				}
				cancel()
				select {
				case err = <-done:
				case <-time.After(cancellationWait):
					t.Fatal("command did not join regional work after cancellation")
				}
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error = %v, want context cancellation", err)
				}
				expectedFailure := cliTestRegion + ": cannot " + action + " [" + eastID + "]"
				if err == nil || !strings.Contains(err.Error(), expectedFailure) {
					t.Errorf("missing region-qualified failure %q: %v", expectedFailure, err)
				}
				expectedResult := west + "  " + westID + "  " + previousState + " -> " + currentState
				if !strings.Contains(output.String(), expectedResult) {
					t.Errorf("successful regional mutation result hidden: %q", output.String())
				}
				if strings.Contains(output.String(), "request accepted for") {
					t.Errorf("partial cancellation printed all-region success: %q", output.String())
				}
				select {
				case <-eastCancelled:
				case <-time.After(cancellationWait):
					t.Fatal("regional HTTP request outlived command cancellation")
				}
			})
		}
	}
}
