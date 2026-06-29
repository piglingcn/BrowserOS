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
		Use:         "annotate [checkout] [feature]",
		Annotations: map[string]string{"group": "Core:"},
		Short:       "Create feature commits in a checkout",
		Example: `  browseros-patch annotate ch1
  browseros-patch annotate ch1 api
  browseros-patch annotate --src /path/to/chromium/src api`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, feature, err := resolveAnnotateTarget(cmd, args, src)
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
				Feature:   feature,
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

func resolveAnnotateTarget(cmd *cobra.Command, args []string, src string) (workspace.Entry, string, error) {
	if src != "" {
		if len(args) > 1 {
			return workspace.Entry{}, "", fmt.Errorf("with --src, pass at most one feature name")
		}
		ws, err := resolveWorkspace(cmd, nil, src)
		feature := ""
		if len(args) == 1 {
			feature = args[0]
		}
		return ws, feature, err
	}
	switch len(args) {
	case 0:
		ws, err := resolveWorkspace(cmd, nil, "")
		return ws, "", err
	case 1:
		if _, err := appState.Registry.Get(args[0]); err == nil {
			ws, resolveErr := resolveWorkspace(cmd, args, "")
			return ws, "", resolveErr
		}
		ws, detectErr := resolveWorkspace(cmd, nil, "")
		if detectErr == nil {
			return ws, args[0], nil
		}
		ws, resolveErr := resolveWorkspace(cmd, args, "")
		return ws, "", resolveErr
	default:
		ws, err := resolveWorkspace(cmd, args[:1], "")
		return ws, args[1], err
	}
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
