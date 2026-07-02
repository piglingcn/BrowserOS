package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
	"gopkg.in/yaml.v3"
)

type AnnotateOptions struct {
	Workspace workspace.Entry
	Repo      *repo.Info
	Progress  Progress
}

type AnnotateResult struct {
	Workspace       string                     `json:"workspace"`
	FeaturesFile    string                     `json:"features_file"`
	Processed       int                        `json:"processed"`
	CommitsCreated  int                        `json:"commits_created"`
	FeaturesSkipped int                        `json:"features_skipped"`
	Committed       []AnnotateCommittedFeature `json:"committed"`
	Skipped         []AnnotateSkippedFeature   `json:"skipped"`
}

type AnnotateCommittedFeature struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Commit      string   `json:"commit"`
	Files       []string `json:"files"`
}

type AnnotateSkippedFeature struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

type annotateFileSet struct {
	report []string
	stage  []string
	commit []string
}

// Annotate creates Chromium checkout commits grouped by the repo feature registry.
func Annotate(ctx context.Context, opts AnnotateOptions) (*AnnotateResult, error) {
	features, featuresFile, err := LoadFeatures(opts.Repo)
	if err != nil {
		return nil, err
	}
	result := &AnnotateResult{
		Workspace:    opts.Workspace.Name,
		FeaturesFile: featuresFile,
		Committed:    []AnnotateCommittedFeature{},
		Skipped:      []AnnotateSkippedFeature{},
	}
	for _, feature := range features {
		result.Processed++
		reportProgress(opts.Progress, "Annotating feature %s", feature.Name)
		if len(feature.Files) == 0 {
			result.Skipped = append(result.Skipped, AnnotateSkippedFeature{
				Name:        feature.Name,
				Description: feature.Description,
				Reason:      "no files",
			})
			result.FeaturesSkipped++
			continue
		}
		changes, err := annotateChanges(ctx, opts.Workspace.Path)
		if err != nil {
			return nil, err
		}
		files := modifiedFeatureFiles(changes, feature)
		if len(files.report) == 0 {
			result.Skipped = append(result.Skipped, AnnotateSkippedFeature{
				Name:        feature.Name,
				Description: feature.Description,
				Reason:      "no changes",
			})
			result.FeaturesSkipped++
			continue
		}
		commit, committedFiles, committed, err := commitFeatureFiles(ctx, opts.Workspace.Path, feature.Description, files.stage, files.commit)
		if err != nil {
			return nil, fmt.Errorf("commit feature %s: %w", feature.Name, err)
		}
		if !committed {
			result.Skipped = append(result.Skipped, AnnotateSkippedFeature{
				Name:        feature.Name,
				Description: feature.Description,
				Reason:      "no changes",
			})
			result.FeaturesSkipped++
			continue
		}
		result.Committed = append(result.Committed, AnnotateCommittedFeature{
			Name:        feature.Name,
			Description: feature.Description,
			Commit:      commit,
			Files:       committedFiles,
		})
		result.CommitsCreated++
	}
	return result, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		if node.Content[idx].Value == key {
			return node.Content[idx+1]
		}
	}
	return nil
}

func scalarValue(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func stringSequence(node *yaml.Node, key string) []string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.SequenceNode {
		return nil
	}
	var items []string
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode {
			continue
		}
		rel := patch.NormalizeChromiumPath(item.Value)
		if rel != "." && rel != "" {
			items = append(items, rel)
		}
	}
	return items
}

func annotateChanges(ctx context.Context, workspacePath string) ([]git.FileChange, error) {
	return git.StatusPorcelain(ctx, workspacePath, nil)
}

func modifiedFeatureFiles(changes []git.FileChange, feature FeatureSpec) annotateFileSet {
	set := annotateFileSet{}
	reportSeen := map[string]bool{}
	stageSeen := map[string]bool{}
	commitSeen := map[string]bool{}
	for _, change := range changes {
		if !featureMatchesChange(feature, change) {
			continue
		}
		for _, rel := range changeReportPaths(change) {
			appendUniquePath(&set.report, reportSeen, rel)
		}
		for _, rel := range changeStagePaths(change) {
			appendUniquePath(&set.stage, stageSeen, rel)
		}
		for _, rel := range changeReportPaths(change) {
			appendUniquePath(&set.commit, commitSeen, rel)
		}
	}
	slices.Sort(set.report)
	slices.Sort(set.stage)
	slices.Sort(set.commit)
	return set
}

func featureMatchesChange(feature FeatureSpec, change git.FileChange) bool {
	for _, rel := range changeReportPaths(change) {
		if patch.PathMatches(rel, feature.Files) {
			return true
		}
	}
	return false
}

func changeReportPaths(change git.FileChange) []string {
	paths := []string{change.Path}
	if change.OldPath != "" {
		paths = append(paths, change.OldPath)
	}
	return normalizeAnnotatePaths(paths)
}

func changeStagePaths(change git.FileChange) []string {
	if change.Status == "??" {
		return normalizeAnnotatePaths([]string{change.Path})
	}
	if len(change.Status) < 2 {
		return changeReportPaths(change)
	}
	var paths []string
	indexStatus := change.Status[0]
	worktreeStatus := change.Status[1]
	if worktreeStatus != ' ' {
		paths = append(paths, change.Path)
		if indexStatus == ' ' && change.OldPath != "" {
			paths = append(paths, change.OldPath)
		}
	}
	return normalizeAnnotatePaths(paths)
}

func normalizeAnnotatePaths(paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, raw := range paths {
		rel := patch.NormalizeChromiumPath(raw)
		if rel == "." || rel == "" || patch.IsInternalPath(rel) {
			continue
		}
		normalized = append(normalized, rel)
	}
	return normalized
}

func appendUniquePath(paths *[]string, seen map[string]bool, rel string) {
	if seen[rel] {
		return
	}
	seen[rel] = true
	*paths = append(*paths, rel)
}

// commitFeatureFiles normalizes selected paths into the index and commits only real HEAD deltas.
func commitFeatureFiles(ctx context.Context, workspacePath string, message string, stagePaths []string, commitPaths []string) (string, []string, bool, error) {
	if err := git.AddAllPaths(ctx, workspacePath, stagePaths); err != nil {
		return "", nil, false, err
	}
	dirty, err := git.IsDirtyPaths(ctx, workspacePath, commitPaths)
	if err != nil {
		return "", nil, false, err
	}
	if !dirty {
		return "", nil, false, nil
	}
	commit, err := git.CommitPaths(ctx, workspacePath, message, commitPaths)
	if err != nil {
		return "", nil, false, err
	}
	committedFiles, err := committedFeatureFiles(ctx, workspacePath, commit, commitPaths)
	if err != nil {
		return "", nil, false, err
	}
	return commit, committedFiles, true, nil
}

func committedFeatureFiles(ctx context.Context, workspacePath string, commit string, commitPaths []string) ([]string, error) {
	changes, err := git.DiffTreeNameStatus(ctx, workspacePath, commit, commitPaths)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, change := range changes {
		for _, rel := range changeReportPaths(change) {
			appendUniquePath(&paths, seen, rel)
		}
	}
	slices.Sort(paths)
	return paths, nil
}
