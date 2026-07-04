package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/resolve"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

type ApplyOptions struct {
	Workspace  workspace.Entry
	Repo       *repo.Info
	Reset      bool
	ChangedRef string
	RangeEnd   string
	Filters    []string
	Mode       string
	// RestorePendingStash carries a rebase-mode sync's intent through a
	// conflict pause: continue/skip restore the parked stash on completion.
	RestorePendingStash bool
	// AutoAnnotate creates feature commits once the apply completes without
	// conflicts. Like RestorePendingStash, the intent survives a conflict
	// pause so continue/skip finish with the same annotation.
	AutoAnnotate bool
	Progress     Progress
}

type AnnotationOutcome struct {
	Annotate *AnnotateResult `json:"annotate,omitempty"`
	// AnnotateError reports an auto-annotate failure without discarding the
	// main command outcome: the patches landed and state is already settled,
	// so the recovery is a standalone annotate, not a retry.
	AnnotateError string `json:"annotate_error,omitempty"`
	// AnnotateSkipped names why auto-annotate did not run, so a missing
	// annotate section is explained.
	AnnotateSkipped string `json:"annotate_skipped,omitempty"`
}

type ApplyResult struct {
	Workspace          string              `json:"workspace"`
	Mode               string              `json:"mode"`
	BaseCommit         string              `json:"base_commit"`
	RepoRev            string              `json:"repo_rev"`
	Applied            []string            `json:"applied"`
	ResetPaths         []string            `json:"reset_paths"`
	Orphaned           []string            `json:"orphaned,omitempty"`
	StashRestored      bool                `json:"stash_restored,omitempty"`
	StashConflict      bool                `json:"stash_conflict,omitempty"`
	StashConflictFiles []string            `json:"stash_conflict_files,omitempty"`
	Conflicts          []resolve.Operation `json:"conflicts,omitempty"`
	AnnotationOutcome
}

