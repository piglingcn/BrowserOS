package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"gopkg.in/yaml.v3"
)

type copyResourcesConfig struct {
	CopyOperations []copyResourceOperation `yaml:"copy_operations"`
}

type copyResourceOperation struct {
	Source      string `yaml:"source"`
	Destination string `yaml:"destination"`
	Type        string `yaml:"type"`
}

// loadManagedOutputPatterns returns Chromium checkout paths produced by
// BrowserOS build steps rather than by chromium_patches feature commits.
func loadManagedOutputPatterns(repoRoot string) ([]string, error) {
	seen := map[string]bool{}
	var patterns []string
	add := func(rel string) {
		appendManagedPattern(&patterns, seen, rel)
	}

	add("chrome/VERSION")
	for _, rel := range managedOverlayPaths(repoRoot) {
		add(rel)
	}
	resourcePaths, err := managedCopyResourcePaths(repoRoot)
	if err != nil {
		return nil, err
	}
	for _, rel := range resourcePaths {
		add(rel)
	}
	for _, rel := range managedStringReplacementPaths(repoRoot) {
		add(rel)
	}
	slices.Sort(patterns)
	return patterns, nil
}

func managedOverlayPaths(repoRoot string) []string {
	var roots []string
	base := filepath.Join(repoRoot, "chromium_files")
	roots = append(roots, filepath.Join(base, "common"))

	productsRoot := filepath.Join(base, "products")
	entries, err := os.ReadDir(productsRoot)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				roots = append(roots, filepath.Join(productsRoot, entry.Name()))
			}
		}
	}

	var paths []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(fullPath string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return walkErr
			}
			rel, err := filepath.Rel(root, fullPath)
			if err != nil {
				return err
			}
			normalized := patch.NormalizeChromiumPath(rel)
			normalized = strings.TrimSuffix(normalized, ".debug")
			normalized = strings.TrimSuffix(normalized, ".release")
			paths = append(paths, normalized)
			return nil
		})
	}
	return paths
}

func managedCopyResourcePaths(repoRoot string) ([]string, error) {
	configPath := filepath.Join(repoRoot, "bos_build", "config", "copy_resources.yaml")
	body, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var config copyResourcesConfig
	if err := yaml.Unmarshal(body, &config); err != nil {
		return nil, err
	}

	var paths []string
	for _, op := range config.CopyOperations {
		if strings.TrimSpace(op.Destination) == "" {
			continue
		}
		switch opType := strings.TrimSpace(op.Type); opType {
		case "", "directory":
			paths = append(paths, op.Destination)
		case "file":
			paths = append(paths, op.Destination)
		case "files":
			matches, err := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(op.Source)))
			if err != nil {
				return nil, err
			}
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || info.IsDir() {
					continue
				}
				paths = append(paths, filepath.ToSlash(filepath.Join(op.Destination, filepath.Base(match))))
			}
		}
	}
	return paths, nil
}

func managedStringReplacementPaths(repoRoot string) []string {
	path := filepath.Join(repoRoot, "bos_build", "steps", "resources", "string_replaces.py")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(body)
	assignment := strings.Index(content, "target_files")
	if assignment < 0 {
		return nil
	}
	open := strings.Index(content[assignment:], "[")
	if open < 0 {
		return nil
	}
	start := assignment + open + 1
	close := strings.Index(content[start:], "]")
	if close < 0 {
		return nil
	}
	block := content[start : start+close]
	quoted := regexp.MustCompile(`["']([^"']+)["']`)
	matches := quoted.FindAllStringSubmatch(block, -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match[1])
	}
	return paths
}

func appendManagedPattern(patterns *[]string, seen map[string]bool, raw string) {
	rel := patch.NormalizeChromiumPath(raw)
	if rel == "." || rel == "" || patch.IsInternalPath(rel) || seen[rel] {
		return
	}
	seen[rel] = true
	*patterns = append(*patterns, rel)
}
