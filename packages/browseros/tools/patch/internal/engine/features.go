package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
	"gopkg.in/yaml.v3"
)

var featureFileCandidates = []string{
	filepath.Join("bos_build", "features.yaml"),
	filepath.Join("build", "features.yaml"),
}

type FeatureSpec struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

type FeatureClaimConflict struct {
	Path     string   `json:"path"`
	Features []string `json:"features"`
}

type FeatureLintResult struct {
	FeaturesFile string                 `json:"features_file"`
	Features     int                    `json:"features"`
	Patches      int                    `json:"patches"`
	Unclaimed    []string               `json:"unclaimed"`
	Duplicates   []FeatureClaimConflict `json:"duplicates"`
}

type FeatureAddOptions struct {
	Workspace   workspace.Entry
	Repo        *repo.Info
	Name        string
	Description string
	RangeStart  string
	RangeEnd    string
}

type FeatureAddExcluded struct {
	Path     string   `json:"path"`
	Features []string `json:"features"`
}

type FeatureAddResult struct {
	FeaturesFile string               `json:"features_file"`
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	Added        []string             `json:"added"`
	Excluded     []FeatureAddExcluded `json:"excluded"`
}

// LoadFeatures reads the repo feature registry in YAML order.
func LoadFeatures(repoInfo *repo.Info) ([]FeatureSpec, string, error) {
	featuresFile, err := FeatureFilePath(repoInfo.Root)
	if err != nil {
		return nil, "", err
	}
	features, err := loadFeaturesFile(featuresFile)
	return features, featuresFile, err
}

