package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/app"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
	"github.com/spf13/cobra"
)

func TestPatchesRepoRemoteDriftFetchesOriginMain(t *testing.T) {
	local, upstream := setupWarningRepos(t)
	commitWarningFile(t, upstream, "remote.txt", "remote\n", "remote change")
	runWarningGit(t, upstream, "push", "origin", "main")

	drift, err := patchesRepoRemoteDrift(context.Background(), local)
	if err != nil {
		t.Fatalf("patchesRepoRemoteDrift: %v", err)
	}
	if drift.LocalAhead != 0 || drift.RemoteAhead != 1 {
		t.Fatalf("drift = %+v, want local=0 remote=1", drift)
	}
}

func TestPatchesRepoRemoteDriftQuietWhenEven(t *testing.T) {
	local, _ := setupWarningRepos(t)

	drift, err := patchesRepoRemoteDrift(context.Background(), local)
	if err != nil {
		t.Fatalf("patchesRepoRemoteDrift: %v", err)
	}
	if drift.hasDrift() {
		t.Fatalf("expected no drift, got %+v", drift)
	}
}

func TestWarnIfPatchesRepoDriftWritesPathToStderr(t *testing.T) {
	local, upstream := setupWarningRepos(t)
	commitWarningFile(t, upstream, "remote.txt", "remote\n", "remote change")
	runWarningGit(t, upstream, "push", "origin", "main")

	oldAppState := appState
	t.Cleanup(func() {
		appState = oldAppState
	})
	appState = &app.App{
		CWD: t.TempDir(),
		Config: &workspace.Config{
			PatchesRepo: local,
		},
	}

	var stderr bytes.Buffer
	cmd := &cobra.Command{Use: "status"}
	cmd.SetErr(&stderr)

	warnIfPatchesRepoDrift(cmd)

	output := stderr.String()
	for _, want := range []string{
		"remote is 1 commit ahead",
		local,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected warning to contain %q, got:\n%s", want, output)
		}
	}
}

func TestWarnIfPatchesRepoDriftPrefersChangedPatchesRepoFlag(t *testing.T) {
	configuredRepo, _ := setupWarningRepos(t)
	overrideRepo, overrideUpstream := setupWarningRepos(t)
	commitWarningFile(t, overrideUpstream, "remote.txt", "remote\n", "remote change")
	runWarningGit(t, overrideUpstream, "push", "origin", "main")

	oldAppState := appState
	t.Cleanup(func() {
		appState = oldAppState
	})
	appState = &app.App{
		CWD: t.TempDir(),
		Config: &workspace.Config{
			PatchesRepo: configuredRepo,
		},
	}

	var stderr bytes.Buffer
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("patches-repo", "", "")
	if err := cmd.Flags().Set("patches-repo", overrideRepo); err != nil {
		t.Fatalf("set patches-repo: %v", err)
	}
	cmd.SetErr(&stderr)

	warnIfPatchesRepoDrift(cmd)

	output := stderr.String()
	if !strings.Contains(output, overrideRepo) {
		t.Fatalf("expected warning to use override repo %q, got:\n%s", overrideRepo, output)
	}
	if strings.Contains(output, configuredRepo) {
		t.Fatalf("warning should not use configured repo %q when override is set, got:\n%s", configuredRepo, output)
	}
}

func setupWarningRepos(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	local := filepath.Join(root, "local")
	upstream := filepath.Join(root, "upstream")

	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	runWarningGit(t, root, "init", "--bare", "--initial-branch=main", origin)
	runWarningGit(t, seed, "init", "--initial-branch=main")
	configWarningGitUser(t, seed)
	commitWarningFile(t, seed, "base.txt", "base\n", "base")
	runWarningGit(t, seed, "remote", "add", "origin", origin)
	runWarningGit(t, seed, "push", "-u", "origin", "main")

	runWarningGit(t, root, "clone", origin, local)
	configWarningGitUser(t, local)
	runWarningGit(t, root, "clone", origin, upstream)
	configWarningGitUser(t, upstream)

	return local, upstream
}

func configWarningGitUser(t *testing.T, dir string) {
	t.Helper()
	runWarningGit(t, dir, "config", "user.name", "Test User")
	runWarningGit(t, dir, "config", "user.email", "test@example.com")
}

func commitWarningFile(t *testing.T, dir string, rel string, body string, message string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	runWarningGit(t, dir, "add", rel)
	runWarningGit(t, dir, "commit", "-m", message)
}

func runWarningGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
