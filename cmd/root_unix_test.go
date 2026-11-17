//go:build unix

package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

type promptSignalWriter struct {
	ready  chan struct{}
	buffer bytes.Buffer
	once   sync.Once
}

func (w *promptSignalWriter) Write(data []byte) (int, error) {
	n, err := w.buffer.Write(data)
	if strings.Contains(w.buffer.String(), "Would you like to proceed?") {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}

func TestSignalCancelsAccessibleConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("Action") != inventoryDescribeAction {
			t.Error("cancelled confirmation sent an action request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId><instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
	}))
	defer server.Close()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	process := cliCommand(t, server.URL, "start", regionsFlag, cliTestRegion)
	process.Stdin = slave
	output := &promptSignalWriter{ready: make(chan struct{})}
	process.Stdout, process.Stderr = output, output
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- process.Wait()
	}()
	defer func() {
		_ = process.Process.Kill()
		if done != nil {
			for range done {
			}
		}
	}()
	select {
	case <-output.ready:
	case <-time.After(cancellationWait):
		t.Fatal("confirmation did not start")
	}
	if err := process.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		done = nil
		if err == nil {
			t.Fatal("cancelled confirmation returned success")
		}
	case <-time.After(cancellationWait):
		t.Fatal("signal left accessible confirmation blocked on input")
	}
	if !strings.Contains(output.buffer.String(), "context canceled") {
		t.Fatalf("missing cancellation diagnostic: %s", output.buffer.String())
	}
}
