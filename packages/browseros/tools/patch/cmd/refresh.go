package cmd

import (
	"fmt"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/engine"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	var src string
	var force bool
	var remote string
	var noPull bool
	command := &cobra.Command{
		Use:         "refresh [checkout]",
		Annotations: map[string]string{"group": "Core:"},
		Short:       "Rebuild a checkout's browseros branch from canonical patches",
		Example: `  browseros-patch refresh ch1
  browseros-patch refresh ch1 --force
  browseros-patch refresh --src /path/to/chromium/src`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveWorkspace(cmd, args, src)
			if err != nil {
				return err
			}
			info, err := repoInfo()
			if err != nil {
				return err
			}
			progress := commandProgress(cmd)
			var result *engine.RefreshResult
			err = withPatchRepoLock(cmd, info, progress, func(lockedInfo *repo.Info) error {
				var refreshErr error
				result, refreshErr = engine.Refresh(cmd.Context(), engine.RefreshOptions{
					Workspace: ws,
					Repo:      lockedInfo,
					Remote:    remote,
					Force:     force,
					Pull:      !noPull,
					Progress:  progress,
				})
				return refreshErr
			})
			if err != nil {
				return err
			}
			return renderResult(result, func() {
				if result.Result == "fresh" {
					fmt.Println(ui.Success(fmt.Sprintf("%s is fresh", ws.Name)))
				} else {
					fmt.Println(ui.Title(fmt.Sprintf("Refreshed %s", ws.Name)))
				}
				fmt.Printf("%s  %s\n", ui.Muted("patches rev:"), result.PatchesRev)
				fmt.Printf("%s  %d\n", ui.Muted("features:"), result.Features)
				fmt.Printf("%s  %d\n", ui.Muted("commits:"), len(result.Commits))
				for _, warning := range result.Warnings {
					fmt.Println(ui.Warning(warning))
				}
				if len(result.Commits) > 0 {
					fmt.Println(ui.Header("Committed:"))
					for _, commit := range result.Commits {
						fmt.Printf("  %-24s %s  %d %s\n", commit.Feature, shortCommit(commit.Commit), len(commit.Files), pluralFiles(len(commit.Files)))
					}
				}
			})
		},
	}
	command.Flags().StringVar(&src, "src", "", srcFlagUsage)
	command.Flags().BoolVar(&force, "force", false, "Abandon tracked checkout state before rebuilding")
	command.Flags().BoolVar(&noPull, "no-pull", false, "Use the local patch repo state without pulling first")
	command.Flags().StringVar(&remote, "remote", "origin", "Remote to pull patch repo updates from")
	rootCmd.AddCommand(command)
}
