package cmd

import (
	"fmt"
	"strings"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/engine"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	command := &cobra.Command{
		Use:         "feature",
		Annotations: map[string]string{"group": "Core:"},
		Short:       "Manage patch feature registration",
	}
	command.AddCommand(newFeatureLintCommand())
	command.AddCommand(newFeatureAddCommand())
	rootCmd.AddCommand(command)
}

func newFeatureLintCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Validate every patch is claimed by exactly one feature",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := repoInfo()
			if err != nil {
				return err
			}
			result, err := engine.LintFeatures(info)
			if err != nil {
				return err
			}
			if err := renderResult(result, func() {
				if result.Valid() {
					fmt.Println(ui.Success("Feature ownership is valid"))
				} else {
					fmt.Println(ui.Warning("Feature ownership has errors"))
				}
				fmt.Printf("%s  %s\n", ui.Muted("features file:"), result.FeaturesFile)
				fmt.Printf("%s  %d\n", ui.Muted("features:"), result.Features)
				fmt.Printf("%s  %d\n", ui.Muted("patches:"), result.Patches)
				printGroup("Unclaimed", result.Unclaimed)
				if len(result.Duplicates) > 0 {
					fmt.Println(ui.Header("Duplicates:"))
					for _, duplicate := range result.Duplicates {
						fmt.Printf("  %s  %s\n", duplicate.Path, strings.Join(duplicate.Features, ", "))
					}
				}
			}); err != nil {
				return err
			}
			return result.Error()
		},
	}
}

func newFeatureAddCommand() *cobra.Command {
	var src string
	var description string
	var filesFromRange string
	command := &cobra.Command{
		Use:   "add <name> [checkout]",
		Short: "Append a feature entry from a checkout range",
		Example: `  browseros-patch feature add new-api ch1 --description "feat: new api" --files-from-range browseros..task/new-api
  browseros-patch feature add new-api --src /path/to/chromium/src --description "feat: new api" --files-from-range browseros..task/new-api`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			workspaceArgs := args[1:]
			ws, err := resolveWorkspace(cmd, workspaceArgs, src)
			if err != nil {
				return err
			}
			rangeStart, rangeEnd, err := parseRevRange(filesFromRange)
			if err != nil {
				return err
			}
			info, err := repoInfo()
			if err != nil {
				return err
			}
			progress := commandProgress(cmd)
			var result *engine.FeatureAddResult
			err = withPatchRepoLock(cmd, info, progress, func(lockedInfo *repo.Info) error {
				var addErr error
				result, addErr = engine.AddFeatureFromRange(cmd.Context(), engine.FeatureAddOptions{
					Workspace:   ws,
					Repo:        lockedInfo,
					Name:        name,
					Description: description,
					RangeStart:  rangeStart,
					RangeEnd:    rangeEnd,
				})
				return addErr
			})
			if err != nil {
				return err
			}
			return renderResult(result, func() {
				fmt.Println(ui.Title(fmt.Sprintf("Added feature %s", result.Name)))
				fmt.Printf("%s  %s\n", ui.Muted("features file:"), result.FeaturesFile)
				fmt.Printf("%s  %d\n", ui.Muted("files:"), len(result.Added))
				printGroup("Added", result.Added)
				if len(result.Excluded) > 0 {
					fmt.Println(ui.Header("Excluded existing claims:"))
					for _, excluded := range result.Excluded {
						fmt.Printf("  %s  %s\n", excluded.Path, strings.Join(excluded.Features, ", "))
					}
				}
			})
		},
	}
	command.Flags().StringVar(&src, "src", "", srcFlagUsage)
	command.Flags().StringVar(&description, "description", "", "Feature commit subject to store in features.yaml")
	command.Flags().StringVar(&filesFromRange, "files-from-range", "", "Checkout range like browseros..task/name")
	_ = command.MarkFlagRequired("description")
	_ = command.MarkFlagRequired("files-from-range")
	return command
}

func parseRevRange(spec string) (string, string, error) {
	if strings.Contains(spec, "...") {
		return "", "", fmt.Errorf(`expected --files-from-range in "<start>..<end>" form, not three-dot syntax`)
	}
	parts := strings.Split(spec, "..")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf(`expected --files-from-range in "<start>..<end>" form`)
	}
	return parts[0], parts[1], nil
}