func FeatureFilePath(repoRoot string) (string, error) {
	for _, rel := range featureFileCandidates {
		candidate := filepath.Join(repoRoot, rel)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("features file not found in %s (tried bos_build/features.yaml, build/features.yaml)", repoRoot)
}

// LintFeatures validates that every concrete patch file has exactly one feature owner.
func LintFeatures(repoInfo *repo.Info) (*FeatureLintResult, error) {
	features, featuresFile, err := LoadFeatures(repoInfo)
	if err != nil {
		return nil, err
	}
	repoSet, err := patch.LoadRepoPatchSet(repoInfo.PatchesDir, nil)
	if err != nil {
		return nil, err
	}
	paths := patch.ScopeFromSet(repoSet)
	result := &FeatureLintResult{
		FeaturesFile: featuresFile,
		Features:     len(features),
		Patches:      len(paths),
		Unclaimed:    []string{},
		Duplicates:   []FeatureClaimConflict{},
	}
	for _, rel := range paths {
		owners := matchingFeatureNames(features, rel)
		switch len(owners) {
		case 0:
			result.Unclaimed = append(result.Unclaimed, rel)
		case 1:
		default:
			result.Duplicates = append(result.Duplicates, FeatureClaimConflict{Path: rel, Features: owners})
		}
	}
	return result, nil
}

func (r *FeatureLintResult) Valid() bool {
	return r != nil && len(r.Unclaimed) == 0 && len(r.Duplicates) == 0
}

func (r *FeatureLintResult) Error() error {
	if r.Valid() {
		return nil
	}
	var parts []string
	if len(r.Unclaimed) > 0 {
		parts = append(parts, fmt.Sprintf("unclaimed patches: %s", strings.Join(r.Unclaimed, ", ")))
	}
	if len(r.Duplicates) > 0 {
		var duplicates []string
		for _, duplicate := range r.Duplicates {
			duplicates = append(duplicates, fmt.Sprintf("%s claimed by %s", duplicate.Path, strings.Join(duplicate.Features, ", ")))
		}
		parts = append(parts, "duplicate patch claims: "+strings.Join(duplicates, "; "))
	}
	return errors.New(strings.Join(parts, "; "))
}

// AddFeatureFromRange appends one feature entry using files changed by a checkout commit range.
func AddFeatureFromRange(ctx context.Context, opts FeatureAddOptions) (*FeatureAddResult, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, fmt.Errorf("feature name is required")
	}
	if strings.TrimSpace(opts.Description) == "" {
		return nil, fmt.Errorf("feature description is required")
	}
	if opts.RangeStart == "" || opts.RangeEnd == "" {
		return nil, fmt.Errorf("feature add requires --files-from-range <start>..<end>")
	}
	features, featuresFile, err := LoadFeatures(opts.Repo)
	if err != nil {
		return nil, err
	}
	for _, feature := range features {
		if feature.Name == opts.Name {
			return nil, fmt.Errorf("feature %q already exists", opts.Name)
		}
	}
	changes, err := git.DiffNameStatusBetween(ctx, opts.Workspace.Path, opts.RangeStart, opts.RangeEnd, nil)
	if err != nil {
		return nil, err
	}
	repoSet, err := patch.LoadRepoPatchSet(opts.Repo.PatchesDir, nil)
	if err != nil {
		return nil, err
	}
	candidates := changedScope(changes)
	seen := map[string]bool{}
	var added []string
	var excluded []FeatureAddExcluded
	var missing []string
	for _, rel := range candidates {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		owners := matchingFeatureNames(features, rel)
		if len(owners) > 0 {
			excluded = append(excluded, FeatureAddExcluded{Path: rel, Features: owners})
			continue
		}
		if _, ok := repoSet[rel]; !ok {
			missing = append(missing, rel)
			continue
		}
		added = append(added, rel)
	}
	slices.Sort(added)
	slices.Sort(missing)
	slices.SortFunc(excluded, func(a, b FeatureAddExcluded) int {
		return strings.Compare(a.Path, b.Path)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("range %s..%s has files not extracted to chromium_patches: %s", opts.RangeStart, opts.RangeEnd, strings.Join(missing, ", "))
	}
	if len(added) == 0 {
		return nil, fmt.Errorf("range %s..%s has no unclaimed files to add", opts.RangeStart, opts.RangeEnd)
	}
	if err := appendFeature(featuresFile, FeatureSpec{
		Name:        opts.Name,
		Description: opts.Description,
		Files:       added,
	}); err != nil {
		return nil, err
	}
	return &FeatureAddResult{
		FeaturesFile: featuresFile,
		Name:         opts.Name,
		Description:  opts.Description,
		Added:        added,
		Excluded:     excluded,
	}, nil
}

func loadFeaturesFile(featuresFile string) ([]FeatureSpec, error) {
	body, err := os.ReadFile(featuresFile)
	if err != nil {
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
	features := make([]FeatureSpec, 0, len(featuresNode.Content)/2)
	for idx := 0; idx+1 < len(featuresNode.Content); idx += 2 {
		name := featuresNode.Content[idx].Value
		data := featuresNode.Content[idx+1]
		description := scalarValue(data, "description")
		if description == "" {
			description = name
		}
		features = append(features, FeatureSpec{
			Name:        name,
			Description: description,
			Files:       stringSequence(data, "files"),
		})
	}
	return features, nil
}

func appendFeature(featuresFile string, feature FeatureSpec) error {
	body, err := os.ReadFile(featuresFile)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return err
	}
	featuresNode := mappingValue(&root, "features")
	if featuresNode == nil || featuresNode.Kind != yaml.MappingNode {
		return fmt.Errorf("no features mapping found in %s", featuresFile)
	}
	entry, err := formatFeatureEntry(feature)
	if err != nil {
		return err
	}
	var builder strings.Builder
	builder.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		builder.WriteByte('\n')
	}
	builder.WriteByte('\n')
	for _, line := range strings.Split(strings.TrimRight(entry, "\n"), "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return os.WriteFile(featuresFile, []byte(builder.String()), 0o644)
}

func formatFeatureEntry(feature FeatureSpec) (string, error) {
	var builder strings.Builder
	encoder := yaml.NewEncoder(&builder)
	encoder.SetIndent(2)
	root := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: feature.Name},
			featureNode(feature),
		},
	}
	if err := encoder.Encode(root); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func featureNode(feature FeatureSpec) *yaml.Node {
	files := &yaml.Node{Kind: yaml.SequenceNode}
	for _, rel := range feature.Files {
		files.Content = append(files.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: rel})
	}
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "description"},
			{Kind: yaml.ScalarNode, Value: feature.Description, Style: yaml.DoubleQuotedStyle},
			{Kind: yaml.ScalarNode, Value: "files"},
			files,
		},
	}
}

func matchingFeatureNames(features []FeatureSpec, rel string) []string {
	var owners []string
	for _, feature := range features {
		if patch.PathMatches(rel, feature.Files) {
			owners = append(owners, feature.Name)
		}
	}
	return owners
}

func featurePatchPaths(feature FeatureSpec, repoSet patch.PatchSet) []string {
	seen := map[string]bool{}
	var paths []string
	scope := patch.ScopeFromSet(repoSet)
	for _, filter := range feature.Files {
		for _, rel := range scope {
			if !patch.PathMatches(rel, []string{filter}) || seen[rel] {
				continue
			}
			seen[rel] = true
			paths = append(paths, rel)
		}
	}
	return paths
}
