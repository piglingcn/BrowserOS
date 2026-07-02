package lock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWithRepoLockUsesGitPrivateLockFile(t *testing.T) {
	ctx := context.Background()
	repoRoot := initGitRepo(t)
	called := false
	if err := WithRepoLock(ctx, repoRoot, nil, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithRepoLock: %v", err)
	}
	if !called {
		t.Fatalf("expected lock callback to run")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".git", "browseros-patch.lock")); err != nil {
		t.Fatalf("expected git-private lock file: %v", err)
	}
	if status := gitOutput(t, repoRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("lock file must not dirty repo, got %q", status)
	}
}

func TestWithRepoLockHonorsContextWhileWaiting(t *testing.T) {
	ctx := context.Background()
	repoRoot := initGitRepo(t)
	if err := WithRepoLock(ctx, repoRoot, nil, func() error {
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		errCh := make(chan error, 1)
		go func() {
			errCh <- WithRepoLock(waitCtx, repoRoot, nil, func() error {
				return errors.New("inner lock callback ran")
			})
		}()
		select {
		case err := <-errCh:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline exceeded, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("waiting lock did not honor context deadline")
		}
		return nil
	}); err != nil {
		t.Fatalf("WithRepoLock: %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(dir, "README.md"), "x\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	return dir
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
