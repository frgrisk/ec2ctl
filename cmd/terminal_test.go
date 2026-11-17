package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frgrisk/ec2ctl/cmd/types"
	"github.com/spf13/cobra"
)

func TestStatusUsesCommandOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-0123456789abcdef0</instanceId><instanceState><name>running</name></instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
	}))
	t.Cleanup(server.Close)
	for key, value := range map[string]string{
		"AWS_ENDPOINT_URL_EC2": server.URL, "AWS_ACCESS_KEY_ID": "test", "AWS_SECRET_ACCESS_KEY": "test",
		"AWS_SESSION_TOKEN": "", "AWS_PROFILE": "", "AWS_EC2_METADATA_DISABLED": "true",
		"AWS_CONFIG_FILE": filepath.Join(t.TempDir(), "config"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(t.TempDir(), "credentials"),
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "false",
	} {
		t.Setenv(key, value)
	}
	previousRegions, previousOutput, previousWide := regions, output, wide
	t.Cleanup(func() { regions, output, wide = previousRegions, previousOutput, previousWide })
	regions, wide = []string{cliTestRegion}, false
	for _, format := range []types.Output{types.Table, types.JSON} {
		t.Run(format.String(), func(t *testing.T) {
			output = format
			var out, diagnostics strings.Builder
			command := &cobra.Command{}
			command.SetOut(&out)
			command.SetErr(&diagnostics)
			if err := statusCmd.RunE(command, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "i-0123456789abcdef0") || diagnostics.Len() != 0 {
				t.Fatalf("output=%q diagnostics=%q", out.String(), diagnostics.String())
			}
			if strings.Contains(out.String(), "EC2 FLEET") != (format == types.Table) {
				t.Fatalf("unexpected rendering for %s: %q", format, out.String())
			}
		})
	}
}
