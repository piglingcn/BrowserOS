package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/ui"
	"github.com/spf13/cobra"
)

const patchesRepoDriftTimeout = 5 * time.Second

type patchesRepoDrift struct {
	LocalAhead  int
	RemoteAhead int
}

func (d patchesRepoDrift) hasDrift() bool {
	return d.LocalAhead > 0 || d.RemoteAhead > 0
}

// warnIfPatchesRepoDrift prints a non-fatal freshness warning for the active patches repo.
func warnIfPatchesRepoDrift(cmd *cobra.Command) {
	repoPath := patchesRepoWarningPath(cmd)
	if repoPath == "" {
		return
	}
	baseCtx := cmd.Context()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, patchesRepoDriftTimeout)
	defer cancel()

	drift, err := patchesRepoRemoteDrift(ctx, repoPath)
	if err != nil || !drift.hasDrift() {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), formatPatchesRepoDriftWarning(repoPath, drift))
}

func patchesRepoWarningPath(cmd *cobra.Command) string {
	if cmd != nil {
		flag := cmd.Flags().Lookup("patches-repo")
		if flag != nil && flag.Changed {
			if override := strings.TrimSpace(flag.Value.String()); override != "" {
				return override
			}
		}
	}
	if appState == nil {
		return ""
	}
	if appState.Config != nil && appState.Config.PatchesRepo != "" {
		return appState.Config.PatchesRepo
	}
	if appState.CWD == "" {
		return ""
	}
	discovered, err := repo.Discover(appState.CWD)
	if err != nil {
		return ""
	}
	return discovered
}

// patchesRepoRemoteDrift fetches origin/main and counts commits on each side of HEAD.
func patchesRepoRemoteDrift(ctx context.Context, repoPath string) (patchesRepoDrift, error) {
	result, err := git.Run(ctx, repoPath, nil, "fetch", "--quiet", "origin", "+refs/heads/main:refs/remotes/origin/main")
	if err != nil {
		return patchesRepoDrift{}, err
	}
	if result.Code != 0 {
		return patchesRepoDrift{}, gitResultError(result)
	}

	result, err = git.Run(ctx, repoPath, nil, "rev-list", "--left-right", "--count", "HEAD...origin/main")
	if err != nil {
		return patchesRepoDrift{}, err
	}
	if result.Code != 0 {
		return patchesRepoDrift{}, gitResultError(result)
	}

	fields := strings.Fields(result.Stdout)
	if len(fields) != 2 {
		return patchesRepoDrift{}, fmt.Errorf("unexpected rev-list output: %q", strings.TrimSpace(result.Stdout))
	}
	localAhead, err := strconv.Atoi(fields[0])
	if err != nil {
		return patchesRepoDrift{}, fmt.Errorf("parse local drift: %w", err)
	}
	remoteAhead, err := strconv.Atoi(fields[1])
	if err != nil {
		return patchesRepoDrift{}, fmt.Errorf("parse remote drift: %w", err)
	}
	return patchesRepoDrift{LocalAhead: localAhead, RemoteAhead: remoteAhead}, nil
}

func formatPatchesRepoDriftWarning(repoPath string, drift patchesRepoDrift) string {
	return fmt.Sprintf(
		"%s patches repo differs from origin/main: %s\n%s %s",
		ui.Warning("Warning:"),
		patchesRepoDriftMessage(drift),
		ui.Muted("path:"),
		repoPath,
	)
}

func patchesRepoDriftMessage(drift patchesRepoDrift) string {
	var parts []string
	if drift.RemoteAhead > 0 {
		parts = append(parts, fmt.Sprintf("remote is %d %s ahead", drift.RemoteAhead, commitNoun(drift.RemoteAhead)))
	}
	if drift.LocalAhead > 0 {
		parts = append(parts, fmt.Sprintf("local is %d %s ahead", drift.LocalAhead, commitNoun(drift.LocalAhead)))
	}
	return strings.Join(parts, " and ") + "."
}

func commitNoun(count int) string {
	if count == 1 {
		return "commit"
	}
	return "commits"
}

func gitResultError(result git.Result) error {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if message == "" {
		message = fmt.Sprintf("git exited with status %d", result.Code)
	}
	return errors.New(message)
}
