package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"golang.org/x/sys/unix"
)

type Progress interface {
	Step(message string)
}

// WithRepoLock serializes mutating operations that share one patch repo worktree.
func WithRepoLock(ctx context.Context, repoRoot string, progress Progress, fn func() error) error {
	lockPath, err := repoLockPath(ctx, repoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if progress != nil {
		progress.Step("Waiting for patch repo lock")
	}
	if err := flock(ctx, int(file.Fd())); err != nil {
		return err
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	if progress != nil {
		progress.Step("Acquired patch repo lock")
	}
	return fn()
}

func repoLockPath(ctx context.Context, repoRoot string) (string, error) {
	result, err := git.Run(ctx, repoRoot, nil, "rev-parse", "--git-path", "browseros-patch.lock")
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(result.Stderr)
	}
	path := filepath.Clean(strings.TrimSpace(result.Stdout))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	return path, nil
}

func flock(ctx context.Context, fd int) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN && err != unix.EINTR {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("acquire patch repo lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
