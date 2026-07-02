package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

const (
	browserOSBranch   = "browseros"
	patchesRevTrailer = "Patches-Rev"
)

type RefreshOptions struct {
	Workspace workspace.Entry
	Repo      *repo.Info
	Remote    string
	Force     bool
	Pull      bool
	Progress  Progress
}

type RefreshCommit struct {
	Feature     string   `json:"feature"`
	Description string   `json:"description"`
	Commit      string   `json:"commit"`
	Files       []string `json:"files"`
}

type RefreshResult struct {
	Workspace  string          `json:"workspace"`
	Result     string          `json:"result"`
	Features   int             `json:"features"`
	Commits    []RefreshCommit `json:"commits"`
	PatchesRev string          `json:"patches_rev"`
	Warnings   []string        `json:"warnings"`
}

// Refresh rebuilds the local browseros branch as feature commits from the patch repo.
func Refresh(ctx context.Context, opts RefreshOptions) (*RefreshResult, error) {
	repoInfo := opts.Repo
	if opts.Remote == "" {
		opts.Remote = "origin"
	}
	if opts.Pull {
		var err error
		repoInfo, err = pullPatchRepoForRefresh(ctx, opts, repoInfo)
		if err != nil {
			return nil, err
		}
	}
	repoRev, err := git.HeadRev(ctx, repoInfo.Root)
	if err != nil {
		return nil, err
	}
	repoCommitTime, err := git.CommitUnixTime(ctx, repoInfo.Root, repoRev)
	if err != nil {
		return nil, err
	}
	commitEnv := refreshCommitEnv(repoCommitTime)
	features, _, err := LoadFeatures(repoInfo)
	if err != nil {
		return nil, err
	}
	lint, err := LintFeatures(repoInfo)
	if err != nil {
		return nil, err
	}
	if err := lint.Error(); err != nil {
		return nil, err
	}
	state, err := workspace.LoadState(opts.Workspace.Path)
	if err != nil {
		return nil, err
	}
	branch, err := git.CurrentBranch(ctx, opts.Workspace.Path)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(branch, "task/") && !opts.Force {
		return nil, fmt.Errorf("checkout %s is on %s; task branches are leased, use --force only when abandoning that task", opts.Workspace.Name, branch)
	}
	dirty, err := git.IsTrackedDirty(ctx, opts.Workspace.Path)
	if err != nil {
		return nil, err
	}
	if dirty && !opts.Force {
		return nil, fmt.Errorf("checkout %s has tracked changes; commit/stash them or use --force to reset tracked state", opts.Workspace.Name)
	}
	if dirty && opts.Force {
		reportProgress(opts.Progress, "Resetting tracked checkout changes")
		if err := git.ResetHard(ctx, opts.Workspace.Path); err != nil {
			return nil, err
		}
	}
	if fresh, err := refreshIsFresh(ctx, opts.Workspace.Path, state, repoRev); err != nil {
		return nil, err
	} else if fresh {
		if branch != browserOSBranch {
			if err := git.CheckoutBranch(ctx, opts.Workspace.Path, browserOSBranch); err != nil {
				return nil, err
			}
		}
		return &RefreshResult{
			Workspace:  opts.Workspace.Name,
			Result:     "fresh",
			Features:   len(features),
			Commits:    []RefreshCommit{},
			PatchesRev: repoRev,
			Warnings:   []string{},
		}, nil
	}
	repoSet, err := patch.LoadRepoPatchSet(repoInfo.PatchesDir, nil)
	if err != nil {
		return nil, err
	}
	collisions, err := untrackedPatchCollisions(ctx, opts.Workspace.Path, repoInfo.BaseCommit, repoSet)
	if err != nil {
		return nil, err
	}
	if len(collisions) > 0 {
		return nil, fmt.Errorf("untracked files collide with patch targets; move them aside before refresh: %s", strings.Join(collisions, ", "))
	}
	warnings := baseChangeWarnings(ctx, opts.Workspace.Path, state.BaseCommit, repoInfo.BaseCommit)
	reportProgress(opts.Progress, "Checking out base %s", shortRev(repoInfo.BaseCommit))
	if exists, err := git.CommitExists(ctx, opts.Workspace.Path, repoInfo.BaseCommit); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("BASE_COMMIT %s is not present in checkout %s", repoInfo.BaseCommit, opts.Workspace.Path)
	}
	if err := git.CheckoutDetached(ctx, opts.Workspace.Path, repoInfo.BaseCommit); err != nil {
		return nil, err
	}
	result := &RefreshResult{
		Workspace:  opts.Workspace.Name,
		Result:     "refreshed",
		Features:   len(features),
		Commits:    []RefreshCommit{},
		PatchesRev: repoRev,
		Warnings:   warnings,
	}
	for _, feature := range features {
		paths := featurePatchPaths(feature, repoSet)
		if len(paths) == 0 {
			continue
		}
		reportProgress(opts.Progress, "Materializing feature %s", feature.Name)
		if err := applyFeaturePatchSet(ctx, opts.Workspace.Path, repoInfo, repoSet, paths); err != nil {
			return nil, refreshMaterializeError("apply feature "+feature.Name, err)
		}
		stagePaths := stagePathsForPatches(repoSet, paths)
		if err := git.AddAllPaths(ctx, opts.Workspace.Path, stagePaths); err != nil {
			return nil, refreshMaterializeError("stage feature "+feature.Name, err)
		}
		dirty, err := git.IsDirtyPaths(ctx, opts.Workspace.Path, stagePaths)
		if err != nil {
			return nil, refreshMaterializeError("check feature "+feature.Name, err)
		}
		if !dirty {
			continue
		}
		commit, err := git.CommitIndexWithBodyEnv(ctx, opts.Workspace.Path, feature.Description, patchesRevTrailer+": "+repoRev, commitEnv)
		if err != nil {
			return nil, refreshMaterializeError("commit feature "+feature.Name, err)
		}
		result.Commits = append(result.Commits, RefreshCommit{
			Feature:     feature.Name,
			Description: feature.Description,
			Commit:      commit,
			Files:       paths,
		})
	}
	if len(result.Commits) == 0 {
		commit, err := git.CommitIndexWithBodyEnv(ctx, opts.Workspace.Path, "chore: materialize browseros patches", patchesRevTrailer+": "+repoRev, commitEnv)
		if err != nil {
			return nil, refreshMaterializeError("commit empty materialization", err)
		}
		result.Commits = append(result.Commits, RefreshCommit{
			Feature:     "materialization",
			Description: "chore: materialize browseros patches",
			Commit:      commit,
			Files:       []string{},
		})
	}
	reportProgress(opts.Progress, "Moving browseros branch")
	if err := git.ForceBranch(ctx, opts.Workspace.Path, browserOSBranch, "HEAD"); err != nil {
		return nil, refreshMaterializeError("move browseros branch", err)
	}
	if err := git.CheckoutBranch(ctx, opts.Workspace.Path, browserOSBranch); err != nil {
		return nil, refreshMaterializeError("checkout browseros branch", err)
	}
	state.BaseCommit = repoInfo.BaseCommit
	state.LastRefreshRev = repoRev
	state.LastRefreshAt = time.Now().UTC()
	if err := workspace.SaveState(opts.Workspace.Path, state); err != nil {
		return nil, err
	}
	return result, nil
}

