//go:build unix

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/creack/pty"
	"github.com/frgrisk/ec2ctl/adapter/aws"
)

type promptResponder struct {
	input    *os.File
	answers  []string
	buffer   bytes.Buffer
	answered int
}

func (r *promptResponder) String() string { return r.buffer.String() }

func (r *promptResponder) Write(data []byte) (int, error) {
	n, err := r.buffer.Write(data)
	questions := strings.Count(r.String(), "Would you like to proceed?") + strings.Count(r.String(), "Are you sure")
	if questions > r.answered && r.answered < len(r.answers) {
		_, err = r.input.WriteString(r.answers[r.answered] + "\n")
		r.answered++
	}
	return n, err
}

func TestLifecycleConfirmationGrouping(t *testing.T) {
	const west = "us-west-2"
	for _, action := range []string{aws.InstanceStart, aws.InstanceStop, terminateName} {
		t.Run(action, func(t *testing.T) {
			var mu sync.Mutex
			var mutations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				region, id := cliTestRegion, "i-00000000000000001"
				if strings.Contains(r.Header.Get("Authorization"), "/"+west+"/ec2/") {
					region, id = west, "i-00000000000000002"
				}
				w.Header().Set("Content-Type", "text/xml")
				if r.Form.Get("Action") == "DescribeInstances" {
					state := "running"
					if action == aws.InstanceStart {
						state = "stopped"
					}
					fmt.Fprintf(
						w,
						`<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>%s</instanceId><instanceState><name>%s</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`,
						id,
						state,
					)
					return
				}
				if r.Form.Get("DryRun") == "true" {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `<Response><Errors><Error><Code>DryRunOperation</Code><Message>dry run</Message></Error></Errors></Response>`)
					return
				}
				mu.Lock()
				mutations = append(mutations, region+":"+r.Form.Get("InstanceId.1"))
				mu.Unlock()
				fmt.Fprint(w, `<Response><return>true</return></Response>`)
			}))
			t.Cleanup(server.Close)
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
			answers := []string{"yes"}
			want := []string{cliTestRegion + ":i-00000000000000001", west + ":i-00000000000000002"}
			if action == aws.InstanceStop {
				answers, want = []string{"no"}, nil
			}
			if action == terminateName {
				answers, want = []string{"no", "yes"}, want[1:]
			}
			responder := &promptResponder{input: master, answers: answers}
			process := cliCommand(
				t,
				server.URL,
				action,
				regionsFlag,
				west+","+cliTestRegion,
				"i-00000000000000001",
				"i-00000000000000002",
			)
			process.Stdin, process.Stdout, process.Stderr = slave, responder, io.Discard
			if err := process.Run(); err != nil {
				t.Fatalf("command failed: %v; output=%s", err, responder.String())
			}
			if responder.answered != len(answers) {
				t.Fatalf(
					"questions=%d want=%d output=%s",
					responder.answered,
					len(answers),
					responder.String(),
				)
			}
			mu.Lock()
			defer mu.Unlock()
			slices.Sort(mutations)
			if !slices.Equal(mutations, want) {
				t.Fatalf(
					"mutations=%v want=%v output=%s",
					mutations,
					want,
					responder.String(),
				)
			}
		})
	}
}
