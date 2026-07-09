package cmd

import (
	"fmt"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/engine"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/ui"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
	"github.com/spf13/cobra"
)

func init() {
	var src string
	command := &cobra.Command{
		Use:         "annotate [checkout]",
		Annotations: map[string]string{"group": "Core:"},
		Short:       "Create feature commits in a checkout",
		Example: `  browseros-patch annotate ch1
  browseros-patch annotate --src /path/to/chromium/src`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveAnnotateTarget(cmd, args, src)
			if err != nil {
				return err
			}
			info, err := repoInfo()
			if err != nil {
				return err
			}
			result, err := engine.Annotate(cmd.Context(), engine.AnnotateOptions{
				Workspace: ws,
				Repo:      info,
				Progress:  commandProgress(cmd),
			})
			if err != nil {
				return err
			}
			return renderResult(result, func() {
				printAnnotateResult(ws, result)
			})
		},
	}
	command.Flags().StringVar(&src, "src", "", srcFlagUsage)
	rootCmd.AddCommand(command)
}

func resolveAnnotateTarget(cmd *cobra.Command, args []string, src string) (workspace.Entry, error) {
	if src != "" && len(args) > 0 {
		return workspace.Entry{}, fmt.Errorf("with --src, do not pass a checkout")
	}
	return resolveWorkspace(cmd, args, src)
}

func printAnnotateResult(ws workspace.Entry, result *engine.AnnotateResult) {
	fmt.Println(ui.Title(fmt.Sprintf("Annotated %s", ws.Name)))
	fmt.Printf("%s  %s\n", ui.Muted("features file:"), result.FeaturesFile)
	fmt.Printf("%s  %d\n", ui.Muted("processed:"), result.Processed)
	fmt.Printf("%s  %d\n", ui.Muted("commits:"), result.CommitsCreated)
	fmt.Printf("%s  %d\n", ui.Muted("skipped:"), result.FeaturesSkipped)
	if len(result.Committed) > 0 {
		fmt.Println(ui.Header("Committed:"))
		for _, committed := range result.Committed {
			fmt.Printf("  %-24s %s  %d %s\n", committed.Name, shortCommit(committed.Commit), len(committed.Files), pluralFiles(len(committed.Files)))
		}
	}
	if len(result.Skipped) > 0 {
		fmt.Println(ui.Header("Skipped:"))
		for _, skipped := range result.Skipped {
			fmt.Printf("  %-24s %s\n", skipped.Name, skipped.Reason)
		}
	}
	if len(result.Unclaimed) > 0 {
		fmt.Println(ui.Warning("Unclaimed changes (no feature owns these; left uncommitted):"))
		for _, rel := range result.Unclaimed {
			fmt.Printf("  %s\n", rel)
		}
		fmt.Println(ui.Hint(`Claim them in bos_build/features.yaml or a managed-output mechanism, then re-run; "browseros-patch feature lint" checks patch coverage.`))
	}
	if result.CommitsCreated == 0 {
		fmt.Println(ui.Hint("No commits created."))
	}
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func pluralFiles(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}
