package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/resolve"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

type WorkspaceStatus struct {
	Workspace              workspace.Entry `json:"workspace"`
	RepoHead               string          `json:"repo_head"`
	BaseCommit             string          `json:"base_commit"`
	LastApplyRev           string          `json:"last_apply_rev,omitempty"`
	LastSyncRev            string          `json:"last_sync_rev,omitempty"`
	LastExtractRev         string          `json:"last_extract_rev,omitempty"`
	LastRefreshRev         string          `json:"last_refresh_rev,omitempty"`
	PatchesRev             string          `json:"patches_rev,omitempty"`
	PatchesHead            string          `json:"patches_head"`
	PatchesFreshness       string          `json:"patches_freshness"`
	PatchesBehind          int             `json:"patches_behind,omitempty"`
	BrowserOSTipPatchesRev string          `json:"browseros_tip_patches_rev,omitempty"`
	PendingStash           string          `json:"pending_stash,omitempty"`
	ActiveResolve          bool            `json:"active_resolve"`
	NeedsApply             []string        `json:"needs_apply"`
	NeedsUpdate            []string        `json:"needs_update"`
	Orphaned               []string        `json:"orphaned"`
	UpToDate               []string        `json:"up_to_date"`
	SyncState              string          `json:"sync_state"`
}

// InSyncButUnreproducible flags the "patches agree but orphans exist" state:
// a fresh checkout would not reproduce this workspace.
func (s *WorkspaceStatus) InSyncButUnreproducible() bool {
	return len(s.NeedsApply) == 0 && len(s.NeedsUpdate) == 0 && len(s.Orphaned) > 0
}

type OrphanGroup struct {
	Dir   string `json:"dir"`
	Count int    `json:"count"`
}

// OrphanSummary groups paths by top-level directory, largest group first.
func OrphanSummary(paths []string) []OrphanGroup {
	counts := map[string]int{}
	for _, rel := range paths {
		dir := "(root)"
		if idx := strings.IndexByte(rel, '/'); idx > 0 {
			dir = rel[:idx]
		}
		counts[dir]++
	}
	groups := make([]OrphanGroup, 0, len(counts))
	for dir, count := range counts {
		groups = append(groups, OrphanGroup{Dir: dir, Count: count})
	}
	slices.SortFunc(groups, func(a, b OrphanGroup) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Dir, b.Dir)
	})
	return groups
}

type InspectWorkspaceOptions struct {
	Workspace workspace.Entry
	Repo      *repo.Info
	Progress  Progress
}

// InspectWorkspace compares a workspace against the patch repo and classifies drift.
func InspectWorkspace(ctx context.Context, opts InspectWorkspaceOptions) (*WorkspaceStatus, error) {
	reportProgress(opts.Progress, "Inspecting workspace drift")
	head, err := git.HeadRev(ctx, opts.Repo.Root)
	if err != nil {
		return nil, err
	}
	state, err := workspace.LoadState(opts.Workspace.Path)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Loading repo patch set")
	repoSet, err := patch.LoadRepoPatchSet(opts.Repo.PatchesDir, nil)
	if err != nil {
		return nil, err
	}
	ignore, err := patch.LoadIgnoreSet(opts.Repo.Root, nil)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Building workspace patch set")
	localSet, err := patch.BuildWorkingTreePatchSet(ctx, opts.Workspace.Path, patch.WorkingTreeOptions{
		Base:   opts.Repo.BaseCommit,
		Ignore: ignore,
		Report: func(message string) { reportProgress(opts.Progress, "%s", message) },
	})
	if err != nil {
		return nil, err
	}
	status := &WorkspaceStatus{
		Workspace:      opts.Workspace,
		RepoHead:       head,
		BaseCommit:     opts.Repo.BaseCommit,
		LastApplyRev:   state.LastApplyRev,
		LastSyncRev:    state.LastSyncRev,
		LastExtractRev: state.LastExtractRev,
		LastRefreshRev: state.LastRefreshRev,
		PatchesHead:    head,
		PendingStash:   state.PendingStash,
		ActiveResolve:  resolve.Exists(opts.Workspace.Path),
	}
	status.PatchesRev, status.BrowserOSTipPatchesRev, status.PatchesFreshness, status.PatchesBehind = materializedFreshness(ctx, opts.Repo.Root, opts.Workspace.Path, state, head)
	for _, delta := range patch.Compare(repoSet, localSet) {
		switch delta.Kind {
		case patch.NeedsApply:
			status.NeedsApply = append(status.NeedsApply, delta.Path)
		case patch.NeedsUpdate:
			status.NeedsUpdate = append(status.NeedsUpdate, delta.Path)
		case patch.Orphaned:
			status.Orphaned = append(status.Orphaned, delta.Path)
		case patch.UpToDate:
			status.UpToDate = append(status.UpToDate, delta.Path)
		}
	}
	status.SyncState = inferSyncState(status)
	return status, nil
}

func materializedFreshness(ctx context.Context, repoRoot string, workspacePath string, state *workspace.State, repoHead string) (string, string, string, int) {
	tipRev := ""
	if exists, err := git.CommitExists(ctx, workspacePath, browserOSBranch); err == nil && exists {
		if trailer, trailerErr := git.CommitTrailer(ctx, workspacePath, browserOSBranch, patchesRevTrailer); trailerErr == nil {
			tipRev = trailer
		}
	}
	switch {
	case tipRev == "":
		return "", tipRev, "unknown", 0
	case state.LastRefreshRev == "":
		return tipRev, tipRev, "unknown", 0
	case state.LastRefreshRev != tipRev:
		return tipRev, tipRev, "mismatch", 0
	case tipRev == repoHead:
		return tipRev, tipRev, "fresh", 0
	default:
		ancestor, err := git.IsAncestor(ctx, repoRoot, tipRev, repoHead)
		if err != nil || !ancestor {
			return tipRev, tipRev, "mismatch", 0
		}
		behind, err := git.RevListCount(ctx, repoRoot, tipRev, repoHead)
		if err != nil || behind == 0 {
			return tipRev, tipRev, "unknown", 0
		}
		return tipRev, tipRev, fmt.Sprintf("behind %d", behind), behind
	}
}

func inferSyncState(status *WorkspaceStatus) string {
	switch {
	case status.ActiveResolve:
		return "conflicted"
	case status.LastSyncRev == "":
		return "never-synced"
	case status.LastSyncRev != status.RepoHead:
		return "needs-sync"
	case len(status.NeedsApply) > 0:
		return "drifted"
	case len(status.NeedsUpdate) > 0 || len(status.Orphaned) > 0:
		return "local-changes"
	default:
		return "synced"
	}
}
