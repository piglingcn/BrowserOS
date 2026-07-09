package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/resolve"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

func TestAbortRevertsAppliedOpsAndRestoresPendingStash(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "a\n")
	writeFile(t, filepath.Join(workspacePath, "b.txt"), "b\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local\n")
	runGit(t, workspacePath, "add", "a.txt", "b.txt", "local.txt")
	runGit(t, workspacePath, "commit", "-m", "base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	runGit(t, workspacePath, "stash", "push", "-m", "test stash", "-u", "--", "local.txt")
	stashRef := gitOutput(t, workspacePath, "stash", "list", "-1", "--format=%gd")
	if stashRef == "" {
		t.Fatalf("expected stash ref")
	}

	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		PendingStash: stashRef,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	writeFile(t, filepath.Join(workspacePath, "a.txt"), "applied\n")
	writeFile(t, filepath.Join(workspacePath, "b.txt"), "conflict\n")
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:  workspacePath,
		RepoRoot:   workspacePath,
		BaseCommit: baseCommit,
		Current:    1,
		Operations: []resolve.Operation{
			{ChromiumPath: "a.txt", PatchRel: "a.txt", Op: patch.OpModify},
			{ChromiumPath: "b.txt", PatchRel: "b.txt", Op: patch.OpModify},
		},
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	if err := Abort(ctx, workspace.Entry{Name: "ws", Path: workspacePath}); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	assertFile(t, filepath.Join(workspacePath, "a.txt"), "a\n")
	assertFile(t, filepath.Join(workspacePath, "b.txt"), "b\n")
	assertFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	if resolve.Exists(workspacePath) {
		t.Fatalf("expected resolve state to be removed")
	}
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("expected pending stash cleared, got %q", state.PendingStash)
	}
}

func TestPublishReturnsHelpfulErrorWhenNothingChanged(t *testing.T) {
	ctx := context.Background()
	repoRoot := initGitRepo(t)
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), "base123\n")
	writeFile(t, filepath.Join(repoRoot, "chromium_patches", ".gitkeep"), "")
	runGit(t, repoRoot, "add", "BASE_COMMIT", "chromium_patches/.gitkeep")
	runGit(t, repoRoot, "commit", "-m", "repo init")

	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}
	if _, err := Publish(ctx, PublishOptions{Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "nothing to publish") {
		t.Fatalf("expected helpful no-op error, got %v", err)
	}
}

func TestOperationsFromChangesNormalizesOldPath(t *testing.T) {
	ops := operationsFromChanges(nil, []git.FileChange{{
		Status:  "R",
		Path:    "chromium_patches/chrome/new.cc",
		OldPath: "chromium_patches/chrome/old.cc",
	}}, nil)

	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0].ChromiumPath != "chrome/new.cc" {
		t.Fatalf("unexpected chromium path: %q", ops[0].ChromiumPath)
	}
	if ops[0].OldPath != "chrome/old.cc" {
		t.Fatalf("unexpected old path: %q", ops[0].OldPath)
	}
}

func TestAnnotateCommitsChangedFilesByFeature(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "core.cc"), "core\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "feature.cc"), "feature\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "nested", "view.cc"), "view\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "clean.cc"), "clean\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "core.cc"), "core changed\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "feature.cc"), "feature changed\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "nested", "view.cc"), "view changed\n")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  core:
    description: "chore: core feature"
    files:
      - chrome/browser/core.cc
  browser-feature:
    description: "feat: browser feature"
    files:
      - chrome/browser/
  clean:
    description: "chore: clean"
    files:
      - chrome/browser/clean.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 2 || result.FeaturesSkipped != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	if result.Processed != 3 {
		t.Fatalf("processed = %d, want 3", result.Processed)
	}
	if len(result.Committed) != 2 {
		t.Fatalf("expected 2 committed features, got %+v", result.Committed)
	}
	if result.Committed[0].Name != "core" || result.Committed[0].Commit == "" {
		t.Fatalf("unexpected first commit result: %+v", result.Committed[0])
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/browser/core.cc"}) {
		t.Fatalf("unexpected core files: %v", result.Committed[0].Files)
	}
	if result.Committed[1].Name != "browser-feature" {
		t.Fatalf("unexpected second commit result: %+v", result.Committed[1])
	}
	if !slices.Equal(result.Committed[1].Files, []string{"chrome/browser/feature.cc", "chrome/browser/nested/view.cc"}) {
		t.Fatalf("unexpected feature files: %v", result.Committed[1].Files)
	}
	subjects := strings.Split(gitOutput(t, workspacePath, "log", "--format=%s", "-2"), "\n")
	if !slices.Equal(subjects, []string{"feat: browser feature", "chore: core feature"}) {
		t.Fatalf("unexpected commit subjects: %v", subjects)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after annotate, got %q", status)
	}
}

func TestAnnotateProcessesOverlappingPathsInFeatureOrder(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "browseros", "core", "shared.cc"), "shared\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "browseros", "core", "browseros_prefs.cc"), "prefs\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "browseros", "core", "shared.cc"), "shared changed\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser", "browseros", "core", "browseros_prefs.cc"), "prefs changed\n")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/browser/browseros/core/
  onboarding-import:
    description: "feat: onboarding import"
    files:
      - chrome/browser/browseros/core/browseros_prefs.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 || result.FeaturesSkipped != 1 {
		t.Fatalf("expected one commit and one skip, got %+v", result)
	}
	if result.Committed[0].Name != "browseros-core" {
		t.Fatalf("expected earlier feature to own overlapping path, got %+v", result.Committed[0])
	}
	if !slices.Equal(result.Committed[0].Files, []string{
		"chrome/browser/browseros/core/browseros_prefs.cc",
		"chrome/browser/browseros/core/shared.cc",
	}) {
		t.Fatalf("unexpected core files: %v", result.Committed[0].Files)
	}
	if result.Skipped[0].Name != "onboarding-import" || result.Skipped[0].Reason != "no changes" {
		t.Fatalf("expected later feature to see no remaining change, got %+v", result.Skipped)
	}
	if files := strings.Split(gitOutput(t, workspacePath, "show", "--name-only", "--format=", "HEAD"), "\n"); !slices.Equal(files, result.Committed[0].Files) {
		t.Fatalf("core commit files = %v", files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after annotate, got %q", status)
	}
}

func TestAnnotateSkipsStaleIndexWhenWorktreeMatchesHead(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	rel := "chrome/browser/browseros/core/browseros_prefs.cc"
	writeFile(t, filepath.Join(workspacePath, rel), "base\n")
	runGit(t, workspacePath, "add", rel)
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, rel), "patched\n")
	runGit(t, workspacePath, "add", rel)
	runGit(t, workspacePath, "commit", "-m", "existing patch")
	head := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "checkout", baseCommit, "--", rel)
	writeFile(t, filepath.Join(workspacePath, rel), "patched\n")
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", rel); status != "MM "+rel {
		t.Fatalf("test setup expected split index status, got %q", status)
	}

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/browser/browseros/core/
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 0 || result.FeaturesSkipped != 1 {
		t.Fatalf("expected stale index no-op to skip cleanly, got %+v", result)
	}
	if got := gitOutput(t, workspacePath, "rev-parse", "HEAD"); got != head {
		t.Fatalf("annotate should not create a commit: got HEAD %s want %s", got, head)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected stale index to be cleaned, got %q", status)
	}
}

