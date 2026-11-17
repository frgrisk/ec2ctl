package aws

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/smithy-go"
)

const testRegion = "us-east-1"

func mockEC2(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ENDPOINT_URL_EC2", server.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
}

func TestInstanceActionsDryRun(t *testing.T) {
	const instanceID = "i-0123456789abcdef0"
	const modify = "modify"
	for _, action := range []string{InstanceStart, InstanceStop, InstanceHibernate, modify} {
		for _, code := range []string{DryRunOperation, "UnauthorizedOperation"} {
			t.Run(action+"/"+code, func(t *testing.T) {
				var mu sync.Mutex
				var requests []url.Values
				mockEC2(t, func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					mu.Lock()
					requests = append(requests, r.PostForm)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/xml")
					if r.Form.Get("DryRun") == "true" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = fmt.Fprintf(w, `<Response><Errors><Error><Code>%s</Code><Message>dry run</Message></Error></Errors><RequestID>test</RequestID></Response>`, code)
						return
					}
					_, _ = fmt.Fprint(w, `<Response><requestId>test</requestId><return>true</return></Response>`)
				})

				var err error
				if action == modify {
					err = ModifyInstanceType(testRegion, "t3.small", instanceID)
				} else {
					_, err = StartStopInstance(testRegion, action, []string{instanceID})
				}
				wantRequests := 1
				if code == DryRunOperation {
					wantRequests++
					if err != nil {
						t.Fatal(err)
					}
				} else if apiErr, ok := errors.AsType[smithy.APIError](err); !ok || apiErr.ErrorCode() != code {
					t.Fatalf("error = %v, want %s", err, code)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(requests) != wantRequests {
					t.Fatalf("sent %d requests, want %d", len(requests), wantRequests)
				}
				for i, request := range requests {
					if got, want := request.Get("DryRun"), fmt.Sprint(i == 0); got != want {
						t.Errorf("request %d: DryRun = %q, want %q", i, got, want)
					}
					wantAction, idKey := "StopInstances", "InstanceId.1"
					switch action {
					case InstanceStart:
						wantAction = "StartInstances"
					case modify:
						wantAction, idKey = "ModifyInstanceAttribute", "InstanceId"
						if got := request.Get("InstanceType.Value"); got != "t3.small" {
							t.Errorf("instance type = %q", got)
						}
					}
					if request.Get("Action") != wantAction || request.Get(idKey) != instanceID {
						t.Errorf("unexpected action or instance ID: %v", request)
					}
					if got, want := request.Get("Hibernate"), action == InstanceHibernate; (got == "true") != want {
						t.Errorf("Hibernate = %q, want %t", got, want)
					}
				}
			})
		}
	}
}

func TestGetDeployedInstancesSortOrder(t *testing.T) {
	const dev = "dev"
	instances := []Instance{
		{ID: "prod-a", Environment: "prod", Name: "a"},
		{ID: "dev-z", Environment: dev, Name: "z"},
		{ID: "dev-a-first", Environment: dev, Name: "a"},
		{ID: "dev-a-second", Environment: dev, Name: "a"},
	}
	mockEC2(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		switch action := r.Form.Get("Action"); action {
		case "DescribeInstances":
			_, _ = fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet>`)
			for _, instance := range instances {
				_, _ = fmt.Fprintf(w, `<item><instanceId>%s</instanceId><instanceState><code>16</code><name>running</name></instanceState><tagSet><item><key>Environment</key><value>%s</value></item><item><key>Name</key><value>%s</value></item></tagSet></item>`, instance.ID, instance.Environment, instance.Name)
			}
			_, _ = fmt.Fprint(w, `</instancesSet></item></reservationSet></DescribeInstancesResponse>`)
		case "DescribeInstanceStatus":
			_, _ = fmt.Fprint(w, `<DescribeInstanceStatusResponse/>`)
		case "DescribeSpotInstanceRequests":
			_, _ = fmt.Fprint(w, `<DescribeSpotInstanceRequestsResponse/>`)
		default:
			t.Errorf("unexpected API action: %s", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	summary, err := GetDeployedInstances(testRegion, nil, InstanceStatus, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dev-a-first", "dev-a-second", "dev-z", "prod-a"}
	if got := IDs(summary.Instances); !slices.Equal(got, want) {
		t.Errorf("instance order = %v, want %v", got, want)
	}
}

func TestInventoryPaginationAndMissingFields(t *testing.T) {
	var pages atomic.Int32
	mockEC2(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if action := r.Form.Get("Action"); action != "DescribeInstances" {
			t.Errorf("on-demand inventory requested unnecessary permission: %s", action)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		pages.Add(1)
		w.Header().Set("Content-Type", "text/xml")
		if r.Form.Get("NextToken") == "" {
			fmt.Fprint(w, `<DescribeInstancesResponse><nextToken>page2</nextToken><reservationSet><item><instancesSet><item><instanceId>i-00000000000000001</instanceId><instanceState><name>running</name></instanceState><privateIpAddress>10.0.0.1</privateIpAddress><placement><availabilityZone>us-east-1a</availabilityZone></placement><hibernationOptions><configured>true</configured></hibernationOptions></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
			return
		}
		if r.Form.Get("NextToken") != "page2" {
			t.Errorf("unexpected page token: %s", r.Form.Get("NextToken"))
		}
		fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-00000000000000002</instanceId><instanceState><name>stopped</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
	})
	summary, err := GetDeployedInstances(testRegion, nil, InstanceStatus, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pages.Load() != 2 || len(summary.Instances) != 2 {
		t.Fatalf("pages=%d instances=%v", pages.Load(), summary.Instances)
	}
	if first := summary.Instances[0]; first.IP != "10.0.0.1" || first.AZ != "us-east-1a" || !first.Hibernation {
		t.Fatalf("missing first instance details: %+v", first)
	}
	if second := summary.Instances[1]; second.IP != "" || second.AZ != "" || second.Hibernation {
		t.Fatalf("fields leaked across instances: %+v", second)
	}
}