func refreshMaterializeError(step string, err error) error {
	return fmt.Errorf("%s: %w; checkout may be left in a partial refresh, rerun refresh --force to recover", step, err)
}

func pullPatchRepoForRefresh(ctx context.Context, opts RefreshOptions, repoInfo *repo.Info) (*repo.Info, error) {
	reportProgress(opts.Progress, "Checking patch repo status")
	dirty, err := git.IsDirty(ctx, repoInfo.Root)
	if err != nil {
		return nil, err
	}
	if dirty {
		return nil, fmt.Errorf("patches repo has uncommitted changes; commit or stash them before refreshing")
	}
	branch, err := git.CurrentBranch(ctx, repoInfo.Root)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Pulling patch repo from %s/%s", opts.Remote, branch)
	if err := git.PullRebase(ctx, repoInfo.Root, opts.Remote, branch); err != nil {
		return nil, err
	}
	return repo.Load(repoInfo.Root)
}

func refreshIsFresh(ctx context.Context, workspacePath string, state *workspace.State, repoRev string) (bool, error) {
	if state.LastRefreshRev != repoRev {
		return false, nil
	}
	exists, err := git.CommitExists(ctx, workspacePath, browserOSBranch)
	if err != nil || !exists {
		return false, err
	}
	trailer, err := git.CommitTrailer(ctx, workspacePath, browserOSBranch, patchesRevTrailer)
	if err != nil {
		return false, err
	}
	return trailer == repoRev, nil
}