func TestAnnotateReportsOnlyFilesIncludedInCommit(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	staleRel := "chrome/browser/browseros/core/stale.cc"
	changedRel := "chrome/browser/browseros/core/changed.cc"
	writeFile(t, filepath.Join(workspacePath, staleRel), "base stale\n")
	writeFile(t, filepath.Join(workspacePath, changedRel), "base changed\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, staleRel), "patched stale\n")
	runGit(t, workspacePath, "add", staleRel)
	runGit(t, workspacePath, "commit", "-m", "existing stale patch")

	runGit(t, workspacePath, "checkout", baseCommit, "--", staleRel)
	writeFile(t, filepath.Join(workspacePath, staleRel), "patched stale\n")
	writeFile(t, filepath.Join(workspacePath, changedRel), "patched changed\n")
	status := gitOutput(t, workspacePath, "status", "--porcelain", "--", staleRel, changedRel)
	if !strings.Contains(status, changedRel) || !strings.Contains(status, "MM "+staleRel) {
		t.Fatalf("test setup expected mixed status, got %q", status)
	}

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/browser/browseros/core/
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected one commit, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{changedRel}) {
		t.Fatalf("reported files should match actual commit files, got %v", result.Committed[0].Files)
	}
	if files := strings.Split(gitOutput(t, workspacePath, "show", "--name-only", "--format=", "HEAD"), "\n"); !slices.Equal(files, []string{changedRel}) {
		t.Fatalf("commit files = %v", files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after annotate, got %q", status)
	}
}

func TestAnnotateCommitsRenamesUnderDirectoryFeature(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "old\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  renames:
    description: "feat: rename file"
    files:
      - chrome/
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected rename commit, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/new.cc", "chrome/old.cc"}) {
		t.Fatalf("unexpected committed rename paths: %v", result.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	if nameStatus := gitOutput(t, workspacePath, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(nameStatus, "R100\tchrome/old.cc\tchrome/new.cc") {
		t.Fatalf("expected rename in commit, got:\n%s", nameStatus)
	}
}

func TestAnnotateBroadFeatureOwnsRenameBeforeSpecificOldPath(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "old\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  broad:
    description: "chore: broad"
    files:
      - chrome/
  specific:
    description: "feat: specific rename"
    files:
      - chrome/old.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 || result.Committed[0].Name != "broad" {
		t.Fatalf("expected earlier broad feature to own rename, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/new.cc", "chrome/old.cc"}) {
		t.Fatalf("unexpected rename files: %v", result.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	if nameStatus := gitOutput(t, workspacePath, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(nameStatus, "R100\tchrome/old.cc\tchrome/new.cc") {
		t.Fatalf("expected rename in commit, got:\n%s", nameStatus)
	}
}

func TestAnnotateBroadFeatureOwnsRenameBeforeSpecificNewPath(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "old\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  broad:
    description: "chore: broad"
    files:
      - chrome/
  specific:
    description: "feat: specific rename"
    files:
      - chrome/new.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 || result.Committed[0].Name != "broad" {
		t.Fatalf("expected earlier broad feature to own rename, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/new.cc", "chrome/old.cc"}) {
		t.Fatalf("unexpected rename files: %v", result.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	if nameStatus := gitOutput(t, workspacePath, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(nameStatus, "R100\tchrome/old.cc\tchrome/new.cc") {
		t.Fatalf("expected rename in commit, got:\n%s", nameStatus)
	}
}

func TestAnnotateExactOldPathOnlyOwnsRename(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "old\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  specific:
    description: "feat: exact old rename"
    files:
      - chrome/old.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected exact old path feature to own rename, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/new.cc", "chrome/old.cc"}) {
		t.Fatalf("unexpected rename files: %v", result.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	if nameStatus := gitOutput(t, workspacePath, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(nameStatus, "R100\tchrome/old.cc\tchrome/new.cc") {
		t.Fatalf("expected rename in commit, got:\n%s", nameStatus)
	}
}

func TestAnnotateExactNewPathOnlyOwnsRename(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "old\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  specific:
    description: "feat: exact new rename"
    files:
      - chrome/new.cc
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected exact new path feature to own rename, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/new.cc", "chrome/old.cc"}) {
		t.Fatalf("unexpected rename files: %v", result.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	if nameStatus := gitOutput(t, workspacePath, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(nameStatus, "R100\tchrome/old.cc\tchrome/new.cc") {
		t.Fatalf("expected rename in commit, got:\n%s", nameStatus)
	}
}

func TestApplyReportsPatchProgress(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "patched\n")
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", "chrome/browser.cc")
	if err != nil {
		t.Fatalf("DiffText: %v", err)
	}
	runGit(t, workspacePath, "checkout", "--", "chrome/browser.cc")

	repoRoot := initGitRepo(t)
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), baseCommit+"\n")
	writeFile(t, filepath.Join(repoRoot, "chromium_patches", "chrome", "browser.cc"), diff)
	runGit(t, repoRoot, "add", "BASE_COMMIT", "chromium_patches/chrome/browser.cc")
	runGit(t, repoRoot, "commit", "-m", "patch repo init")
	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}

	progress := &progressRecorder{}
	_, err = Apply(ctx, ApplyOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Progress:  progress,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	progress.requireContains(t, "Inspecting workspace changes")
	progress.requireContains(t, "Applying 1 patch operation")
	progress.requireContains(t, "Applying 1/1 chrome/browser.cc")
	assertFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "patched\n")
}

func TestApplyAutoAnnotatesAfterCleanApply(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/core.cc", "patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/core.cc
`)

	result, err := Apply(ctx, ApplyOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected one auto-annotate commit, got %+v", result.Annotate)
	}
	if len(result.Annotate.Unclaimed) != 0 {
		t.Fatalf("expected no unclaimed changes, got %v", result.Annotate.Unclaimed)
	}
	if subject := gitOutput(t, workspacePath, "log", "-1", "--format=%s"); subject != "chore: browseros core" {
		t.Fatalf("unexpected HEAD subject %q", subject)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome"); status != "" {
		t.Fatalf("expected clean checkout, got %q", status)
	}
}

func TestApplyWithNoOpsStillAutoAnnotates(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/core.cc", "patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/core.cc
`)

	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	if _, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo}); err != nil {
		t.Fatalf("Apply without annotate: %v", err)
	}
	result, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo, AutoAnnotate: true})
	if err != nil {
		t.Fatalf("Apply with annotate: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("expected up-to-date tree to need no operations, got %v", result.Applied)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected annotate to commit leftover applied changes, got %+v", result.Annotate)
	}
}

func TestAnnotateRefusesDuringConflictResolution(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/
`)
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:  workspacePath,
		RepoRoot:   repoInfo.Root,
		BaseCommit: baseCommit,
		Operations: []resolve.Operation{{ChromiumPath: "chrome/core.cc", PatchRel: "chrome/core.cc", Op: patch.OpModify}},
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	_, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict resolution is in progress") {
		t.Fatalf("expected pending-conflict refusal, got %v", err)
	}
}

func TestAnnotateSkipsUntrackedJunk(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "patched\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc.rej"), "reject junk\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc.orig"), "orig junk\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "debug.log"), "log junk\n")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected one commit, got %+v", result)
	}
	if !slices.Equal(result.Committed[0].Files, []string{"chrome/core.cc"}) {
		t.Fatalf("junk must stay out of feature commits, got %v", result.Committed[0].Files)
	}
	if len(result.Unclaimed) != 0 {
		t.Fatalf("junk must not be reported as unclaimed, got %v", result.Unclaimed)
	}
	if files := gitOutput(t, workspacePath, "show", "--name-only", "--format=", "HEAD"); files != "chrome/core.cc" {
		t.Fatalf("commit files = %q", files)
	}
}

func TestAnnotateSkipsManagedGeneratedOutputs(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	managedGenerated := []string{
		"chrome/VERSION",
		"chrome/BROWSEROS_VERSION",
		"chrome/app/chromium_strings.grd",
		"chrome/app/settings_chromium_strings.grdp",
		"chrome/app/theme/chromium/BRANDING",
		"chrome/app/theme/chromium/chromeos/chrome_app_icon_32.png",
		"chrome/app/theme/chromium/chromium.ai",
		"chrome/app/theme/chromium/linux/product_logo_32.xpm",
		"chrome/app/theme/chromium/mac/AppIcon.icns",
		"chrome/app/theme/chromium/mac/Assets.car",
		"chrome/app/theme/chromium/mac/Assets.xcassets/AppIcon.appiconset/appicon_1024.png",
		"chrome/app/theme/chromium/mac/app.icns",
		"chrome/app/theme/chromium/mac/document.icns",
		"chrome/app/theme/chromium/product_logo.ai",
		"chrome/app/theme/chromium/product_logo.png",
		"chrome/app/theme/chromium/product_logo.svg",
		"chrome/app/theme/chromium/product_logo_1024.png",
		"chrome/app/theme/chromium/product_logo_16.png",
		"chrome/app/theme/chromium/product_logo_192.png",
		"chrome/app/theme/chromium/product_logo_22.png",
		"chrome/app/theme/chromium/product_logo_22_mono.png",
		"chrome/app/theme/chromium/product_logo_32.png",
		"chrome/app/theme/chromium/product_logo_animation.svg",
		"chrome/app/theme/chromium/product_logo_name_22.png",
		"chrome/app/theme/chromium/product_logo_name_22_2x.png",
		"chrome/app/theme/chromium/product_logo_name_22_white.png",
		"chrome/app/theme/chromium/product_logo_name_22_white_2x.png",
		"chrome/app/theme/chromium/win/tiles/Logo.png",
		"chrome/app/theme/default_100_percent/chromium/product_logo_32.png",
		"chrome/app/theme/default_200_percent/chromium/product_logo_name_22_white.png",
		"chrome/browser/browseros/claw_server/resources/bin/browseros-claw-server",
		"chrome/browser/browseros/onboarding/resources/index.html",
		"chrome/browser/browseros/server/resources/bin/browseros_server",
		"chrome/enterprise_companion/branding.gni",
		"chrome/updater/branding.gni",
	}
	allFiles := append([]string{}, managedGenerated...)
	allFiles = append(allFiles,
		"chrome/app/theme/chromium/not_managed.txt",
		"content/render.cc",
	)
	for _, rel := range allFiles {
		writeFile(t, filepath.Join(workspacePath, filepath.FromSlash(rel)), "base\n")
	}
	runGit(t, workspacePath, "add", ".")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	for _, rel := range allFiles {
		writeFile(t, filepath.Join(workspacePath, filepath.FromSlash(rel)), "changed\n")
	}

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/browser/core.cc
`)
	writeFile(t, filepath.Join(repoInfo.Root, "chromium_files", "products", "browseros", "chrome", "app", "theme", "chromium", "BRANDING.debug"), "overlay\n")
	writeFile(t, filepath.Join(repoInfo.Root, "chromium_files", "products", "browseros", "chrome", "enterprise_companion", "branding.gni"), "overlay\n")
	writeFile(t, filepath.Join(repoInfo.Root, "chromium_files", "products", "browseros", "chrome", "updater", "branding.gni"), "overlay\n")
	for _, rel := range []string{
		"resources/browseros/icons/chromium.ai",
		"resources/browseros/icons/product_logo.ai",
		"resources/browseros/icons/product_logo.png",
		"resources/browseros/icons/product_logo.svg",
		"resources/browseros/icons/product_logo_1024.png",
		"resources/browseros/icons/product_logo_16.png",
		"resources/browseros/icons/product_logo_192.png",
		"resources/browseros/icons/product_logo_22.png",
		"resources/browseros/icons/product_logo_22_mono.png",
		"resources/browseros/icons/product_logo_32.png",
		"resources/browseros/icons/product_logo_animation.svg",
		"resources/browseros/icons/product_logo_name_22.png",
		"resources/browseros/icons/product_logo_name_22_2x.png",
		"resources/browseros/icons/product_logo_name_22_white.png",
		"resources/browseros/icons/product_logo_name_22_white_2x.png",
	} {
		writeFile(t, filepath.Join(repoInfo.Root, filepath.FromSlash(rel)), "icon\n")
	}
	writeFile(t, filepath.Join(repoInfo.Root, "bos_build", "config", "copy_resources.yaml"), `copy_operations:
  - name: Version
    source: resources/BROWSEROS_VERSION
    destination: chrome/BROWSEROS_VERSION
    type: file
  - name: Product root icons
    source: resources/browseros/icons/*.png
    destination: chrome/app/theme/chromium/
    type: files
  - name: Product root AI
    source: resources/browseros/icons/*.ai
    destination: chrome/app/theme/chromium/
    type: files
  - name: Product root SVG
    source: resources/browseros/icons/*.svg
    destination: chrome/app/theme/chromium/
    type: files
  - name: ChromeOS icons
    source: resources/browseros/icons/chromeos
    destination: chrome/app/theme/chromium/chromeos
    type: directory
  - name: Linux icons
    source: resources/browseros/icons/linux
    destination: chrome/app/theme/chromium/linux
    type: directory
  - name: Mac icons
    source: resources/browseros/icons/mac
    destination: chrome/app/theme/chromium/mac
    type: directory
  - name: Windows icons
    source: resources/browseros/icons/win
    destination: chrome/app/theme/chromium/win
    type: directory
  - name: DPI 100 icons
    source: resources/browseros/icons/default_100_percent
    destination: chrome/app/theme/default_100_percent/chromium
    type: directory
  - name: DPI 200 icons
    source: resources/browseros/icons/default_200_percent
    destination: chrome/app/theme/default_200_percent/chromium
    type: directory
  - name: Server resources
    source: resources/binaries/browseros_server/darwin-arm64/resources
    destination: chrome/browser/browseros/server/resources
    type: directory
  - name: Claw server resources
    source: resources/binaries/browseros_claw_server/darwin-arm64/resources
    destination: chrome/browser/browseros/claw_server/resources
    type: directory
  - name: Claw onboarding resources
    source: resources/binaries/browseros_claw_onboard/resources
    destination: chrome/browser/browseros/onboarding/resources
    type: directory
`)
	writeFile(t, filepath.Join(repoInfo.Root, "bos_build", "steps", "resources", "string_replaces.py"), `target_files = [
    "chrome/app/chromium_strings.grd",
    "chrome/app/settings_chromium_strings.grdp",
]
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	wantUnclaimed := []string{
		"chrome/app/theme/chromium/not_managed.txt",
		"content/render.cc",
	}
	if !slices.Equal(result.Unclaimed, wantUnclaimed) {
		t.Fatalf("expected only unmanaged files unclaimed, got %v", result.Unclaimed)
	}
	if result.CommitsCreated != 0 {
		t.Fatalf("managed output filtering should not create commits, got %+v", result.Committed)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/app/theme/chromium/product_logo_16.png"); status != "M chrome/app/theme/chromium/product_logo_16.png" {
		t.Fatalf("managed output should stay modified for build pipeline, got %q", status)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/app/theme/chromium/not_managed.txt"); status != "M chrome/app/theme/chromium/not_managed.txt" {
		t.Fatalf("unmanaged file should stay modified, got %q", status)
	}
}

func TestAnnotateReportsUnclaimedChanges(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	writeFile(t, filepath.Join(workspacePath, "content", "render.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome", "content")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "patched\n")
	writeFile(t, filepath.Join(workspacePath, "content", "render.cc"), "patched\n")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/
`)

	result, err := Annotate(ctx, AnnotateOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if result.CommitsCreated != 1 {
		t.Fatalf("expected the claimed file committed, got %+v", result)
	}
	if !slices.Equal(result.Unclaimed, []string{"content/render.cc"}) {
		t.Fatalf("expected unclaimed report, got %v", result.Unclaimed)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "content"); status != "M content/render.cc" {
		t.Fatalf("unclaimed file should stay uncommitted, got %q", status)
	}
}

func TestSkipCompletesAutoAnnotateFromPausedApply(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "aaa.cc"), "a1\na2\na3\na4\na5\na6\na7\na8\na9\na10\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "bbb.cc"), "base-bbb\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	// Two hunks: the first applies at base, the second's context does not
	// exist -> the --reject fallback half-applies the file and pauses.
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "chrome", "aaa.cc"),
		"diff --git a/chrome/aaa.cc b/chrome/aaa.cc\n"+
			"index 0000000000000000000000000000000000000000..1111111111111111111111111111111111111111 100644\n"+
			"--- a/chrome/aaa.cc\n"+
			"+++ b/chrome/aaa.cc\n"+
			"@@ -1,5 +1,5 @@\n a1\n-a2\n+a2 patched\n a3\n a4\n a5\n"+
			"@@ -7,4 +7,4 @@\n a7\n-mismatched-context\n+a8 patched\n a9\n a10\n")
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/bbb.cc", "patched-bbb\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: browseros core"
    files:
      - chrome/
`)

	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	paused, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo, AutoAnnotate: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(paused.Conflicts) != 1 || paused.Conflicts[0].ChromiumPath != "chrome/aaa.cc" {
		t.Fatalf("expected pause on chrome/aaa.cc, got %+v", paused.Conflicts)
	}
	if paused.Annotate != nil {
		t.Fatalf("paused apply must not annotate a half-applied tree")
	}
	assertFile(t, filepath.Join(workspacePath, "chrome", "aaa.cc"), "a1\na2 patched\na3\na4\na5\na6\na7\na8\na9\na10\n")
	state, err := resolve.Load(workspacePath)
	if err != nil {
		t.Fatalf("resolve.Load: %v", err)
	}
	if !state.AutoAnnotate {
		t.Fatalf("expected auto-annotate intent to survive the pause")
	}

	result, err := Skip(ctx, SkipOptions{Workspace: ws})
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected skip to finish the requested annotation, got %+v", result.Annotate)
	}
	if !slices.Equal(result.Annotate.Committed[0].Files, []string{"chrome/bbb.cc"}) {
		t.Fatalf("expected only the applied file committed, got %v", result.Annotate.Committed[0].Files)
	}
	if slices.Contains(result.Annotate.Unclaimed, "chrome/aaa.cc") {
		t.Fatalf("skipped conflict must not be reported unclaimed, got %v", result.Annotate.Unclaimed)
	}
	if resolve.Exists(workspacePath) {
		t.Fatalf("expected resolve state cleared after skip completion")
	}
	if files := gitOutput(t, workspacePath, "show", "--name-only", "--format=", "HEAD"); files != "chrome/bbb.cc" {
		t.Fatalf("half-applied skip and reject junk must stay out of the feature commit, got %q", files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/aaa.cc"); status != "M chrome/aaa.cc" {
		t.Fatalf("skipped conflict should stay dirty for the user, got %q", status)
	}
}

func TestSkippedExcludePathsCoversRenameOldPath(t *testing.T) {
	state := &resolve.State{
		Skipped: []string{"chrome/new.cc", "content/plain.cc"},
		Operations: []resolve.Operation{
			{ChromiumPath: "chrome/new.cc", OldPath: "chrome/old.cc", Op: patch.OpRename},
			{ChromiumPath: "content/plain.cc", Op: patch.OpModify},
			{ChromiumPath: "chrome/applied.cc", Op: patch.OpModify},
		},
	}
	got := skippedExcludePaths(state)
	slices.Sort(got)
	want := []string{"chrome/new.cc", "chrome/old.cc", "content/plain.cc"}
	if !slices.Equal(got, want) {
		t.Fatalf("skippedExcludePaths = %v, want %v (rename old path and skipped paths, not applied ops)", got, want)
	}
}

func TestApplyRefusesDuringConflictResolution(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:  workspacePath,
		RepoRoot:   repoInfo.Root,
		BaseCommit: baseCommit,
		Operations: []resolve.Operation{{ChromiumPath: "chrome/core.cc", PatchRel: "chrome/core.cc", Op: patch.OpModify}},
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	if _, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "conflict resolution is in progress") {
		t.Fatalf("expected apply to refuse during pending resolution, got %v", err)
	}
	if _, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "conflict resolution is in progress") {
		t.Fatalf("expected sync to refuse during pending resolution, got %v", err)
	}
	if _, err := Refresh(ctx, RefreshOptions{Workspace: ws, Repo: repoInfo, Pull: false}); err == nil || !strings.Contains(err.Error(), "conflict resolution is in progress") {
		t.Fatalf("expected refresh to refuse during pending resolution, got %v", err)
	}
	if !resolve.Exists(workspacePath) {
		t.Fatalf("pending resolve state must survive the refused runs")
	}
}

func TestApplyRefusesWithUnmergedFiles(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	rel := "chrome/core.cc"
	writeFile(t, filepath.Join(workspacePath, rel), "base\n")
	runGit(t, workspacePath, "add", rel)
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	// Two branches touching the same line, merged to force an unmerged (UU)
	// entry — the state a conflicted stash pop leaves, with no resolve.json.
	runGit(t, workspacePath, "checkout", "-b", "other")
	writeFile(t, filepath.Join(workspacePath, rel), "other\n")
	runGit(t, workspacePath, "commit", "-am", "other change")
	runGit(t, workspacePath, "checkout", "-")
	writeFile(t, filepath.Join(workspacePath, rel), "mine\n")
	runGit(t, workspacePath, "commit", "-am", "my change")
	if err := exec.Command("git", "-C", workspacePath, "merge", "other").Run(); err == nil {
		t.Fatalf("expected merge to conflict")
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", rel); !strings.HasPrefix(status, "UU") {
		t.Fatalf("test setup expected unmerged status, got %q", status)
	}

	repoInfo := newPatchRepo(t, baseCommit)
	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	if _, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "unresolved merge conflicts") {
		t.Fatalf("expected apply to refuse on unmerged files, got %v", err)
	}
}

func TestAnnotateRefusesOnConflictMarkers(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	rel := "chrome/core.cc"
	writeFile(t, filepath.Join(workspacePath, rel), "base\n")
	runGit(t, workspacePath, "add", rel)
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	// The plain-modified-with-markers state a stash rebase leaves (no UU entry).
	writeFile(t, filepath.Join(workspacePath, rel), "<<<<<<< local\nmine\n=======\ntheirs\n>>>>>>> stashed\n")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: core"
    files:
      - chrome/
`)
	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	if _, err := Annotate(ctx, AnnotateOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "leftover conflict markers") {
		t.Fatalf("expected annotate to refuse on conflict markers, got %v", err)
	}
	if _, err := Apply(ctx, ApplyOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "leftover conflict markers") {
		t.Fatalf("expected apply to refuse on conflict markers, got %v", err)
	}
}

