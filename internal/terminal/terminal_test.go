package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/frgrisk/ec2ctl/adapter/aws"
)

const (
	testRegion     = "us-east-1"
	testInstanceID = "i-0123456789abcdef0"
	runningState   = "running"
)

func TestRedirectedStreams(t *testing.T) {
	t.Setenv("ACCESSIBLE", "1")
	var output, diagnostics bytes.Buffer
	ui := New(strings.NewReader("yes\n"), &output, &diagnostics)
	if err := ui.RequireConfirmation("start", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("piped yes bypassed refusal: %v", err)
	}
	if err := ui.RequireConfirmation("start", true); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("action failed")
	calls := 0
	if err := ui.Progress(t.Context(), "scanning", func(context.Context) error { calls++; return failure }); !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("progress: calls=%d error=%v", calls, err)
	}
	if output.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatal("redirected progress wrote output")
	}
	ui.Fleet(aws.AccountSummary{{
		Region: "us-east-1",
		Instances: []aws.Instance{{
			Name:   "test",
			Status: runningState,
		}},
	}}, false)
	ui.Printf("request accepted\n")
	hasExpectedOutput := strings.Contains(output.String(), "EC2 FLEET") &&
		strings.Contains(output.String(), "request accepted")
	if !hasExpectedOutput || strings.Contains(output.String(), "\x1b") {
		t.Fatalf("redirected output: %q", output.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %q", diagnostics.String())
	}
}

func TestAccessibleConfirmation(t *testing.T) {
	for _, answer := range []string{"yes", "no"} {
		t.Run(answer, func(t *testing.T) {
			t.Setenv("ACCESSIBLE", "1")
			var output, diagnostics bytes.Buffer
			ui := New(strings.NewReader(answer+"\n"), &output, &diagnostics)
			confirmed, err := ui.Confirm(t.Context(), "Would you like to proceed?", "")
			if err != nil || confirmed != (answer == "yes") {
				t.Fatalf("confirmed=%t err=%v", confirmed, err)
			}
			if !strings.Contains(output.String(), "Would you like to proceed?") || diagnostics.Len() != 0 {
				t.Fatalf("output=%q diagnostics=%q", output.String(), diagnostics.String())
			}
		})
	}
}

func TestProgressCancellationJoinsAction(t *testing.T) {
	for _, canAnimate := range []bool{false, true} {
		name := "plain"
		if canAnimate {
			name = "animated"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("TERM", "screen-256color")
			ui := New(strings.NewReader(""), io.Discard, io.Discard)
			ui.canAnimate = canAnimate
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{})
			exited := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- ui.Progress(ctx, "scanning", func(ctx context.Context) error {
					close(started)
					<-ctx.Done()
					defer close(exited)
					return ctx.Err()
				})
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("action did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want context cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("progress did not stop")
			}
			select {
			case <-exited:
			default:
				t.Fatal("progress returned before its action exited")
			}
		})
	}
}
