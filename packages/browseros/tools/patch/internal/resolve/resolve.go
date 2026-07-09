package resolve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

type Operation struct {
	ChromiumPath string       `json:"chromium_path"`
	PatchRel     string       `json:"patch_rel"`
	Op           patch.FileOp `json:"op"`
	OldPath      string       `json:"old_path,omitempty"`
	RejectPath   string       `json:"reject_path,omitempty"`
	Message      string       `json:"message,omitempty"`
}

type State struct {
	Workspace  string      `json:"workspace"`
	RepoRoot   string      `json:"repo_root"`
	BaseCommit string      `json:"base_commit"`
	RepoRev    string      `json:"repo_rev,omitempty"`
	Mode       string      `json:"mode,omitempty"`
	Current    int         `json:"current"`
	Operations []Operation `json:"operations"`
	Resolved   []string    `json:"resolved,omitempty"`
	Skipped    []string    `json:"skipped,omitempty"`
	// RestorePendingStash marks that the paused operation was a rebase-mode
	// sync: when the conflict loop completes, the parked stash comes back.
	// Stashes parked explicitly with --no-rebase stay parked.
	RestorePendingStash bool `json:"restore_pending_stash,omitempty"`
	// AutoAnnotate marks that the paused apply wants feature commits once the
	// conflict loop completes, so continue/skip finish what apply started.
	AutoAnnotate bool `json:"auto_annotate,omitempty"`
}

func Path(workspacePath string) string {
	return filepath.Join(workspace.StateDir(workspacePath), "resolve.json")
}

func Exists(workspacePath string) bool {
	_, err := os.Stat(Path(workspacePath))
	return err == nil
}

func Load(workspacePath string) (*State, error) {
	data, err := os.ReadFile(Path(workspacePath))
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func Save(workspacePath string, state *State) error {
	dir := workspace.StateDir(workspacePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// Write atomically: a crash mid-write would otherwise leave a truncated
	// resolve.json that every recovery command (continue/skip/abort) fails to
	// parse, wedging the checkout.
	tmp, err := os.CreateTemp(dir, "resolve-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, Path(workspacePath)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func Delete(workspacePath string) error {
	if err := os.Remove(Path(workspacePath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func FindActive(reg *workspace.Registry, cwd string) (workspace.Entry, error) {
	if ws, err := workspace.Detect(reg, cwd); err == nil && Exists(ws.Path) {
		return ws, nil
	}
	var active []workspace.Entry
	for _, ws := range reg.Workspaces {
		if Exists(ws.Path) {
			active = append(active, ws)
		}
	}
	switch len(active) {
	case 0:
		return workspace.Entry{}, fmt.Errorf(`no active conflict resolution found; run "browseros-patch apply" or "browseros-patch sync" first`)
	case 1:
		return active[0], nil
	default:
		return workspace.Entry{}, fmt.Errorf("multiple Chromium checkouts have active conflicts; run from inside the target checkout")
	}
}

func (s *State) CurrentOperation() (Operation, error) {
	if s.Current < 0 || s.Current >= len(s.Operations) {
		return Operation{}, fmt.Errorf("no active conflict remaining")
	}
	return s.Operations[s.Current], nil
}