func TestApplyAnnotatesWithParkedCleanStash(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	rel := "chrome/core.cc"
	writeFile(t, filepath.Join(workspacePath, rel), "base\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local\n")
	runGit(t, workspacePath, "add", ".")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, rel, "patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  browseros-core:
    description: "chore: core"
    files:
      - chrome/
`)

	// Simulate a --no-rebase sync that parked local changes: the change is in
	// a stash, removed from the worktree, and recorded on workspace state.
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	stashRef, err := git.StashPush(ctx, workspacePath, "parked", true, []string{"local.txt"})
	if err != nil {
		t.Fatalf("StashPush: %v", err)
	}
	if err := workspace.SaveState(workspacePath, &workspace.State{Version: 1, Workspace: workspacePath, BaseCommit: baseCommit, PendingStash: stashRef}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	result, err := Apply(ctx, ApplyOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AnnotateSkipped != "" {
		t.Fatalf("a parked-clean stash must not block annotation, got skip=%q", result.AnnotateSkipped)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected feature commit for the pure-patch tree, got %+v", result.Annotate)
	}
}

func TestApplyAutoAnnotateSkipsWithoutFeaturesRegistry(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/core.cc", "patched\n")

	result, err := Apply(ctx, ApplyOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
	})
	if err != nil {
		t.Fatalf("Apply must succeed without a features registry: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("expected the patch applied, got %v", result.Applied)
	}
	if result.Annotate != nil || result.AnnotateError != "" {
		t.Fatalf("expected annotation skipped quietly, got %+v / %q", result.Annotate, result.AnnotateError)
	}
}

func TestApplyRecordsAnnotateErrorWithoutFailing(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/core.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/core.cc", "patched\n")
	writeFeaturesYAML(t, repoInfo.Root, "features: broken\n")

	result, err := Apply(ctx, ApplyOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
	})
	if err != nil {
		t.Fatalf("Apply must not fail on an annotate error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("apply outcome must survive the annotate failure, got %v", result.Applied)
	}
	if result.AnnotateError == "" || result.Annotate != nil {
		t.Fatalf("expected recorded annotate error, got %+v / %q", result.Annotate, result.AnnotateError)
	}
	assertFile(t, filepath.Join(workspacePath, "chrome", "core.cc"), "patched\n")
}

func TestInspectWorkspaceSkipsIgnoredUntrackedFiles(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, ".llm", "scratch.md"), "junk\n")
	writeFile(t, filepath.Join(workspacePath, "debug.log"), "junk\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "feature.cc"), "real\n")

	repoRoot := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "chromium_patches"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), baseCommit+"\n")
	runGit(t, repoRoot, "add", "BASE_COMMIT")
	runGit(t, repoRoot, "commit", "-m", "patch repo init")
	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}

	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if !slices.Contains(status.Orphaned, "chrome/feature.cc") {
		t.Fatalf("expected real untracked file as orphan, got %v", status.Orphaned)
	}
	for _, junk := range []string{".llm/scratch.md", "debug.log"} {
		if slices.Contains(status.Orphaned, junk) {
			t.Fatalf("expected %q to be ignored, got orphans %v", junk, status.Orphaned)
		}
	}
}

// newPatchRepo builds a minimal committed patch repo pointing at baseCommit.
func newPatchRepo(t *testing.T, baseCommit string) *repo.Info {
	t.Helper()
	repoRoot := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "chromium_patches"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), baseCommit+"\n")
	runGit(t, repoRoot, "add", "BASE_COMMIT")
	runGit(t, repoRoot, "commit", "-m", "patch repo init")
	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}
	return repoInfo
}

func TestExtractRoundTripIsChurnFree(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\nline\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "patched\nline\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "feature.cc"), "new feature\n")

	repoInfo := newPatchRepo(t, baseCommit)
	ws := workspace.Entry{Name: "ws", Path: workspacePath}

	first, err := Extract(ctx, ExtractOptions{Workspace: ws, Repo: repoInfo})
	if err != nil {
		t.Fatalf("first Extract: %v", err)
	}
	if len(first.Written) != 2 {
		t.Fatalf("expected 2 files written, got %v", first.Written)
	}

	// After extract, status must agree the workspace is fully captured.
	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{Workspace: ws, Repo: repoInfo})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if len(status.NeedsUpdate) != 0 || len(status.NeedsApply) != 0 || len(status.Orphaned) != 0 {
		t.Fatalf("expected clean status after extract, got needs_update=%v needs_apply=%v orphaned=%v",
			status.NeedsUpdate, status.NeedsApply, status.Orphaned)
	}

	beforeBytes := map[string]string{}
	for _, rel := range first.Written {
		data, err := os.ReadFile(filepath.Join(repoInfo.PatchesDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read patch %s: %v", rel, err)
		}
		beforeBytes[rel] = string(data)
	}

	second, err := Extract(ctx, ExtractOptions{Workspace: ws, Repo: repoInfo})
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}
	if len(second.Written) != 0 || len(second.Deleted) != 0 {
		t.Fatalf("second extract must be a no-op, wrote %v deleted %v", second.Written, second.Deleted)
	}
	if len(second.Unchanged) != 2 {
		t.Fatalf("expected both files unchanged, got %v", second.Unchanged)
	}
	for rel, before := range beforeBytes {
		data, err := os.ReadFile(filepath.Join(repoInfo.PatchesDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read patch %s: %v", rel, err)
		}
		if string(data) != before {
			t.Fatalf("patch %s churned between identical extracts", rel)
		}
	}
}

func TestExtractFromTwoCheckoutsIsByteIdentical(t *testing.T) {
	ctx := context.Background()
	checkout1 := initGitRepo(t)
	writeFile(t, filepath.Join(checkout1, "chrome", "browser.cc"), "base\nline\n")
	runGit(t, checkout1, "add", "chrome/browser.cc")
	runGit(t, checkout1, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, checkout1, "rev-parse", "HEAD")

	checkout2Parent := t.TempDir()
	runGit(t, checkout2Parent, "clone", checkout1, "clone")
	checkout2 := filepath.Join(checkout2Parent, "clone")
	// Hostile per-checkout config must not leak into extracted patches.
	runGit(t, checkout2, "config", "core.abbrev", "9")
	runGit(t, checkout2, "config", "diff.algorithm", "histogram")
	runGit(t, checkout2, "config", "diff.mnemonicPrefix", "true")

	edit := "patched\nline\n"
	addition := "new feature\n"
	for _, checkout := range []string{checkout1, checkout2} {
		writeFile(t, filepath.Join(checkout, "chrome", "browser.cc"), edit)
		writeFile(t, filepath.Join(checkout, "chrome", "feature.cc"), addition)
	}

	repo1 := newPatchRepo(t, baseCommit)
	repo2 := newPatchRepo(t, baseCommit)
	if _, err := Extract(ctx, ExtractOptions{Workspace: workspace.Entry{Name: "c1", Path: checkout1}, Repo: repo1}); err != nil {
		t.Fatalf("extract checkout1: %v", err)
	}
	if _, err := Extract(ctx, ExtractOptions{Workspace: workspace.Entry{Name: "c2", Path: checkout2}, Repo: repo2}); err != nil {
		t.Fatalf("extract checkout2: %v", err)
	}

	for _, rel := range []string{"chrome/browser.cc", "chrome/feature.cc"} {
		data1, err := os.ReadFile(filepath.Join(repo1.PatchesDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read repo1 %s: %v", rel, err)
		}
		data2, err := os.ReadFile(filepath.Join(repo2.PatchesDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read repo2 %s: %v", rel, err)
		}
		if string(data1) != string(data2) {
			t.Fatalf("patch %s differs across checkouts\n--- c1 ---\n%s\n--- c2 ---\n%s", rel, data1, data2)
		}
	}
}

func TestExtractReportsUntrackedScanProgress(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "one.cc"), "one\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "two.cc"), "two\n")

	repoInfo := newPatchRepo(t, baseCommit)
	progress := &progressRecorder{}
	if _, err := Extract(ctx, ExtractOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Progress:  progress,
	}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	progress.requireContains(t, "Scanning untracked 1/2")
	progress.requireContains(t, "Scanning untracked 2/2")
	progress.requireContains(t, "Writing 2 patch files")
}

func TestExtractDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "patched\n")

	repoInfo := newPatchRepo(t, baseCommit)
	ws := workspace.Entry{Name: "ws", Path: workspacePath}

	result, err := Extract(ctx, ExtractOptions{Workspace: ws, Repo: repoInfo, DryRun: true})
	if err != nil {
		t.Fatalf("Extract dry-run: %v", err)
	}
	if !result.DryRun {
		t.Fatalf("expected dry_run result flag")
	}
	if len(result.Created) != 1 || result.Created[0] != "chrome/browser.cc" {
		t.Fatalf("expected planned create, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(repoInfo.PatchesDir, "chrome", "browser.cc")); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write patch files")
	}
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.LastExtractRev != "" {
		t.Fatalf("dry-run must not record extract state, got %q", state.LastExtractRev)
	}
}

func TestExtractExcludesFilterUntracked(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "scratch", "notes.md"), "junk\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "feature.cc"), "real\n")

	repoInfo := newPatchRepo(t, baseCommit)
	result, err := Extract(ctx, ExtractOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Excludes:  []string{"scratch/"},
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if slices.Contains(result.Written, "scratch/notes.md") {
		t.Fatalf("excluded path extracted anyway: %v", result.Written)
	}
	if !slices.Contains(result.Written, "chrome/feature.cc") {
		t.Fatalf("expected real file extracted, got %v", result.Written)
	}
}

// syncFixture builds a workspace with one patched file and one local-only
// change, plus a patch repo (with bare remote) whose patch rewrites a.txt.
func syncFixture(t *testing.T, patchedLine string, localLine string) (workspace.Entry, *repo.Info) {
	t.Helper()
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local base\n")
	runGit(t, workspacePath, "add", "a.txt", "local.txt")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	// Build the repo patch for a.txt from a temporary edit.
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "line1\n"+patchedLine+"\nline3\n")
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", "a.txt")
	if err != nil {
		t.Fatalf("DiffText: %v", err)
	}
	runGit(t, workspacePath, "checkout", "--", "a.txt")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "a.txt"), diff)
	runGit(t, repoInfo.Root, "add", "chromium_patches/a.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "add a.txt patch")
	remoteRepo := t.TempDir()
	runGit(t, remoteRepo, "init", "--bare")
	runGit(t, repoInfo.Root, "remote", "add", "origin", remoteRepo)
	runGit(t, repoInfo.Root, "push", "-u", "origin", "HEAD")

	// Local divergence in the workspace.
	writeFile(t, filepath.Join(workspacePath, "local.txt"), localLine+"\n")
	return workspace.Entry{Name: "ws", Path: workspacePath}, repoInfo
}

func TestSyncRebaseRestoresLocalChanges(t *testing.T) {
	ctx := context.Background()
	ws, repoInfo := syncFixture(t, "PATCHED", "local change")

	result, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", result.Conflicts)
	}
	if !result.StashRestored {
		t.Fatalf("expected stash restored, got %+v", result)
	}
	assertFile(t, filepath.Join(ws.Path, "a.txt"), "line1\nPATCHED\nline3\n")
	assertFile(t, filepath.Join(ws.Path, "local.txt"), "local change\n")
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("expected no pending stash, got %q", state.PendingStash)
	}
}

func TestSyncNoRebaseKeepsStashRecorded(t *testing.T) {
	ctx := context.Background()
	ws, repoInfo := syncFixture(t, "PATCHED", "local change")

	result, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: false})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.StashRef == "" {
		t.Fatalf("expected stash to be created")
	}
	if result.StashRestored {
		t.Fatalf("no-rebase must not pop the stash")
	}
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != result.StashRef {
		t.Fatalf("pending stash = %q, want %q (must stay recorded)", state.PendingStash, result.StashRef)
	}
	if stashList := gitOutput(t, ws.Path, "stash", "list"); stashList == "" {
		t.Fatalf("stash entry should still exist")
	}
}

func TestAbortRestoresSyncParkedStashBySHA(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "a\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local\n")
	runGit(t, workspacePath, "add", "a.txt", "local.txt")
	runGit(t, workspacePath, "commit", "-m", "base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	// Park a local change exactly the way sync does: recorded by commit SHA.
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	sha, err := git.StashPush(ctx, workspacePath, "sync stash", true, []string{"local.txt"})
	if err != nil {
		t.Fatalf("StashPush: %v", err)
	}
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		PendingStash: sha,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	writeFile(t, filepath.Join(workspacePath, "a.txt"), "applied\n")
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:  workspacePath,
		RepoRoot:   workspacePath,
		BaseCommit: baseCommit,
		Current:    0,
		Operations: []resolve.Operation{
			{ChromiumPath: "a.txt", PatchRel: "a.txt", Op: patch.OpModify},
		},
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	if err := Abort(ctx, workspace.Entry{Name: "ws", Path: workspacePath}); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	assertFile(t, filepath.Join(workspacePath, "a.txt"), "a\n")
	assertFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("expected pending stash cleared, got %q", state.PendingStash)
	}
}

func TestSyncRefusesToDoubleParkLocalChanges(t *testing.T) {
	ctx := context.Background()
	ws, repoInfo := syncFixture(t, "PATCHED", "local change")

	first, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: false})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if first.StashRef == "" {
		t.Fatalf("expected a parked stash")
	}

	// New divergence while changes are still parked.
	writeFile(t, filepath.Join(ws.Path, "local.txt"), "second change\n")
	_, err = Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: false})
	if err == nil || !strings.Contains(err.Error(), "already parked") {
		t.Fatalf("expected double-park refusal, got %v", err)
	}
	// The original stash record must be intact.
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != first.StashRef {
		t.Fatalf("pending stash = %q, want %q", state.PendingStash, first.StashRef)
	}
}

func TestSyncRestoresPreviouslyParkedStash(t *testing.T) {
	ctx := context.Background()
	ws, repoInfo := syncFixture(t, "PATCHED", "local change")

	// First sync parks the local change (--no-rebase).
	first, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: false})
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if first.StashRef == "" {
		t.Fatalf("expected a parked stash")
	}
	assertFile(t, filepath.Join(ws.Path, "local.txt"), "local base\n")

	// Second sync (rebase default) must bring the parked change back.
	second, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: true})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(second.Conflicts) != 0 || second.StashConflict {
		t.Fatalf("unexpected conflicts: %+v", second)
	}
	assertFile(t, filepath.Join(ws.Path, "local.txt"), "local change\n")
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("pending stash should be cleared after restore, got %q", state.PendingStash)
	}
	if stashList := gitOutput(t, ws.Path, "stash", "list"); stashList != "" {
		t.Fatalf("stash should be dropped after restore, got %q", stashList)
	}
}

func TestSyncReportsStashPopConflict(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "line1\nline2\nline3\n")
	runGit(t, workspacePath, "add", "a.txt")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "a.txt"), "line1\nPATCHED\nline3\n")
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", "a.txt")
	if err != nil {
		t.Fatalf("DiffText: %v", err)
	}
	runGit(t, workspacePath, "checkout", "--", "a.txt")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "a.txt"), diff)
	runGit(t, repoInfo.Root, "add", "chromium_patches/a.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "add a.txt patch")
	remoteRepo := t.TempDir()
	runGit(t, remoteRepo, "init", "--bare")
	runGit(t, repoInfo.Root, "remote", "add", "origin", remoteRepo)
	runGit(t, repoInfo.Root, "push", "-u", "origin", "HEAD")

	// Local edit to the same line the patch rewrites -> stash pop conflict.
	writeFile(t, filepath.Join(workspacePath, "a.txt"), "line1\nLOCAL\nline3\n")

	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	result, err := Sync(ctx, SyncOptions{Workspace: ws, Repo: repoInfo, Rebase: true})
	if err != nil {
		t.Fatalf("Sync should report stash conflicts, not fail: %v", err)
	}
	if !result.StashConflict {
		t.Fatalf("expected stash conflict, got %+v", result)
	}
	if !slices.Contains(result.StashConflictFiles, "a.txt") {
		t.Fatalf("expected a.txt in conflict files, got %v", result.StashConflictFiles)
	}
	state, err := workspace.LoadState(ws.Path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash == "" {
		t.Fatalf("pending stash must stay recorded after pop conflict")
	}
	if stashList := gitOutput(t, ws.Path, "stash", "list"); stashList == "" {
		t.Fatalf("stash entry must survive a pop conflict")
	}
	merged, err := os.ReadFile(filepath.Join(ws.Path, "a.txt"))
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	for _, marker := range []string{"<<<<<<<", "PATCHED", "LOCAL", ">>>>>>>"} {
		if !strings.Contains(string(merged), marker) {
			t.Fatalf("expected 3-way conflict markers with both sides, got:\n%s", merged)
		}
	}
}

func TestContinueRestoresPendingStashAfterLastConflict(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "b.txt"), "base\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local base\n")
	runGit(t, workspacePath, "add", "b.txt", "local.txt")
	runGit(t, workspacePath, "commit", "-m", "base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "b.txt"), "patched\n")
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", "b.txt")
	if err != nil {
		t.Fatalf("DiffText: %v", err)
	}

	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "b.txt"), diff)
	runGit(t, repoInfo.Root, "add", "chromium_patches/b.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "add b.txt patch")

	// Park a local change in a stash, recorded as pending (as sync does).
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	runGit(t, workspacePath, "stash", "push", "-m", "sync stash", "-u", "--", "local.txt")
	stashRef := gitOutput(t, workspacePath, "stash", "list", "-1", "--format=%gd")
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		PendingStash: stashRef,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// The conflicted operation is already resolved in the working tree; the
	// pause came from a rebase-mode sync, so restore intent is recorded.
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:           workspacePath,
		RepoRoot:            repoInfo.Root,
		BaseCommit:          baseCommit,
		Current:             0,
		Operations:          []resolve.Operation{{ChromiumPath: "b.txt", PatchRel: "b.txt", Op: patch.OpModify}},
		RestorePendingStash: true,
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	result, err := Continue(ctx, ContinueOptions{Workspace: workspace.Entry{Name: "ws", Path: workspacePath}})
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if !result.StashRestored {
		t.Fatalf("expected pending stash restored on completion, got %+v", result)
	}
	assertFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("pending stash should be cleared, got %q", state.PendingStash)
	}
}

func TestContinueLeavesExplicitlyParkedStashAlone(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "b.txt"), "base\n")
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local base\n")
	runGit(t, workspacePath, "add", "b.txt", "local.txt")
	runGit(t, workspacePath, "commit", "-m", "base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "b.txt"), "patched\n")
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", "b.txt")
	if err != nil {
		t.Fatalf("DiffText: %v", err)
	}
	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "b.txt"), diff)
	runGit(t, repoInfo.Root, "add", "chromium_patches/b.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "add b.txt patch")

	// Stash parked by an explicit --no-rebase sync: no restore intent.
	writeFile(t, filepath.Join(workspacePath, "local.txt"), "local changed\n")
	sha, err := git.StashPush(ctx, workspacePath, "sync stash", true, []string{"local.txt"})
	if err != nil {
		t.Fatalf("StashPush: %v", err)
	}
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		PendingStash: sha,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := resolve.Save(workspacePath, &resolve.State{
		Workspace:  workspacePath,
		RepoRoot:   repoInfo.Root,
		BaseCommit: baseCommit,
		Current:    0,
		Operations: []resolve.Operation{{ChromiumPath: "b.txt", PatchRel: "b.txt", Op: patch.OpModify}},
	}); err != nil {
		t.Fatalf("resolve.Save: %v", err)
	}

	result, err := Continue(ctx, ContinueOptions{Workspace: workspace.Entry{Name: "ws", Path: workspacePath}})
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if result.StashRestored {
		t.Fatalf("--no-rebase parked stash must stay parked, got %+v", result)
	}
	assertFile(t, filepath.Join(workspacePath, "local.txt"), "local base\n")
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != sha {
		t.Fatalf("pending stash record must survive, got %q want %q", state.PendingStash, sha)
	}
}

func TestInspectWorkspaceReportsPendingStash(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	repoInfo := newPatchRepo(t, baseCommit)

	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		PendingStash: "stash@{0}",
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if status.PendingStash != "stash@{0}" {
		t.Fatalf("pending stash = %q, want stash@{0}", status.PendingStash)
	}
}

func TestInspectWorkspaceDoesNotReportFreshFromStateAlone(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	repoInfo := newPatchRepo(t, baseCommit)
	repoHead := gitOutput(t, repoInfo.Root, "rev-parse", "HEAD")
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:        1,
		Workspace:      workspacePath,
		BaseCommit:     baseCommit,
		LastRefreshRev: repoHead,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if status.PatchesFreshness == "fresh" {
		t.Fatalf("state alone must not report fresh: %+v", status)
	}
	if status.PatchesRev != "" {
		t.Fatalf("patches_rev should be empty without browseros trailer, got %q", status.PatchesRev)
	}
}

func TestInspectWorkspaceReportsMismatchForDivergentMaterializedRev(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	repoInfo := newPatchRepo(t, baseCommit)
	mainBranch := gitOutput(t, repoInfo.Root, "branch", "--show-current")

	runGit(t, repoInfo.Root, "checkout", "-b", "side")
	writeFile(t, filepath.Join(repoInfo.Root, "side.txt"), "side\n")
	runGit(t, repoInfo.Root, "add", "side.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "side")
	sideRev := gitOutput(t, repoInfo.Root, "rev-parse", "HEAD")
	runGit(t, repoInfo.Root, "checkout", mainBranch)
	writeFile(t, filepath.Join(repoInfo.Root, "main.txt"), "main\n")
	runGit(t, repoInfo.Root, "add", "main.txt")
	runGit(t, repoInfo.Root, "commit", "-m", "main")

	runGit(t, workspacePath, "checkout", "-b", "browseros")
	runGit(t, workspacePath, "commit", "--allow-empty", "-m", "materialized", "-m", "Patches-Rev: "+sideRev)
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:        1,
		Workspace:      workspacePath,
		BaseCommit:     baseCommit,
		LastRefreshRev: sideRev,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if status.PatchesFreshness != "mismatch" {
		t.Fatalf("expected mismatch for divergent materialized rev, got %+v", status)
	}
}

func TestRefreshRebuildsIdenticalBrowserOSBranchesAndFastNoops(t *testing.T) {
	ctx := context.Background()
	checkout1 := initGitRepo(t)
	writeFile(t, filepath.Join(checkout1, "chrome", "a.cc"), "a base\n")
	writeFile(t, filepath.Join(checkout1, "chrome", "b.cc"), "b base\n")
	writeFile(t, filepath.Join(checkout1, "chrome", "c.cc"), "c base\n")
	runGit(t, checkout1, "add", "chrome")
	runGit(t, checkout1, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, checkout1, "rev-parse", "HEAD")

	cloneParent := t.TempDir()
	runGit(t, cloneParent, "clone", checkout1, "clone")
	checkout2 := filepath.Join(cloneParent, "clone")
	runGit(t, checkout2, "config", "user.name", "Test User")
	runGit(t, checkout2, "config", "user.email", "test@example.com")
	runGit(t, checkout1, "config", "commit.gpgsign", "true")
	runGit(t, checkout2, "config", "commit.gpgsign", "true")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, checkout1, repoInfo, baseCommit, "chrome/a.cc", "a patched\n")
	writePatchFromEdit(t, ctx, checkout1, repoInfo, baseCommit, "chrome/b.cc", "b patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  first:
    description: "feat: first"
    files:
      - chrome/a.cc
  second:
    description: "feat: second"
    files:
      - chrome/b.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")
	repoHead := gitOutput(t, repoInfo.Root, "rev-parse", "HEAD")

	first, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "c1", Path: checkout1},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("refresh checkout1: %v", err)
	}
	second, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "c2", Path: checkout2},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("refresh checkout2: %v", err)
	}
	if first.Result != "refreshed" || second.Result != "refreshed" {
		t.Fatalf("expected refreshed results, got %+v %+v", first, second)
	}
	if len(first.Commits) != 2 || len(second.Commits) != 2 {
		t.Fatalf("expected two commits per checkout, got %+v %+v", first.Commits, second.Commits)
	}
	if got := gitOutput(t, checkout1, "branch", "--show-current"); got != "browseros" {
		t.Fatalf("checkout1 branch = %q, want browseros", got)
	}
	log1 := gitOutput(t, checkout1, "log", "--format=%s%n%B", "browseros", "--not", baseCommit)
	log2 := gitOutput(t, checkout2, "log", "--format=%s%n%B", "browseros", "--not", baseCommit)
	if log1 != log2 {
		t.Fatalf("materialized logs differ\n--- checkout1 ---\n%s\n--- checkout2 ---\n%s", log1, log2)
	}
	tip1 := gitOutput(t, checkout1, "rev-parse", "browseros")
	tip2 := gitOutput(t, checkout2, "rev-parse", "browseros")
	if tip1 != tip2 {
		t.Fatalf("browseros tips differ: %s != %s", tip1, tip2)
	}
	trailer := gitOutput(t, checkout1, "log", "-1", "--format=%B", "browseros")
	if !strings.Contains(trailer, "Patches-Rev: "+repoHead) {
		t.Fatalf("browseros tip missing patches trailer for %s:\n%s", repoHead, trailer)
	}
	state, err := workspace.LoadState(checkout1)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.LastRefreshRev != repoHead || state.BaseCommit != baseCommit {
		t.Fatalf("unexpected refresh state: %+v", state)
	}
	headBefore := gitOutput(t, checkout1, "rev-parse", "HEAD")
	fresh, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "c1", Path: checkout1},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("fresh refresh: %v", err)
	}
	if fresh.Result != "fresh" || len(fresh.Commits) != 0 {
		t.Fatalf("expected fast fresh no-op, got %+v", fresh)
	}
	if headAfter := gitOutput(t, checkout1, "rev-parse", "HEAD"); headAfter != headBefore {
		t.Fatalf("fresh refresh moved HEAD: before %s after %s", headBefore, headAfter)
	}

	writePatchFromEdit(t, ctx, checkout1, repoInfo, baseCommit, "chrome/c.cc", "c patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  first:
    description: "feat: first"
    files:
      - chrome/a.cc
  second:
    description: "feat: second"
    files:
      - chrome/b.cc
  third:
    description: "feat: third"
    files:
      - chrome/c.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "add third patch")

	status, err := InspectWorkspace(ctx, InspectWorkspaceOptions{
		Workspace: workspace.Entry{Name: "c1", Path: checkout1},
		Repo:      repoInfo,
	})
	if err != nil {
		t.Fatalf("InspectWorkspace: %v", err)
	}
	if status.PatchesFreshness != "behind 1" || status.PatchesBehind != 1 {
		t.Fatalf("expected behind 1 freshness, got %+v", status)
	}
}

func TestRefreshPreconditionsAndForce(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "a base\n")
	runGit(t, workspacePath, "add", "chrome/a.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/a.cc", "a patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  first:
    description: "feat: first"
    files:
      - chrome/a.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")
	runGit(t, workspacePath, "checkout", "-b", "task/demo")
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "local dirty\n")
	writeFile(t, filepath.Join(workspacePath, "scratch.txt"), "keep me\n")

	_, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err == nil || !strings.Contains(err.Error(), "task branches are leased") {
		t.Fatalf("expected task branch refusal, got %v", err)
	}

	result, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Force:     true,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("force refresh: %v", err)
	}
	if result.Result != "refreshed" {
		t.Fatalf("expected refreshed, got %+v", result)
	}
	assertFile(t, filepath.Join(workspacePath, "scratch.txt"), "keep me\n")
	if got := gitOutput(t, workspacePath, "branch", "--show-current"); got != "browseros" {
		t.Fatalf("branch = %q, want browseros", got)
	}
}

func TestRefreshAutoAnnotatesPreservedLocalTrackedChanges(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "a base\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "local.cc"), "local base\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/a.cc", "a patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  canonical:
    description: "feat: canonical"
    files:
      - chrome/a.cc
  local:
    description: "feat: local"
    files:
      - chrome/local.cc
  untracked:
    description: "feat: untracked"
    files:
      - chrome/untracked.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")
	repoHead := gitOutput(t, repoInfo.Root, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(workspacePath, "chrome", "local.cc"), "local changed\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "untracked.cc"), "do not sweep\n")
	result, err := Refresh(ctx, RefreshOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected one auto-annotate commit, got %+v", result.Annotate)
	}
	if result.Annotate.Committed[0].Name != "local" {
		t.Fatalf("expected local feature commit, got %+v", result.Annotate.Committed)
	}
	if !slices.Equal(result.Annotate.Committed[0].Files, []string{"chrome/local.cc"}) {
		t.Fatalf("expected local file committed, got %v", result.Annotate.Committed[0].Files)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/local.cc"); status != "" {
		t.Fatalf("expected tracked local file clean after annotate, got %q", status)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/untracked.cc"); status != "?? chrome/untracked.cc" {
		t.Fatalf("untracked file should stay out of refresh annotation, got %q", status)
	}
	if subject := gitOutput(t, workspacePath, "log", "-1", "--format=%s"); subject != "feat: local" {
		t.Fatalf("unexpected HEAD subject %q", subject)
	}
	if body := gitOutput(t, workspacePath, "log", "-1", "--format=%B"); !strings.Contains(body, "Patches-Rev: "+repoHead) {
		t.Fatalf("auto-annotate commit should preserve refresh trailer:\n%s", body)
	}

	fresh, err := Refresh(ctx, RefreshOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if fresh.Result != "fresh" || fresh.Annotate != nil {
		t.Fatalf("expected clean second refresh to stay a plain fresh no-op, got %+v", fresh)
	}
}

func TestRefreshDoesNotAnnotateUntrackedOnlyChanges(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "a base\n")
	runGit(t, workspacePath, "add", "chrome/a.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/a.cc", "a patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  canonical:
    description: "feat: canonical"
    files:
      - chrome/a.cc
  untracked:
    description: "feat: untracked"
    files:
      - chrome/untracked.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")

	writeFile(t, filepath.Join(workspacePath, "chrome", "untracked.cc"), "local untracked\n")
	result, err := Refresh(ctx, RefreshOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Annotate != nil {
		t.Fatalf("untracked-only refresh must not auto-annotate, got %+v", result.Annotate)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/untracked.cc"); status != "?? chrome/untracked.cc" {
		t.Fatalf("untracked file should stay uncommitted, got %q", status)
	}
	if subject := gitOutput(t, workspacePath, "log", "-1", "--format=%s"); subject != "feat: canonical" {
		t.Fatalf("unexpected HEAD subject %q", subject)
	}
}

func TestRefreshAutoAnnotatesLocalRename(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "old.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/old.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  rename:
    description: "feat: rename local"
    files:
      - chrome/old.cc
      - chrome/new.cc
`)
	runGit(t, repoInfo.Root, "add", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "feature registry")

	runGit(t, workspacePath, "mv", "chrome/old.cc", "chrome/new.cc")
	result, err := Refresh(ctx, RefreshOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected rename auto-annotate commit, got %+v", result.Annotate)
	}
	if _, err := os.Stat(filepath.Join(workspacePath, "chrome", "old.cc")); !os.IsNotExist(err) {
		t.Fatalf("old path should stay deleted after refresh rename annotation, stat err=%v", err)
	}
	assertFile(t, filepath.Join(workspacePath, "chrome", "new.cc"), "base\n")
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome"); status != "" {
		t.Fatalf("expected clean checkout after rename annotate, got %q", status)
	}
	oldInParent, err := git.FileExistsAtCommit(ctx, workspacePath, "HEAD^", "chrome/old.cc")
	if err != nil {
		t.Fatalf("FileExistsAtCommit parent old: %v", err)
	}
	oldInHead, err := git.FileExistsAtCommit(ctx, workspacePath, "HEAD", "chrome/old.cc")
	if err != nil {
		t.Fatalf("FileExistsAtCommit head old: %v", err)
	}
	newInHead, err := git.FileExistsAtCommit(ctx, workspacePath, "HEAD", "chrome/new.cc")
	if err != nil {
		t.Fatalf("FileExistsAtCommit head new: %v", err)
	}
	if !oldInParent || oldInHead || !newInHead {
		t.Fatalf("rename tree state parentOld=%v headOld=%v headNew=%v", oldInParent, oldInHead, newInHead)
	}
}