func Apply(ctx context.Context, opts ApplyOptions) (*ApplyResult, error) {
	if err := requireNoPendingResolution(ctx, opts.Workspace); err != nil {
		return nil, err
	}
	repoRev, err := git.HeadRev(ctx, opts.Repo.Root)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Inspecting workspace changes")
	ops, orphaned, err := buildApplyOperations(ctx, opts)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Applying %d patch %s", len(ops), plural(len(ops), "operation", "operations"))
	result := &ApplyResult{
		Workspace:  opts.Workspace.Name,
		Mode:       applyMode(opts),
		BaseCommit: opts.Repo.BaseCommit,
		RepoRev:    repoRev,
		Orphaned:   orphaned,
	}
	if len(ops) == 0 {
		if err := finishApply(ctx, opts, repoRev, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	next, err := applyOperationRange(ctx, opts.Workspace, opts.Repo, ops, 0, nil, nil, opts.RestorePendingStash, opts.AutoAnnotate, result, opts.Progress)
	if err != nil {
		return nil, err
	}
	if next < len(ops) {
		return result, nil
	}
	if err := finishApply(ctx, opts, repoRev, result); err != nil {
		return nil, err
	}
	return result, nil
}

// requireNoPendingResolution stops a new run from operating on a paused or
// conflicted checkout: a pending resolve state would be silently clobbered
// (stranding continue/skip and losing the run's persisted intent), unmerged
// index entries mean a stash pop is waiting on the user, and leftover conflict
// markers mean a stash rebase left work half-merged (git leaves those as plain
// modified files, not unmerged entries). Committing or re-applying over any of
// these bakes conflict debris into the tree.
func requireNoPendingResolution(ctx context.Context, ws workspace.Entry) error {
	if resolve.Exists(ws.Path) {
		return fmt.Errorf(
			"conflict resolution is in progress for %s; finish it with \"browseros-patch continue\", \"browseros-patch skip\", or \"browseros-patch abort\" first",
			ws.Name)
	}
	if err := requireNoPendingStashConflict(ctx, ws); err != nil {
		return err
	}
	unmerged, err := git.UnmergedFiles(ctx, ws.Path)
	if err != nil {
		return err
	}
	if len(unmerged) > 0 {
		return fmt.Errorf(
			"%s has unresolved merge conflicts (%s); resolve them before running this command",
			ws.Name, strings.Join(unmerged, ", "))
	}
	markers, err := git.ConflictMarkerFiles(ctx, ws.Path)
	if err != nil {
		return err
	}
	if len(markers) > 0 {
		return fmt.Errorf(
			"%s has leftover conflict markers (%s); resolve them (then \"git stash drop\" if a sync left a stash) before running this command",
			ws.Name, strings.Join(markers, ", "))
	}
	return nil
}

func requireNoPendingStashConflict(ctx context.Context, ws workspace.Entry) error {
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		return err
	}
	if !state.PendingStashConflict {
		return nil
	}
	if state.PendingStash != "" {
		live, err := git.StashEntryExists(ctx, ws.Path, state.PendingStash)
		if err != nil {
			return err
		}
		if live {
			files := strings.Join(state.PendingStashConflictFiles, ", ")
			if files == "" {
				files = "unknown files"
			}
			return fmt.Errorf(
				"%s has unresolved stashed local changes (%s); resolve them, then \"git stash drop\" before running this command",
				ws.Name, files)
		}
	}
	state.PendingStash = ""
	state.PendingStashConflict = false
	state.PendingStashConflictFiles = nil
	return workspace.SaveState(ws.Path, state)
}

// finishApply records completion state and, when requested, turns the applied
// changes into feature commits in the same run.
func finishApply(ctx context.Context, opts ApplyOptions, repoRev string, result *ApplyResult) error {
	if err := markApplyComplete(opts.Workspace.Path, opts.Repo.BaseCommit, repoRev); err != nil {
		return err
	}
	if err := clearResolveState(opts.Workspace.Path); err != nil {
		return err
	}
	if !opts.AutoAnnotate {
		return nil
	}
	annotateApplied(ctx, opts.Workspace, opts.Repo, nil, &result.AnnotationOutcome, opts.Progress)
	return nil
}

// annotateApplied runs the auto-annotate step of a completed apply. A repo
// without a features registry skips with a recorded reason, and a failure is
// recorded on the result instead of returned: the apply itself succeeded and
// its state is already settled, so the caller must still see what was applied.
// A parked --no-rebase stash needs no special case here — its local changes
// sit in the stash, not the worktree, so the tree is pure patches and annotate
// commits them correctly; the shared guard blocks the unsafe case (conflict
// markers from a stash rebase) inside Annotate.
func annotateApplied(ctx context.Context, ws workspace.Entry, repoInfo *repo.Info, exclude []string, outcome *AnnotationOutcome, progress Progress) {
	annotateWorkspaceChanges(ctx, ws, repoInfo, nil, exclude, outcome, progress, "")
}

func annotateWorkspaceChanges(ctx context.Context, ws workspace.Entry, repoInfo *repo.Info, include []string, exclude []string, outcome *AnnotationOutcome, progress Progress, commitBody string) {
	if _, err := FeatureFilePath(repoInfo.Root); err != nil {
		outcome.AnnotateSkipped = "no features registry"
		return
	}
	annotateResult, err := Annotate(ctx, AnnotateOptions{
		Workspace:  ws,
		Repo:       repoInfo,
		Include:    include,
		Exclude:    exclude,
		CommitBody: commitBody,
		Progress:   progress,
	})
	if err != nil {
		outcome.AnnotateError = err.Error()
		if len(exclude) > 0 {
			// A standalone annotate would not know about these: warn before
			// the user follows the recovery hint and sweeps them in.
			outcome.AnnotateError += fmt.Sprintf(
				"; skipped conflicts remain uncommitted (%s) — resolve or reset them before annotating",
				strings.Join(exclude, ", "))
		}
		return
	}
	outcome.Annotate = annotateResult
}

type ContinueOptions struct {
	Workspace workspace.Entry
	Progress  Progress
}

// Continue resumes a saved patch application after the current conflict is resolved.
func Continue(ctx context.Context, opts ContinueOptions) (*ApplyResult, error) {
	ws := opts.Workspace
	state, err := resolve.Load(ws.Path)
	if err != nil {
		return nil, err
	}
	repoInfo, err := repo.Load(state.RepoRoot)
	if err != nil {
		return nil, err
	}
	current, err := state.CurrentOperation()
	if err != nil {
		return nil, err
	}
	if err := verifyResolved(ctx, ws.Path, repoInfo, current, state.BaseCommit); err != nil {
		return nil, err
	}
	state.Resolved = append(state.Resolved, current.ChromiumPath)
	result := &ApplyResult{
		Workspace:  ws.Name,
		Mode:       state.Mode,
		BaseCommit: state.BaseCommit,
		RepoRev:    state.RepoRev,
		Applied:    append([]string{}, state.Resolved...),
		Conflicts:  nil,
	}
	reportProgress(opts.Progress, "Continuing patch resolution")
	next, err := applyOperationRange(ctx, ws, repoInfo, state.Operations, state.Current+1, state.Resolved, state.Skipped, state.RestorePendingStash, state.AutoAnnotate, result, opts.Progress)
	if err != nil {
		return nil, err
	}
	if next >= len(state.Operations) && len(result.Conflicts) == 0 {
		if err := finishResolvedRun(ctx, ws, repoInfo, state, result, opts.Progress); err != nil {
			return nil, err
		}
	}
	return result, nil
}

type SkipOptions struct {
	Workspace workspace.Entry
	Progress  Progress
}

// Skip records the current conflict as skipped and resumes the remaining patch operations.
func Skip(ctx context.Context, opts SkipOptions) (*ApplyResult, error) {
	ws := opts.Workspace
	state, err := resolve.Load(ws.Path)
	if err != nil {
		return nil, err
	}
	repoInfo, err := repo.Load(state.RepoRoot)
	if err != nil {
		return nil, err
	}
	current, err := state.CurrentOperation()
	if err != nil {
		return nil, err
	}
	state.Skipped = append(state.Skipped, current.ChromiumPath)
	result := &ApplyResult{
		Workspace:  ws.Name,
		Mode:       state.Mode,
		BaseCommit: state.BaseCommit,
		RepoRev:    state.RepoRev,
		Applied:    append([]string{}, state.Resolved...),
	}
	reportProgress(opts.Progress, "Skipping current conflict")
	next, err := applyOperationRange(ctx, ws, repoInfo, state.Operations, state.Current+1, state.Resolved, state.Skipped, state.RestorePendingStash, state.AutoAnnotate, result, opts.Progress)
	if err != nil {
		return nil, err
	}
	if next >= len(state.Operations) && len(result.Conflicts) == 0 {
		if err := finishResolvedRun(ctx, ws, repoInfo, state, result, opts.Progress); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// finishResolvedRun closes out a conflict loop that reached the last
// operation: record completion, drop the resolve state, then honor the
// original run's intent (feature commits for an apply, stash restore for a
// rebase-mode sync). Annotate runs before the stash restore so restored local
// changes are never swept into feature commits, and skipped conflicts are
// excluded — their files may hold partially applied hunks the user still has
// to reconcile.
func finishResolvedRun(ctx context.Context, ws workspace.Entry, repoInfo *repo.Info, state *resolve.State, result *ApplyResult, progress Progress) error {
	if err := markApplyComplete(ws.Path, state.BaseCommit, state.RepoRev); err != nil {
		return err
	}
	if err := resolve.Delete(ws.Path); err != nil {
		return err
	}
	if state.AutoAnnotate {
		annotateApplied(ctx, ws, repoInfo, skippedExcludePaths(state), &result.AnnotationOutcome, progress)
	}
	if state.RestorePendingStash {
		if err := restorePendingStash(ctx, ws.Path, result, progress); err != nil {
			return err
		}
	}
	return nil
}

// skippedExcludePaths returns every checkout path a skipped conflict may have
// touched. A skipped rename leaves the old path deleted and the new path
// half-written, so both must be excluded from auto-annotate — otherwise the
// old-path deletion gets committed into a feature as a broken half-rename.
func skippedExcludePaths(state *resolve.State) []string {
	if len(state.Skipped) == 0 {
		return nil
	}
	skipped := make(map[string]bool, len(state.Skipped))
	for _, rel := range state.Skipped {
		skipped[rel] = true
	}
	seen := map[string]bool{}
	var paths []string
	add := func(rel string) {
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		paths = append(paths, rel)
	}
	for _, op := range state.Operations {
		if !skipped[op.ChromiumPath] {
			continue
		}
		add(op.ChromiumPath)
		add(op.OldPath)
	}
	return paths
}

// restorePendingStash pops a stash a conflicted sync left behind once the
// last operation completes, so the rebase loop ends with local changes back
// in the tree. A pop conflict keeps the stash pending and is reported on the
// result instead of failing.
func restorePendingStash(ctx context.Context, workspacePath string, result *ApplyResult, progress Progress) error {
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		return err
	}
	if state.PendingStash == "" {
		return nil
	}
	reportProgress(progress, "Rebasing stashed local changes")
	if err := git.StashRebase(ctx, workspacePath, state.PendingStash); err != nil {
		if errors.Is(err, git.ErrStashNotFound) {
			state.PendingStash = ""
			return workspace.SaveState(workspacePath, state)
		}
		var conflict *git.StashConflictError
		if !errors.As(err, &conflict) {
			return err
		}
		result.StashConflict = true
		result.StashConflictFiles = conflict.Files
		return nil
	}
	result.StashRestored = true
	state.PendingStash = ""
	return workspace.SaveState(workspacePath, state)
}

func Abort(ctx context.Context, ws workspace.Entry) error {
	state, err := resolve.Load(ws.Path)
	if err != nil {
		return err
	}
	for idx := 0; idx < len(state.Operations); idx++ {
		op := state.Operations[idx]
		if op.OldPath != "" {
			if err := git.ResetPathToCommit(ctx, ws.Path, state.BaseCommit, op.OldPath); err != nil {
				return err
			}
		}
		if err := git.ResetPathToCommit(ctx, ws.Path, state.BaseCommit, op.ChromiumPath); err != nil {
			return err
		}
		if op.RejectPath != "" {
			_ = os.Remove(op.RejectPath)
		}
	}
	workspaceState, err := workspace.LoadState(ws.Path)
	if err != nil {
		return err
	}
	if err := resolve.Delete(ws.Path); err != nil {
		return err
	}
	if workspaceState.PendingStash == "" {
		return nil
	}
	if err := git.StashPop(ctx, ws.Path, workspaceState.PendingStash); err != nil {
		if !errors.Is(err, git.ErrStashNotFound) {
			return err
		}
		// Stale record (stash gone): clearing it is the correct rollback.
	}
	workspaceState.PendingStash = ""
	return workspace.SaveState(ws.Path, workspaceState)
}

func buildApplyOperations(ctx context.Context, opts ApplyOptions) ([]resolve.Operation, []string, error) {
	repoSet, err := patch.LoadRepoPatchSet(opts.Repo.PatchesDir, opts.Filters)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case opts.Reset:
		return operationsFromPatchSet(repoSet), nil, nil
	case opts.ChangedRef != "":
		changes, err := repoPatchChanges(ctx, opts.Repo, opts.ChangedRef, opts.RangeEnd)
		if err != nil {
			return nil, nil, err
		}
		return operationsFromChanges(repoSet, changes, opts.Filters), nil, nil
	default:
		ignore, err := patch.LoadIgnoreSet(opts.Repo.Root, nil)
		if err != nil {
			return nil, nil, err
		}
		localSet, err := patch.BuildWorkingTreePatchSet(ctx, opts.Workspace.Path, patch.WorkingTreeOptions{
			Base:    opts.Repo.BaseCommit,
			Filters: opts.Filters,
			Ignore:  ignore,
			Report:  func(message string) { reportProgress(opts.Progress, "%s", message) },
		})
		if err != nil {
			return nil, nil, err
		}
		var ops []resolve.Operation
		var orphaned []string
		for _, delta := range patch.Compare(repoSet, localSet) {
			switch delta.Kind {
			case patch.NeedsApply, patch.NeedsUpdate:
				ops = append(ops, operationFromPatch(*delta.Repo))
			case patch.Orphaned:
				orphaned = append(orphaned, delta.Path)
			}
		}
		return ops, orphaned, nil
	}
}

func applyMode(opts ApplyOptions) string {
	switch {
	case opts.Mode != "":
		return opts.Mode
	case opts.Reset:
		return "reset"
	case opts.ChangedRef != "":
		return "changed"
	default:
		return "incremental"
	}
}

func applyOperationRange(
	ctx context.Context,
	ws workspace.Entry,
	repoInfo *repo.Info,
	ops []resolve.Operation,
	start int,
	resolved []string,
	skipped []string,
	restorePending bool,
	autoAnnotate bool,
	result *ApplyResult,
	progress Progress,
) (int, error) {
	repoSet, err := patch.LoadRepoPatchSet(repoInfo.PatchesDir, nil)
	if err != nil {
		return start, err
	}
	for idx := start; idx < len(ops); idx++ {
		op := ops[idx]
		reportProgress(progress, "Applying %d/%d %s", idx+1, len(ops), op.ChromiumPath)
		result.ResetPaths = append(result.ResetPaths, op.ChromiumPath)
		if op.OldPath != "" {
			if err := git.ResetPathToCommit(ctx, ws.Path, repoInfo.BaseCommit, op.OldPath); err != nil {
				return idx, err
			}
		}
		if err := git.ResetPathToCommit(ctx, ws.Path, repoInfo.BaseCommit, op.ChromiumPath); err != nil {
			return idx, err
		}
		patchFile, ok := repoSet[op.ChromiumPath]
		if ok {
			if err := applySingleOperation(ctx, ws.Path, patchFile); err != nil {
				op.RejectPath = rejectPath(ws.Path, op)
				op.Message = err.Error()
				ops[idx] = op
				state := &resolve.State{
					Workspace:           ws.Path,
					RepoRoot:            repoInfo.Root,
					BaseCommit:          repoInfo.BaseCommit,
					RepoRev:             result.RepoRev,
					Mode:                result.Mode,
					Current:             idx,
					Operations:          ops,
					Resolved:            append([]string{}, resolved...),
					Skipped:             append([]string{}, skipped...),
					RestorePendingStash: restorePending,
					AutoAnnotate:        autoAnnotate,
				}
				if err := resolve.Save(ws.Path, state); err != nil {
					return idx, err
				}
				result.Conflicts = []resolve.Operation{op}
				return idx, nil
			}
		} else if op.Op == patch.OpDelete {
			if err := os.RemoveAll(filepath.Join(ws.Path, filepath.FromSlash(op.ChromiumPath))); err != nil {
				return idx, err
			}
		}
		result.Applied = append(result.Applied, op.ChromiumPath)
	}
	return len(ops), nil
}

func applySingleOperation(ctx context.Context, workspacePath string, patchFile patch.FilePatch) error {
	switch {
	case patchFile.Op == patch.OpDelete:
		return os.RemoveAll(filepath.Join(workspacePath, filepath.FromSlash(patchFile.Path)))
	case patchFile.IsPureRename():
		from := filepath.Join(workspacePath, filepath.FromSlash(patchFile.OldPath))
		to := filepath.Join(workspacePath, filepath.FromSlash(patchFile.Path))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		return os.Rename(from, to)
	case patchFile.Op == patch.OpBinary && len(patchFile.Content) == 0:
		return fmt.Errorf("binary markers are not directly applicable: %s", patchFile.Path)
	default:
		_, err := git.ApplyPatch(ctx, workspacePath, patchFile.Content)
		return err
	}
}

func verifyResolved(ctx context.Context, workspacePath string, repoInfo *repo.Info, op resolve.Operation, base string) error {
	repoSet, err := patch.LoadRepoPatchSet(repoInfo.PatchesDir, []string{op.ChromiumPath})
	if err != nil {
		return err
	}
	localSet, err := patch.BuildWorkingTreePatchSet(ctx, workspacePath, patch.WorkingTreeOptions{
		Base:    base,
		Filters: []string{op.ChromiumPath},
	})
	if err != nil {
		return err
	}
	for _, delta := range patch.Compare(repoSet, localSet) {
		if delta.Path == op.ChromiumPath && delta.Kind == patch.UpToDate {
			if op.RejectPath != "" {
				_ = os.Remove(op.RejectPath)
			}
			return nil
		}
	}
	return fmt.Errorf("current conflict is not resolved yet for %s", op.ChromiumPath)
}

func operationFromPatch(p patch.FilePatch) resolve.Operation {
	return resolve.Operation{
		ChromiumPath: p.Path,
		PatchRel:     p.Path,
		Op:           p.Op,
		OldPath:      p.OldPath,
	}
}

func operationsFromPatchSet(set patch.PatchSet) []resolve.Operation {
	paths := patch.ScopeFromSet(set)
	ops := make([]resolve.Operation, 0, len(paths))
	for _, rel := range paths {
		ops = append(ops, operationFromPatch(set[rel]))
	}
	return ops
}

func operationsFromChanges(repoSet patch.PatchSet, changes []git.FileChange, filters []string) []resolve.Operation {
	var ops []resolve.Operation
	for _, change := range changes {
		rel := normalizeChangedPatchPath(change.Path)
		if !patch.PathMatches(rel, filters) {
			continue
		}
		if patchFile, ok := repoSet[rel]; ok {
			ops = append(ops, operationFromPatch(patchFile))
			continue
		}
		ops = append(ops, resolve.Operation{
			ChromiumPath: rel,
			PatchRel:     rel,
			Op:           patch.OpDelete,
			OldPath:      normalizeChangedPatchPath(change.OldPath),
		})
	}
	return ops
}

func repoPatchChanges(ctx context.Context, repoInfo *repo.Info, ref string, rangeEnd string) ([]git.FileChange, error) {
	pathspecs := []string{"chromium_patches"}
	if rangeEnd == "" {
		return git.DiffTreeNameStatus(ctx, repoInfo.Root, ref, pathspecs)
	}
	return git.DiffNameStatusBetween(ctx, repoInfo.Root, ref, rangeEnd, pathspecs)
}

func rejectPath(workspacePath string, op resolve.Operation) string {
	candidate := patch.RejectPath(workspacePath, op.ChromiumPath)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func normalizeChangedPatchPath(path string) string {
	return strings.TrimPrefix(patch.NormalizeChromiumPath(path), "chromium_patches/")
}

func clearResolveState(workspacePath string) error {
	if resolve.Exists(workspacePath) {
		return resolve.Delete(workspacePath)
	}
	return nil
}

func markApplyComplete(workspacePath string, baseCommit string, repoRev string) error {
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		return err
	}
	state.BaseCommit = baseCommit
	state.LastApplyRev = repoRev
	state.LastApplyAt = time.Now().UTC()
	return workspace.SaveState(workspacePath, state)
}