func applyFeaturePatchSet(ctx context.Context, workspacePath string, repoInfo *repo.Info, repoSet patch.PatchSet, paths []string) error {
	for _, rel := range paths {
		patchFile := repoSet[rel]
		if patchFile.OldPath != "" {
			if err := git.ResetPathToCommit(ctx, workspacePath, repoInfo.BaseCommit, patchFile.OldPath); err != nil {
				return err
			}
		}
		if err := git.ResetPathToCommit(ctx, workspacePath, repoInfo.BaseCommit, patchFile.Path); err != nil {
			return err
		}
		if err := applySingleOperation(ctx, workspacePath, patchFile); err != nil {
			return err
		}
	}
	return nil
}

func stagePathsForPatches(repoSet patch.PatchSet, paths []string) []string {
	seen := map[string]bool{}
	var stage []string
	for _, rel := range paths {
		patchFile := repoSet[rel]
		if patchFile.OldPath != "" && !seen[patchFile.OldPath] {
			seen[patchFile.OldPath] = true
			stage = append(stage, patchFile.OldPath)
		}
		if !seen[patchFile.Path] {
			seen[patchFile.Path] = true
			stage = append(stage, patchFile.Path)
		}
	}
	return stage
}

func untrackedPatchCollisions(ctx context.Context, workspacePath string, base string, repoSet patch.PatchSet) ([]string, error) {
	untracked, err := git.ListUntracked(ctx, workspacePath, nil)
	if err != nil {
		return nil, err
	}
	if len(untracked) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var collisions []string
	for _, patchFile := range repoSet {
		for _, rel := range []string{patchFile.OldPath, patchFile.Path} {
			if rel == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			existsAtBase, err := git.FileExistsAtCommit(ctx, workspacePath, base, rel)
			if err != nil {
				return nil, err
			}
			if existsAtBase {
				continue
			}
			if collidesWithUntracked(rel, untracked) {
				collisions = append(collisions, rel)
			}
		}
	}
	slices.Sort(collisions)
	return collisions, nil
}

func collidesWithUntracked(rel string, untracked []string) bool {
	for _, candidate := range untracked {
		if candidate == rel || strings.HasPrefix(candidate, rel+"/") || strings.HasPrefix(rel, candidate+"/") {
			return true
		}
	}
	return false
}

func baseChangeWarnings(ctx context.Context, workspacePath string, previousBase string, nextBase string) []string {
	if previousBase == "" || previousBase == nextBase {
		return nil
	}
	changes, err := git.DiffNameStatusBetween(ctx, workspacePath, previousBase, nextBase, []string{"DEPS", filepath.ToSlash(filepath.Join("chrome", "VERSION"))})
	if err != nil || len(changes) == 0 {
		return nil
	}
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return []string{fmt.Sprintf("BASE_COMMIT changed and %s differ; run gclient sync before relying on build outputs", strings.Join(paths, ", "))}
}

func shortRev(rev string) string {
	if len(rev) <= 12 {
		return rev
	}
	return rev[:12]
}

func refreshCommitEnv(unixTime string) []string {
	date := "@" + unixTime + " +0000"
	return []string{
		"GIT_AUTHOR_NAME=BrowserOS Patch Tool",
		"GIT_AUTHOR_EMAIL=patches@browseros.local",
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=BrowserOS Patch Tool",
		"GIT_COMMITTER_EMAIL=patches@browseros.local",
		"GIT_COMMITTER_DATE=" + date,
	}
}
