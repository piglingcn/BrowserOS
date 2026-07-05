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
	// Include limits annotation to these checkout paths. Nil means all
	// eligible working-tree changes.
	Include []string
	// Exclude lists checkout paths deliberately left uncommitted — skipped
	// conflicts whose files may hold partially applied hunks.
	Exclude []string
	// CommitBody is appended to every feature commit. Refresh uses this to
	// keep the browseros branch carrying its materialized Patches-Rev trailer.
	CommitBody string
	Progress   Progress
}

type AnnotateResult struct {
	Workspace       string                     `json:"workspace"`
	FeaturesFile    string                     `json:"features_file"`
	Processed       int                        `json:"processed"`
	CommitsCreated  int                        `json:"commits_created"`
	FeaturesSkipped int                        `json:"features_skipped"`
	Committed       []AnnotateCommittedFeature `json:"committed"`
	Skipped         []AnnotateSkippedFeature   `json:"skipped"`
	// Unclaimed lists changed files no feature or managed-output mechanism
	// owns; they stay uncommitted until the pipeline claims them.
	Unclaimed []string `json:"unclaimed,omitempty"`
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
// It refuses to run mid-conflict-resolution: committing a half-applied tree
// would bake reject files and partial patches into feature history.
func Annotate(ctx context.Context, opts AnnotateOptions) (*AnnotateResult, error) {
	if err := requireNoPendingResolution(ctx, opts.Workspace); err != nil {
		return nil, err
	}
	features, featuresFile, err := LoadFeatures(opts.Repo)
	if err != nil {
		return nil, err
	}
	ignore, err := patch.LoadIgnoreSet(opts.Repo.Root, nil)
	if err != nil {
		return nil, err
	}
	result := &AnnotateResult{
		Workspace:    opts.Workspace.Name,
		FeaturesFile: featuresFile,
		Committed:    []AnnotateCommittedFeature{},
		Skipped:      []AnnotateSkippedFeature{},
	}
	// One status snapshot serves the whole run; a full scan is seconds on a
	// Chromium checkout. Each feature consumes the entries it matched —
	// commitFeatureFiles settles them (committed, or staged clean). Managed
	// build outputs are filtered before feature partitioning, so what remains
	// at the end is exactly the unclaimed leftovers.
	changes, err := annotateChanges(ctx, opts.Workspace.Path, ignore, opts.Include, opts.Exclude)
	if err != nil {
		return nil, err
	}
	managedOutputs, err := loadManagedOutputPatterns(opts.Repo.Root)
	if err != nil {
		return nil, err
	}
	changes = filterManagedOutputChanges(changes, managedOutputs)
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
		matched, rest := partitionFeatureChanges(changes, feature)
		files := annotateFileSetFrom(matched)
		if len(files.report) == 0 {
			result.Skipped = append(result.Skipped, AnnotateSkippedFeature{
				Name:        feature.Name,
				Description: feature.Description,
				Reason:      "no changes",
			})
			result.FeaturesSkipped++
			continue
		}
		commit, committedFiles, committed, err := commitFeatureFiles(ctx, opts.Workspace.Path, feature.Description, opts.CommitBody, files.stage, files.commit)
		if err != nil {
			return nil, fmt.Errorf("commit feature %s: %w", feature.Name, err)
		}
		changes = rest
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
	result.Unclaimed = uniqueReportPaths(changes)
	return result, nil
}

func filterManagedOutputChanges(changes []git.FileChange, managedOutputs []string) []git.FileChange {
	if len(managedOutputs) == 0 {
		return changes
	}
	kept := changes[:0]
	for _, change := range changes {
		if annotateChangeIsManagedOutput(change, managedOutputs) {
			continue
		}
		kept = append(kept, change)
	}
	return kept
}

func annotateChangeIsManagedOutput(change git.FileChange, managedOutputs []string) bool {
	paths := changeReportPaths(change)
	if len(paths) == 0 {
		return false
	}
	for _, rel := range paths {
		if !patch.PathMatches(rel, managedOutputs) {
			return false
		}
	}
	return true
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

// annotateChanges returns working-tree changes eligible for feature commits.
// Untracked junk (reject files, logs, .browseros-patchignore patterns) is
// filtered like extract does; tracked modifications always pass through
// unless explicitly excluded.
func annotateChanges(ctx context.Context, workspacePath string, ignore *patch.IgnoreSet, include []string, exclude []string) ([]git.FileChange, error) {
	changes, err := git.StatusPorcelain(ctx, workspacePath, include)
	if err != nil {
		return nil, err
	}
	kept := changes[:0]
	for _, change := range changes {
		if change.Status == "??" && ignore.Match(change.Path) {
			continue
		}
		if len(exclude) > 0 && patch.PathMatches(patch.NormalizeChromiumPath(change.Path), exclude) {
			continue
		}
		kept = append(kept, change)
	}
	return kept, nil
}

// uniqueReportPaths flattens changes into sorted, deduplicated checkout paths.
func uniqueReportPaths(changes []git.FileChange) []string {
	seen := map[string]bool{}
	var paths []string
	for _, change := range changes {
		for _, rel := range changeReportPaths(change) {
			appendUniquePath(&paths, seen, rel)
		}
	}
	slices.Sort(paths)
	return paths
}

// partitionFeatureChanges splits the snapshot into the entries a feature
// claims and the rest, so the caller can settle the claimed ones and carry
// the remainder to the next feature without re-scanning git status.
func partitionFeatureChanges(changes []git.FileChange, feature FeatureSpec) ([]git.FileChange, []git.FileChange) {
	var matched, rest []git.FileChange
	for _, change := range changes {
		if featureMatchesChange(feature, change) {
			matched = append(matched, change)
			continue
		}
		rest = append(rest, change)
	}
	return matched, rest
}

func annotateFileSetFrom(changes []git.FileChange) annotateFileSet {
	set := annotateFileSet{}
	reportSeen := map[string]bool{}
	stageSeen := map[string]bool{}
	commitSeen := map[string]bool{}
	for _, change := range changes {
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
func commitFeatureFiles(ctx context.Context, workspacePath string, message string, body string, stagePaths []string, commitPaths []string) (string, []string, bool, error) {
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
	commit, err := git.CommitPathsWithBody(ctx, workspacePath, message, body, commitPaths)
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
	return uniqueReportPaths(changes), nil
}
