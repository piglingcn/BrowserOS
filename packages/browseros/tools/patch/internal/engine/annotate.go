package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type annotateFeature struct {
	Name        string
	Description string
	Files       []string
	Index       int
}

type annotateFileSet struct {
	report []string
	stage  []string
	commit []string
}

// Annotate creates Chromium checkout commits grouped by build/features.yaml.
func Annotate(ctx context.Context, opts AnnotateOptions) (*AnnotateResult, error) {
	featuresFile := filepath.Join(opts.Repo.Root, "build", "features.yaml")
	features, err := loadAnnotateFeatures(featuresFile)
	if err != nil {
		return nil, err
	}
	changes, err := annotateChanges(ctx, opts.Workspace.Path)
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
		files := modifiedFeatureFiles(changes, feature, features)
		if len(files.report) == 0 {
			result.Skipped = append(result.Skipped, AnnotateSkippedFeature{
				Name:        feature.Name,
				Description: feature.Description,
				Reason:      "no changes",
			})
			result.FeaturesSkipped++
			continue
		}
		commit, err := commitFeatureFiles(ctx, opts.Workspace.Path, feature.Description, files.stage, files.commit)
		if err != nil {
			return nil, fmt.Errorf("commit feature %s: %w", feature.Name, err)
		}
		result.Committed = append(result.Committed, AnnotateCommittedFeature{
			Name:        feature.Name,
			Description: feature.Description,
			Commit:      commit,
			Files:       files.report,
		})
		result.CommitsCreated++
	}
	return result, nil
}

func loadAnnotateFeatures(featuresFile string) ([]annotateFeature, error) {
	body, err := os.ReadFile(featuresFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("features file not found: %s", featuresFile)
		}
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	featuresNode := mappingValue(&root, "features")
	if featuresNode == nil || featuresNode.Kind != yaml.MappingNode || len(featuresNode.Content) == 0 {
		return nil, fmt.Errorf("no features found in %s", featuresFile)
	}
	features := make([]annotateFeature, 0, len(featuresNode.Content)/2)
	for idx := 0; idx+1 < len(featuresNode.Content); idx += 2 {
		name := featuresNode.Content[idx].Value
		data := featuresNode.Content[idx+1]
		description := scalarValue(data, "description")
		if description == "" {
			description = name
		}
		features = append(features, annotateFeature{
			Name:        name,
			Description: description,
			Files:       stringSequence(data, "files"),
			Index:       len(features),
		})
	}
	return features, nil
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

func modifiedFeatureFiles(changes []git.FileChange, feature annotateFeature, allFeatures []annotateFeature) annotateFileSet {
	set := annotateFileSet{}
	reportSeen := map[string]bool{}
	stageSeen := map[string]bool{}
	commitSeen := map[string]bool{}
	for _, change := range changes {
		owner := ownerFeature(change, allFeatures)
		if owner == nil || owner.Name != feature.Name {
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

func ownerFeature(change git.FileChange, features []annotateFeature) *annotateFeature {
	var owner *annotateFeature
	bestScore := -1
	for idx := range features {
		score := featureChangeScore(features[idx], change)
		if score < 0 {
			continue
		}
		if score > bestScore || score == bestScore && owner != nil && features[idx].Index > owner.Index {
			bestScore = score
			owner = &features[idx]
		}
	}
	return owner
}

func featureChangeScore(feature annotateFeature, change git.FileChange) int {
	best := -1
	for _, rel := range changeReportPaths(change) {
		if score := featurePathScore(feature, rel); score > best {
			best = score
		}
	}
	return best
}

func featurePathScore(feature annotateFeature, rel string) int {
	best := -1
	for _, scope := range feature.Files {
		if !patch.PathMatches(rel, []string{scope}) {
			continue
		}
		if len(scope) > best {
			best = len(scope)
		}
	}
	return best
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

func commitFeatureFiles(ctx context.Context, workspacePath string, message string, stagePaths []string, commitPaths []string) (string, error) {
	if err := git.AddAllPaths(ctx, workspacePath, stagePaths); err != nil {
		return "", err
	}
	return git.CommitPaths(ctx, workspacePath, message, commitPaths)
}