func TestRefreshAutoAnnotatesModeOnlyChange(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "tool.sh"), "#!/bin/sh\n")
	runGit(t, workspacePath, "add", "chrome/tool.sh")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  tool:
    description: "feat: executable tool"
    files:
      - chrome/tool.sh
`)
	runGit(t, repoInfo.Root, "add", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "feature registry")

	if err := os.Chmod(filepath.Join(workspacePath, "chrome", "tool.sh"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	result, err := Refresh(ctx, RefreshOptions{
		Workspace:    workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Annotate == nil || result.Annotate.CommitsCreated != 1 {
		t.Fatalf("expected mode auto-annotate commit, got %+v", result.Annotate)
	}
	mode, err := git.FileModeAtCommit(ctx, workspacePath, "HEAD", "chrome/tool.sh")
	if err != nil {
		t.Fatalf("FileModeAtCommit: %v", err)
	}
	if mode != "100755" {
		t.Fatalf("HEAD tool mode = %q, want 100755", mode)
	}
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/tool.sh"); status != "" {
		t.Fatalf("expected clean checkout after mode annotate, got %q", status)
	}
}

func TestRefreshNoAnnotateRestoresLocalChangesDirty(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "a base\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "local.cc"), "local base\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writePatchFromEdit(t, ctx, workspacePath, repoInfo, baseCommit, "chrome/a.cc", "a patched\n")
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  canonical:
    description: "feat: canonical"
    files:
      - chrome/a.cc
  local:
    description: "feat: local"
    files:
      - chrome/local.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")

	writeFile(t, filepath.Join(workspacePath, "chrome", "local.cc"), "local changed\n")
	result, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Annotate != nil || result.AnnotateError != "" {
		t.Fatalf("expected annotation disabled, got %+v / %q", result.Annotate, result.AnnotateError)
	}
	assertFile(t, filepath.Join(workspacePath, "chrome", "local.cc"), "local changed\n")
	if status := gitOutput(t, workspacePath, "status", "--porcelain", "--", "chrome/local.cc"); status != "M chrome/local.cc" {
		t.Fatalf("expected restored local file to stay dirty, got %q", status)
	}
	if subject := gitOutput(t, workspacePath, "log", "-1", "--format=%s"); subject != "feat: canonical" {
		t.Fatalf("unexpected HEAD subject %q", subject)
	}
}

func TestRefreshStashConflictBlocksFollowupCommands(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	rel := "chrome/local.cc"
	writeFile(t, filepath.Join(workspacePath, rel), "base\n")
	runGit(t, workspacePath, "add", rel)
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	if err := os.Remove(filepath.Join(workspacePath, rel)); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", rel)
	if err != nil {
		t.Fatalf("DiffText delete: %v", err)
	}
	writeFile(t, filepath.Join(repoInfo.PatchesDir, filepath.FromSlash(rel)), diff)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  deleted:
    description: "feat: delete local"
    files:
      - chrome/local.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "delete patch")
	runGit(t, workspacePath, "checkout", baseCommit, "--", rel)

	writeFile(t, filepath.Join(workspacePath, rel), "local edit\n")
	ws := workspace.Entry{Name: "ws", Path: workspacePath}
	result, err := Refresh(ctx, RefreshOptions{
		Workspace:    ws,
		Repo:         repoInfo,
		AutoAnnotate: true,
		Pull:         false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !result.StashConflict || result.AnnotateSkipped == "" {
		t.Fatalf("expected refresh stash conflict and annotation skip, got %+v", result)
	}
	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !state.PendingStashConflict || state.PendingStash == "" {
		t.Fatalf("expected pending stash conflict recorded, got %+v", state)
	}
	if _, err := Annotate(ctx, AnnotateOptions{Workspace: ws, Repo: repoInfo}); err == nil || !strings.Contains(err.Error(), "unresolved stashed local changes") {
		t.Fatalf("expected annotate to refuse pending stash conflict, got %v", err)
	}

	runGit(t, workspacePath, "stash", "drop")
	if _, err := Annotate(ctx, AnnotateOptions{Workspace: ws, Repo: repoInfo}); err != nil {
		t.Fatalf("expected stale pending stash conflict record to clear after stash drop, got %v", err)
	}
}

func TestRefreshRefusesUntrackedPatchTargetCollision(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "base.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/base.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(workspacePath, "chrome", "new.cc"), "untracked local\n")
	diff, err := git.DiffNoIndex(ctx, workspacePath, "chrome/new.cc")
	if err != nil {
		t.Fatalf("DiffNoIndex: %v", err)
	}
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "chrome", "new.cc"), diff)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  new-file:
    description: "feat: new file"
    files:
      - chrome/new.cc
`)
	runGit(t, repoInfo.Root, "add", "chromium_patches", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "patch stack")

	_, err = Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Force:     true,
		Pull:      false,
	})
	if err == nil || !strings.Contains(err.Error(), "untracked files collide") {
		t.Fatalf("expected untracked collision error, got %v", err)
	}
	assertFile(t, filepath.Join(workspacePath, "chrome", "new.cc"), "untracked local\n")
}

