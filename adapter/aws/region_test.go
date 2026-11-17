package aws

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAccountSummaryPromptAccessible(t *testing.T) {
	for _, answer := range []string{"yes", "no"} {
		t.Run(answer, func(t *testing.T) {
			t.Setenv("ACCESSIBLE", "1")
			t.Setenv("GLAMOUR_STYLE", "dark")
			inputPath := filepath.Join(t.TempDir(), "input")
			if err := os.WriteFile(inputPath, []byte(answer+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = input.Close() })
			output, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = output.Close() })
			stdin, stdout := os.Stdin, os.Stdout
			os.Stdin, os.Stdout = input, output
			t.Cleanup(func() { os.Stdin, os.Stdout = stdin, stdout })

			summary := AccountSummary{{Region: testRegion, Instances: []Instance{{ID: "i-test", Name: "test"}}}}
			got, err := summary.Prompt(InstanceStart)
			if err != nil {
				t.Fatal(err)
			}
			if answer == "yes" {
				if len(got) != 1 || !slices.Equal(got[0].Instances, summary[0].Instances) {
					t.Errorf("confirmed summary = %v, want %v", got, summary)
				}
			} else if len(got) != 0 {
				t.Errorf("declined prompt returned instances: %v", got)
			}
			rendered, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rendered), "Would you like to proceed?") {
				t.Errorf("missing confirmation question: %s", rendered)
			}
		})
	}
}
