//go:build unix && integration

package cmd_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/frgrisk/ec2ctl/adapter/aws"
)

const (
	emulatorRegion  = "us-east-1"
	emulatorTimeout = 30 * time.Second
)

type emulator struct {
	client   *ec2.Client
	imageID  *string
	env      []string
	requests atomic.Uint64
}

func newEmulator(t *testing.T) *emulator {
	t.Helper()
	dir := os.Getenv("EC2CTL_EMULATOR_DIR")
	endpoint, err := url.Parse(os.Getenv("EC2CTL_EMULATOR_ENDPOINT"))
	if err != nil || dir == "" || endpoint.Scheme != "https" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("integration tests require a local AWS Emulator; run ./scripts/test-emulator.sh")
	}
	// Clear inherited AWS settings before loading this run's emulator credentials.
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(key, "AWS_") {
			t.Setenv(key, "")
		}
	}
	settings := map[string]string{
		"AWS_ENDPOINT_URL":                    endpoint.String(),
		"AWS_CONFIG_FILE":                     filepath.Join(dir, "config"),
		"AWS_SHARED_CREDENTIALS_FILE":         filepath.Join(dir, "emulator_credentials"),
		"AWS_CA_BUNDLE":                       filepath.Join(dir, "ca.pem"),
		"AWS_REGION":                          emulatorRegion,
		"AWS_EC2_METADATA_DISABLED":           "true",
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "false",
	}
	for key, value := range settings {
		t.Setenv(key, value)
	}
	ctx, cancel := context.WithTimeout(t.Context(), emulatorTimeout)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Account == nil || *identity.Account != "000000000000" {
		t.Fatal("endpoint is not the emulator account")
	}
	e := &emulator{client: ec2.NewFromConfig(cfg)}
	images, err := e.client.DescribeImages(ctx, &ec2.DescribeImagesInput{Filters: []types.Filter{
		{Name: new("architecture"), Values: []string{"x86_64"}},
		{Name: new("root-device-type"), Values: []string{"ebs"}},
		{Name: new("state"), Values: []string{"available"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images.Images) == 0 {
		t.Fatal("emulator has no available x86_64 EBS image")
	}
	e.imageID = images.Images[0].ImageId

	// Forward actual emulator responses; count only requests from the CLI subprocess.
	ca, err := os.ReadFile(settings["AWS_CA_BUNDLE"])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid emulator CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	proxy := httputil.NewSingleHostReverseProxy(endpoint) //nolint:gosec // The endpoint is restricted to HTTPS on 127.0.0.1 above.
	proxy.Transport = transport
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.requests.Add(1)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	ca = append(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})...)
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(bundle, ca, 0o600); err != nil { //nolint:gosec // The bundle path is inside t.TempDir.
		t.Fatal(err)
	}
	settings["AWS_CA_BUNDLE"] = bundle
	settings["AWS_ENDPOINT_URL"] = server.URL
	for key, value := range settings {
		e.env = append(e.env, key+"="+value)
	}
	return e
}

func (e *emulator) instance(t *testing.T, action string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), emulatorTimeout)
	defer cancel()
	result, err := e.client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId: e.imageID, InstanceType: types.InstanceTypeT3Micro,
		MinCount: new(int32(1)), MaxCount: new(int32(1)),
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeInstance,
			Tags:         []types.Tag{{Key: new("Name"), Value: new(t.Name())}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := *result.Instances[0].InstanceId
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.Context()), emulatorTimeout)
		defer cleanupCancel()
		if _, err := e.client.TerminateInstances(cleanupCtx, &ec2.TerminateInstancesInput{InstanceIds: []string{id}}); err != nil {
			t.Errorf("clean up instance %s: %v", id, err)
		}
	})
	e.assertState(t, id, types.InstanceStateNameRunning)
	if action == aws.InstanceStart {
		if _, err := e.client.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}}); err != nil {
			t.Fatal(err)
		}
		e.assertState(t, id, types.InstanceStateNameStopped)
	}
	return id
}

func (e *emulator) assertState(t *testing.T, id string, want types.InstanceStateName) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), emulatorTimeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := e.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
		if err != nil {
			t.Fatal(err)
		}
		got := result.Reservations[0].Instances[0].State.Name
		if got == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("instance %s state = %s, want %s", id, got, want)
		case <-ticker.C:
		}
	}
}

func expectedState(action string, confirmed bool) types.InstanceStateName {
	if !confirmed {
		if action == aws.InstanceStart {
			return types.InstanceStateNameStopped
		}
		return types.InstanceStateNameRunning
	}
	switch action {
	case aws.InstanceStart:
		return types.InstanceStateNameRunning
	case aws.InstanceStop:
		return types.InstanceStateNameStopped
	default:
		return types.InstanceStateNameTerminated
	}
}