func TestRefreshEmptyPatchStackCreatesFreshMaterializationCommit(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "base.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/base.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  empty:
    description: "chore: empty"
    files: []
`)
	runGit(t, repoInfo.Root, "add", "bos_build/features.yaml")
	runGit(t, repoInfo.Root, "commit", "-m", "empty feature registry")
	repoHead := gitOutput(t, repoInfo.Root, "rev-parse", "HEAD")

	result, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(result.Commits) != 1 || result.Commits[0].Feature != "materialization" {
		t.Fatalf("expected materialization commit, got %+v", result.Commits)
	}
	trailer := gitOutput(t, workspacePath, "log", "-1", "--format=%B", "browseros")
	if !strings.Contains(trailer, "Patches-Rev: "+repoHead) {
		t.Fatalf("materialization commit missing trailer:\n%s", trailer)
	}
	fresh, err := Refresh(ctx, RefreshOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Pull:      false,
	})
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if fresh.Result != "fresh" {
		t.Fatalf("expected second refresh to be fresh, got %+v", fresh)
	}
}

func TestFeatureLintReportsUnclaimedAndDuplicatePatchClaims(t *testing.T) {
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "a.cc"), "a base\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "b.cc"), "b base\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "chrome", "a.cc"), testPatchContent("chrome/a.cc", "a base", "a new"))
	writeFile(t, filepath.Join(repoInfo.PatchesDir, "chrome", "b.cc"), testPatchContent("chrome/b.cc", "b base", "b new"))
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  broad:
    description: "chore: broad"
    files:
      - chrome/
  specific:
    description: "chore: specific"
    files:
      - chrome/a.cc
`)

	result, err := LintFeatures(repoInfo)
	if err != nil {
		t.Fatalf("LintFeatures: %v", err)
	}
	if len(result.Unclaimed) != 0 {
		t.Fatalf("expected no unclaimed patches, got %v", result.Unclaimed)
	}
	if len(result.Duplicates) != 1 || result.Duplicates[0].Path != "chrome/a.cc" {
		t.Fatalf("expected duplicate chrome/a.cc, got %+v", result.Duplicates)
	}
	if err := result.Error(); err == nil || !strings.Contains(err.Error(), "chrome/a.cc claimed by broad, specific") {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  specific:
    description: "chore: specific"
    files:
      - chrome/a.cc
`)
	result, err = LintFeatures(repoInfo)
	if err != nil {
		t.Fatalf("LintFeatures unclaimed: %v", err)
	}
	if !slices.Equal(result.Unclaimed, []string{"chrome/b.cc"}) {
		t.Fatalf("expected chrome/b.cc unclaimed, got %v", result.Unclaimed)
	}
}

func TestFeatureAddExcludesExistingClaimsAndLintPasses(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "existing.cc"), "existing base\n")
	runGit(t, workspacePath, "add", "chrome/existing.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	runGit(t, workspacePath, "checkout", "-b", "browseros")
	runGit(t, workspacePath, "checkout", "-b", "task/demo")
	writeFile(t, filepath.Join(workspacePath, "chrome", "existing.cc"), "existing task\n")
	writeFile(t, filepath.Join(workspacePath, "chrome", "new.cc"), "new task\n")
	runGit(t, workspacePath, "add", "chrome")
	runGit(t, workspacePath, "commit", "-m", "feat: demo")

	repoInfo := newPatchRepo(t, baseCommit)
	initialFeatures := `version: "1.0"
features:
  # keep curated comments
  existing:
    description: "chore: existing"
    files:
      - chrome/existing.cc
`
	writeFeaturesYAML(t, repoInfo.Root, initialFeatures)
	if _, err := Extract(ctx, ExtractOptions{
		Workspace:  workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:       repoInfo,
		RangeStart: "browseros",
		RangeEnd:   "task/demo",
		Squash:     true,
	}); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	result, err := AddFeatureFromRange(ctx, FeatureAddOptions{
		Workspace:   workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:        repoInfo,
		Name:        "demo",
		Description: "feat: demo",
		RangeStart:  "browseros",
		RangeEnd:    "task/demo",
	})
	if err != nil {
		t.Fatalf("AddFeatureFromRange: %v", err)
	}
	if !slices.Equal(result.Added, []string{"chrome/new.cc"}) {
		t.Fatalf("added files = %v, want chrome/new.cc", result.Added)
	}
	if len(result.Excluded) != 1 || result.Excluded[0].Path != "chrome/existing.cc" {
		t.Fatalf("expected existing file excluded, got %+v", result.Excluded)
	}
	lint, err := LintFeatures(repoInfo)
	if err != nil {
		t.Fatalf("LintFeatures: %v", err)
	}
	if err := lint.Error(); err != nil {
		t.Fatalf("feature lint should pass after add: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(repoInfo.Root, "bos_build", "features.yaml"))
	if err != nil {
		t.Fatalf("read features: %v", err)
	}
	if !strings.HasPrefix(string(body), initialFeatures+"\n") {
		t.Fatalf("feature add should preserve existing yaml text, got:\n%s", body)
	}
	for _, want := range []string{"demo:", `description: "feat: demo"`, "chrome/new.cc"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("expected features yaml to contain %q, got:\n%s", want, body)
		}
	}
}

func TestFeatureAddFailsBeforeRangeFilesAreExtracted(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "existing.cc"), "existing base\n")
	runGit(t, workspacePath, "add", "chrome/existing.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")
	runGit(t, workspacePath, "checkout", "-b", "browseros")
	runGit(t, workspacePath, "checkout", "-b", "task/demo")
	writeFile(t, filepath.Join(workspacePath, "chrome", "new.cc"), "new task\n")
	runGit(t, workspacePath, "add", "chrome/new.cc")
	runGit(t, workspacePath, "commit", "-m", "feat: demo")

	repoInfo := newPatchRepo(t, baseCommit)
	writeFeaturesYAML(t, repoInfo.Root, `version: "1.0"
features:
  existing:
    description: "chore: existing"
    files:
      - chrome/existing.cc
`)
	_, err := AddFeatureFromRange(ctx, FeatureAddOptions{
		Workspace:   workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:        repoInfo,
		Name:        "demo",
		Description: "feat: demo",
		RangeStart:  "browseros",
		RangeEnd:    "task/demo",
	})
	if err == nil || !strings.Contains(err.Error(), "not extracted to chromium_patches: chrome/new.cc") {
		t.Fatalf("expected missing extraction error, got %v", err)
	}
}

func TestOrphanSummaryGroupsByTopLevelDir(t *testing.T) {
	groups := OrphanSummary([]string{
		"chrome/app/one.cc",
		"chrome/browser/two.cc",
		"third_party/sparkle/bin",
		"BUILD.gn",
	})
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %v", groups)
	}
	if groups[0].Dir != "chrome" || groups[0].Count != 2 {
		t.Fatalf("expected chrome first with count 2, got %v", groups)
	}
	rest := map[string]int{groups[1].Dir: groups[1].Count, groups[2].Dir: groups[2].Count}
	if rest["third_party"] != 1 || rest["(root)"] != 1 {
		t.Fatalf("unexpected groups: %v", groups)
	}
}

func TestInSyncButUnreproducible(t *testing.T) {
	status := &WorkspaceStatus{Orphaned: []string{"chrome/x"}}
	if !status.InSyncButUnreproducible() {
		t.Fatalf("expected hint condition with only orphans present")
	}
	status.NeedsApply = []string{"chrome/y"}
	if status.InSyncButUnreproducible() {
		t.Fatalf("hint must not fire when applies are pending")
	}
}

func TestSyncClearsPendingStashAfterSuccessfulNonRebaseRun(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	remoteRepo := t.TempDir()
	runGit(t, remoteRepo, "init", "--bare")

	repoRoot := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "chromium_patches"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), baseCommit+"\n")
	runGit(t, repoRoot, "add", "BASE_COMMIT")
	runGit(t, repoRoot, "commit", "-m", "patch repo init")
	runGit(t, repoRoot, "remote", "add", "origin", remoteRepo)
	runGit(t, repoRoot, "push", "-u", "origin", "HEAD")
	repoHead := gitOutput(t, repoRoot, "rev-parse", "HEAD")

	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}
	if err := workspace.SaveState(workspacePath, &workspace.State{
		Version:      1,
		Workspace:    workspacePath,
		BaseCommit:   baseCommit,
		LastSyncRev:  repoHead,
		PendingStash: "stash@{42}",
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	result, err := Sync(ctx, SyncOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Remote:    "origin",
		Rebase:    false,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.StashRef != "" {
		t.Fatalf("expected no new stash ref, got %q", result.StashRef)
	}

	state, err := workspace.LoadState(workspacePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.PendingStash != "" {
		t.Fatalf("expected pending stash to be cleared, got %q", state.PendingStash)
	}
}

func TestSyncReportsPatchRepoProgress(t *testing.T) {
	ctx := context.Background()
	workspacePath := initGitRepo(t)
	writeFile(t, filepath.Join(workspacePath, "chrome", "browser.cc"), "base\n")
	runGit(t, workspacePath, "add", "chrome/browser.cc")
	runGit(t, workspacePath, "commit", "-m", "workspace base")
	baseCommit := gitOutput(t, workspacePath, "rev-parse", "HEAD")

	remoteRepo := t.TempDir()
	runGit(t, remoteRepo, "init", "--bare")

	repoRoot := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "chromium_patches"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(repoRoot, "BASE_COMMIT"), baseCommit+"\n")
	runGit(t, repoRoot, "add", "BASE_COMMIT")
	runGit(t, repoRoot, "commit", "-m", "patch repo init")
	runGit(t, repoRoot, "remote", "add", "origin", remoteRepo)
	runGit(t, repoRoot, "push", "-u", "origin", "HEAD")

	repoInfo, err := repo.Load(repoRoot)
	if err != nil {
		t.Fatalf("repo.Load: %v", err)
	}
	progress := &progressRecorder{}
	_, err = Sync(ctx, SyncOptions{
		Workspace: workspace.Entry{Name: "ws", Path: workspacePath},
		Repo:      repoInfo,
		Remote:    "origin",
		Progress:  progress,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	progress.requireContains(t, "Checking patch repo status")
	progress.requireContains(t, "Pulling patch repo from origin/")
	progress.requireContains(t, "Inspecting workspace drift")
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	return dir
}

type progressRecorder struct {
	messages []string
}

func (p *progressRecorder) Step(message string) {
	p.messages = append(p.messages, message)
}

func (p *progressRecorder) requireContains(t *testing.T, want string) {
	t.Helper()
	if slices.ContainsFunc(p.messages, func(message string) bool {
		return strings.Contains(message, want)
	}) {
		return
	}
	t.Fatalf("progress missing %q in %#v", want, p.messages)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeFeaturesYAML(t *testing.T, repoRoot string, body string) {
	t.Helper()
	writeFile(t, filepath.Join(repoRoot, "bos_build", "features.yaml"), body)
}

func writePatchFromEdit(t *testing.T, ctx context.Context, workspacePath string, repoInfo *repo.Info, baseCommit string, rel string, body string) {
	t.Helper()
	writeFile(t, filepath.Join(workspacePath, filepath.FromSlash(rel)), body)
	diff, err := git.DiffText(ctx, workspacePath, baseCommit, "--", rel)
	if err != nil {
		t.Fatalf("DiffText %s: %v", rel, err)
	}
	writeFile(t, filepath.Join(repoInfo.PatchesDir, filepath.FromSlash(rel)), diff)
	runGit(t, workspacePath, "checkout", baseCommit, "--", rel)
}

func testPatchContent(rel string, oldLine string, newLine string) string {
	return "diff --git a/" + rel + " b/" + rel + "\n" +
		"index 0000000000000000000000000000000000000000..1111111111111111111111111111111111111111 100644\n" +
		"--- a/" + rel + "\n" +
		"+++ b/" + rel + "\n" +
		"@@ -1 +1 @@\n" +
		"-" + oldLine + "\n" +
		"+" + newLine + "\n"
}

func assertFile(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("unexpected file contents for %s: got %q want %q", path, string(data), want)
	}
}
