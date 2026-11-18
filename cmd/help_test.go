package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func TestHelpNavigationNeedsNoAWS(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	root, diagnostics, err := runCLI(t, server.URL)
	if err != nil || diagnostics != "" {
		t.Fatalf("root: %v %s", err, diagnostics)
	}
	for _, args := range [][]string{{"--help"}, {"help"}} {
		out, stderr, runErr := runCLI(t, server.URL, args...)
		if runErr != nil || out != root {
			t.Fatalf("%v differs from no-args help: %v %s\n%s", args, runErr, stderr, out)
		}
	}
	rootText := strings.Join(strings.Fields(root), " ")
	for _, fact := range []string{"Instance IDs are optional for status, start and stop.", "Omit --regions to search all enabled regions.", "status and demo output format: table or json"} {
		if !strings.Contains(rootText, fact) {
			t.Errorf("missing root guidance %q:\n%s", fact, root)
		}
	}
	previous := -1
	for _, heading := range []string{"Start here: ec2ctl demo", "Explore:", "Change instances:", "Shell and help tools:"} {
		index := strings.Index(root, heading)
		if index <= previous {
			t.Fatalf("missing/out-of-order navigation %q:\n%s", heading, root)
		}
		previous = index
	}
	for _, name := range []string{"demo", statusCmd.Name(), "start", "stop", "modify", "terminate", "completion", "help"} {
		if !strings.Contains(root, "  "+name+" ") {
			t.Errorf("missing command %s", name)
		}
		viaFlag, stderr, flagErr := runCLI(t, server.URL, name, "--help")
		viaHelp, helpStderr, helpErr := runCLI(t, server.URL, "help", name)
		if flagErr != nil || helpErr != nil || stderr != "" || viaFlag != viaHelp {
			t.Errorf("%s help entry points differ: %v %v %s %s", name, flagErr, helpErr, stderr, helpStderr)
		}
	}
	for _, alias := range []string{"ls", "list", "delete", "destroy"} {
		out, stderr, runErr := runCLI(t, server.URL, alias, "--help")
		if runErr != nil || stderr != "" || !strings.Contains(out, alias) {
			t.Errorf("alias %s: %v %s %s", alias, runErr, stderr, out)
		}
	}
	completionRoot := &cobra.Command{Use: "ec2ctl"}
	completionRoot.InitDefaultCompletionCmd()
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		out, _, runErr := runCLI(t, server.URL, "completion", shell)
		if runErr != nil || out == "" || strings.Contains(out, "\x1b") {
			t.Errorf("completion %s: %v", shell, runErr)
		}
		nested, _, nestedErr := runCLI(t, server.URL, "completion", shell, "--help")
		viaHelp, _, helpErr := runCLI(t, server.URL, "help", "completion", shell)
		if nestedErr != nil || helpErr != nil || nested != viaHelp {
			t.Fatalf("nested completion %s help differs: %v %v", shell, nestedErr, helpErr)
		}
		metadata, _, findErr := completionRoot.Find([]string{"completion", shell})
		if findErr != nil {
			t.Fatal(findErr)
		}
		for line := range strings.SplitSeq(metadata.Long, "\n") {
			if strings.HasPrefix(line, "\t") && !strings.Contains(nested, line+"\n") {
				t.Errorf("%s shell snippet changed: %q", shell, line)
			}
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("help/completion sent %d AWS requests", requests.Load())
	}
}

func TestModifyHelpDescribesActualOperation(t *testing.T) {
	out, stderr, err := runCLI(t, "http://127.0.0.1:1", "modify", "--help")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s", err, stderr)
	}
	text := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{"instance type", "explicit instance IDs", "Requires --type", "without a confirmation prompt", "INSTANCE-ID [INSTANCE-ID...]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestHelpUsesCobraMetadataAndWriters(t *testing.T) {
	const futureCommand = "future"
	root := &cobra.Command{Use: "fixture", Short: "Fixture"}
	root.AddGroup(&cobra.Group{ID: "tasks", Title: "Tasks:"})
	root.SetHelpFunc(commandHelp)
	root.SetHelpCommandGroupID("tasks")
	root.SetCompletionCommandGroupID("tasks")
	child := &cobra.Command{Use: futureCommand, GroupID: "tasks", Short: "A future task", Aliases: []string{"next"}, Run: func(*cobra.Command, []string) {}}
	root.AddCommand(child, &cobra.Command{Use: "hidden", Hidden: true, Run: func(*cobra.Command, []string) {}},
		&cobra.Command{Use: "ungrouped", Short: "An additional task", Run: func(*cobra.Command, []string) {}})
	root.PersistentFlags().String("profile", "sample", "select a `name`\nwith extra context")
	child.Flags().Bool("empty", false, "")
	child.Flags().String("secret", "", "hidden flag")
	_ = child.Flags().MarkHidden("secret")
	child.Flags().String("legacy", "value", "old option")
	_ = child.Flags().MarkDeprecated("legacy", "use --profile")
	child.Flags().Lookup("legacy").Hidden = false
	var out, diagnostics strings.Builder
	root.SetOut(&out)
	root.SetErr(&diagnostics)
	root.SetArgs([]string{futureCommand, "--help"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{futureCommand, "next", "--profile name", `(default "sample")`, "with extra context", "--empty", "--legacy string", "DEPRECATED: use --profile"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing metadata %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "--secret") || diagnostics.Len() != 0 || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("writer/hidden policy: %q %q", out.String(), diagnostics.String())
	}
	out.Reset()
	commandHelp(root, nil)
	if !strings.Contains(out.String(), "Tasks:\n  ") || !strings.Contains(out.String(), futureCommand) || strings.Contains(out.String(), "  hidden") || !strings.Contains(out.String(), "Additional commands:\n  ungrouped") {
		t.Fatalf("command metadata: %s", out.String())
	}
}
