package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

type FileChange struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
}

func Run(ctx context.Context, dir string, stdin []byte, args ...string) (Result, error) {
	return RunEnv(ctx, dir, stdin, nil, args...)
}

func RunEnv(ctx context.Context, dir string, stdin []byte, env []string, args ...string) (Result, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	if len(env) > 0 {
		command.Env = append(os.Environ(), env...)
	}
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := -1
	if command.ProcessState != nil {
		code = command.ProcessState.ExitCode()
	}
	result := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   code,
	}
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, err
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return result, err
	}
	if command.ProcessState == nil {
		return result, err
	}
	return result, nil
}

func HeadRev(ctx context.Context, dir string) (string, error) {
	result, err := Run(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func RevParse(ctx context.Context, dir string, ref string) (string, error) {
	result, err := Run(ctx, dir, nil, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func CurrentBranch(ctx context.Context, dir string) (string, error) {
	result, err := Run(ctx, dir, nil, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func IsDirty(ctx context.Context, dir string) (bool, error) {
	return IsDirtyPaths(ctx, dir, nil)
}

func IsTrackedDirty(ctx context.Context, dir string) (bool, error) {
	result, err := Run(ctx, dir, nil, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, err
	}
	if result.Code != 0 {
		return false, errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func IsDirtyPaths(ctx context.Context, dir string, pathspecs []string) (bool, error) {
	args := []string{"status", "--porcelain"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return false, err
	}
	if result.Code != 0 {
		return false, errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

// StatusPorcelain returns tracked and untracked working-tree changes.
func StatusPorcelain(ctx context.Context, dir string, pathspecs []string) ([]FileChange, error) {
	args := []string{"-c", "core.quotepath=false", "status", "--porcelain", "--untracked-files=all"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	var changes []FileChange
	for _, line := range strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		rel := line[3:]
		change := FileChange{Status: status, Path: rel}
		if strings.Contains(rel, " -> ") {
			parts := strings.SplitN(rel, " -> ", 2)
			change.OldPath = parts[0]
			change.Path = parts[1]
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// CommitPaths creates a commit scoped to paths and returns the new HEAD.
func CommitPaths(ctx context.Context, dir string, message string, paths []string) (string, error) {
	args := []string{"commit", "-m", message}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return HeadRev(ctx, dir)
}

func CommitPathsWithBody(ctx context.Context, dir string, subject string, body string, paths []string) (string, error) {
	return CommitPathsWithBodyEnv(ctx, dir, subject, body, nil, paths)
}

func CommitPathsWithBodyEnv(ctx context.Context, dir string, subject string, body string, env []string, paths []string) (string, error) {
	args := []string{"commit", "-m", subject}
	if body != "" {
		args = append(args, "-m", body)
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	result, err := RunEnv(ctx, dir, nil, env, args...)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return HeadRev(ctx, dir)
}

func CommitUnixTime(ctx context.Context, dir string, ref string) (string, error) {
	result, err := Run(ctx, dir, nil, "show", "-s", "--format=%ct", ref)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func CommitIndexWithBodyEnv(ctx context.Context, dir string, subject string, body string, env []string) (string, error) {
	tree, err := writeTree(ctx, dir)
	if err != nil {
		return "", err
	}
	parent, err := HeadRev(ctx, dir)
	if err != nil {
		return "", err
	}
	message := subject + "\n"
	if body != "" {
		message += "\n" + body + "\n"
	}
	// Bypass hooks and signing so refresh materialization hashes match across checkouts.
	result, err := RunEnv(ctx, dir, []byte(message), env, "-c", "commit.gpgsign=false", "commit-tree", tree, "-p", parent)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	commit := strings.TrimSpace(result.Stdout)
	if err := ResetSoft(ctx, dir, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func writeTree(ctx context.Context, dir string) (string, error) {
	result, err := Run(ctx, dir, nil, "write-tree")
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func CommitTrailer(ctx context.Context, dir string, ref string, key string) (string, error) {
	result, err := Run(ctx, dir, nil, "log", "-1", "--format=%B", ref)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	prefix := key + ":"
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
		}
	}
	return "", nil
}

func CommitExists(ctx context.Context, dir string, ref string) (bool, error) {
	result, err := Run(ctx, dir, nil, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return false, err
	}
	return result.Code == 0, nil
}

func FileExistsAtCommit(ctx context.Context, dir string, ref string, rel string) (bool, error) {
	result, err := Run(ctx, dir, nil, "cat-file", "-e", fmt.Sprintf("%s:%s", ref, rel))
	if err != nil {
		return false, err
	}
	return result.Code == 0, nil
}

func ShowFile(ctx context.Context, dir string, ref string, rel string) ([]byte, error) {
	result, err := Run(ctx, dir, nil, "show", fmt.Sprintf("%s:%s", ref, rel))
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	return []byte(result.Stdout), nil
}

func CheckoutFiles(ctx context.Context, dir string, ref string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := []string{"checkout", ref, "--"}
	args = append(args, paths...)
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func ResetPathToCommit(ctx context.Context, dir string, ref string, rel string) error {
	exists, err := FileExistsAtCommit(ctx, dir, ref, rel)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if exists {
		return CheckoutFiles(ctx, dir, ref, []string{rel})
	}
	return os.RemoveAll(target)
}

// diffArgs builds a diff invocation whose output is a pure function of the
// compared content: per-checkout config (abbrev, prefixes, algorithm, context)
// must not leak into patch files that get committed to the patch repo.
func diffArgs(extra ...string) []string {
	args := []string{
		"-c", "diff.algorithm=myers",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"-c", "diff.interHunkContext=0",
		"-c", "diff.suppressBlankEmpty=false",
		"-c", "core.quotepath=false",
		"diff", "--binary", "--full-index", "-U3", "--no-ext-diff", "--no-textconv",
	}
	return append(args, extra...)
}

func DiffText(ctx context.Context, dir string, args ...string) (string, error) {
	result, err := Run(ctx, dir, nil, diffArgs(append([]string{"-M"}, args...)...)...)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

func DiffNoIndex(ctx context.Context, dir string, path string) (string, error) {
	result, err := Run(ctx, dir, nil, diffArgs("--no-index", "--", "/dev/null", path)...)
	if err != nil {
		return "", err
	}
	if result.Code != 0 && result.Code != 1 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// FileModeAtCommit returns the tree mode (e.g. 100644/100755) for rel at ref,
// or "" with no error when the path has no entry there. Real git failures
// propagate so callers don't silently mis-mode files.
func FileModeAtCommit(ctx context.Context, dir string, ref string, rel string) (string, error) {
	result, err := Run(ctx, dir, nil, "ls-tree", ref, "--", rel)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

func ListUntracked(ctx context.Context, dir string, pathspecs []string) ([]string, error) {
	args := []string{"ls-files", "--others", "--exclude-standard"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	lines := splitLines(result.Stdout)
	return lines, nil
}

func DiffNameStatusBetween(ctx context.Context, dir string, from string, to string, pathspecs []string) ([]FileChange, error) {
	args := []string{"diff", "--name-status", "-M", fmt.Sprintf("%s..%s", from, to)}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	return runNameStatus(ctx, dir, args...)
}

func DiffTreeNameStatus(ctx context.Context, dir string, ref string, pathspecs []string) ([]FileChange, error) {
	args := []string{"diff-tree", "--no-commit-id", "--name-status", "-r", ref}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	return runNameStatus(ctx, dir, args...)
}

func RevListRange(ctx context.Context, dir string, start string, end string) ([]string, error) {
	result, err := Run(ctx, dir, nil, "rev-list", "--reverse", fmt.Sprintf("%s..%s", start, end))
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	return splitLines(result.Stdout), nil
}

func RevListCount(ctx context.Context, dir string, start string, end string) (int, error) {
	result, err := Run(ctx, dir, nil, "rev-list", "--count", fmt.Sprintf("%s..%s", start, end))
	if err != nil {
		return 0, err
	}
	if result.Code != 0 {
		return 0, errors.New(strings.TrimSpace(result.Stderr))
	}
	count, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil {
		return 0, err
	}
	return count, nil
}

func IsAncestor(ctx context.Context, dir string, ancestor string, descendant string) (bool, error) {
	result, err := Run(ctx, dir, nil, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		return false, err
	}
	switch result.Code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, errors.New(strings.TrimSpace(result.Stderr))
	}
}

func ApplyPatch(ctx context.Context, dir string, patch []byte) (string, error) {
	strategies := [][]string{
		{"apply", "--ignore-whitespace", "--whitespace=nowarn", "-p1"},
		{"apply", "--ignore-whitespace", "--whitespace=nowarn", "-p1", "--3way"},
		{"apply", "--ignore-whitespace", "--whitespace=fix", "-p1"},
		{"apply", "--reject", "--ignore-whitespace", "--whitespace=nowarn", "-p1"},
	}
	var lastErr string
	for _, args := range strategies {
		result, err := Run(ctx, dir, patch, args...)
		if err != nil {
			return "", err
		}
		if result.Code == 0 {
			return strings.Join(args[1:], " "), nil
		}
		lastErr = strings.TrimSpace(result.Stderr)
	}
	if lastErr == "" {
		lastErr = "git apply failed"
	}
	return "", errors.New(lastErr)
}

func StashPush(ctx context.Context, dir string, message string, includeUntracked bool, paths []string) (string, error) {
	args := []string{"stash", "push", "-m", message}
	if includeUntracked {
		args = append(args, "-u")
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	if strings.Contains(result.Stdout, "No local changes to save") {
		return "", nil
	}
	// Record the stash by commit SHA: positional stash@{N} refs shift on any
	// later stash and would make us restore (and drop) the wrong entry.
	list, err := Run(ctx, dir, nil, "stash", "list", "-1", "--format=%H")
	if err != nil {
		return "", err
	}
	if list.Code != 0 {
		return "", errors.New(strings.TrimSpace(list.Stderr))
	}
	return strings.TrimSpace(list.Stdout), nil
}

// ErrStashNotFound reports a recorded stash that is no longer in the stash
// list (dropped or popped outside the tool).
var ErrStashNotFound = errors.New("stash entry not found")

// StashEntryExists reports whether a recorded stash SHA/ref is still listed.
func StashEntryExists(ctx context.Context, dir string, ref string) (bool, error) {
	_, err := resolveStashEntry(ctx, dir, ref)
	if errors.Is(err, ErrStashNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// resolveStashEntry maps a stash commit SHA or positional ref to the current
// positional name.
func resolveStashEntry(ctx context.Context, dir string, ref string) (string, error) {
	result, err := Run(ctx, dir, nil, "-c", "core.quotepath=false", "stash", "list", "--format=%gd %H")
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", errors.New(strings.TrimSpace(result.Stderr))
	}
	for _, line := range splitLines(result.Stdout) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[0] == ref || fields[1] == ref || strings.HasPrefix(fields[1], ref) {
			return fields[0], nil
		}
	}
	return "", ErrStashNotFound
}

// StashConflictError reports a stash pop that left merge conflicts in the
// working tree; git keeps the stash entry in that case.
type StashConflictError struct {
	Files []string
}

func (e *StashConflictError) Error() string {
	return fmt.Sprintf("stash pop conflicted in %s", strings.Join(e.Files, ", "))
}

func StashPop(ctx context.Context, dir string, stashRef string) error {
	args := []string{"stash", "pop"}
	if stashRef != "" {
		// git stash pop rejects raw commit SHAs; translate to the current
		// positional entry (also catches records that no longer exist).
		entry, err := resolveStashEntry(ctx, dir, stashRef)
		if err != nil {
			return err
		}
		args = append(args, entry)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code == 0 {
		return nil
	}
	unmerged, unmergedErr := UnmergedFiles(ctx, dir)
	if unmergedErr == nil && len(unmerged) > 0 {
		return &StashConflictError{Files: unmerged}
	}
	return errors.New(strings.TrimSpace(result.Stderr))
}

func UnmergedFiles(ctx context.Context, dir string) ([]string, error) {
	result, err := Run(ctx, dir, nil, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	return splitLines(result.Stdout), nil
}

// ConflictMarkerFiles returns tracked files that carry leftover conflict
// markers relative to HEAD — the state a stash-rebase conflict leaves as plain
// modified files (no unmerged index entry). It relies on git's own detector
// ("git diff --check"), filtering to marker lines so trailing-whitespace
// warnings in legitimately patched files are ignored.
func ConflictMarkerFiles(ctx context.Context, dir string) ([]string, error) {
	result, err := Run(ctx, dir, nil, "-c", "core.quotepath=false", "diff", "--check", "HEAD")
	if err != nil {
		return nil, err
	}
	// --check exits 2 when it reports markers or whitespace; both are expected
	// signals, not command failures. A negative code means git never ran.
	if result.Code < 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	seen := map[string]bool{}
	var files []string
	for _, line := range splitLines(result.Stdout) {
		if !strings.HasSuffix(line, "leftover conflict marker") {
			continue
		}
		path := line
		if idx := strings.Index(line, ":"); idx > 0 {
			path = line[:idx]
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files, nil
}

// StashRebase replays a stash on top of the current working tree with a
// per-file 3-way merge (stash parent as base). Unlike `git stash pop`, it
// works when the files were modified after the stash was taken — exactly the
// state a patch apply leaves behind. On success the stash entry is dropped;
// conflicts keep the stash entry and surface as a StashConflictError (content
// conflicts get markers; modify/delete conflicts restore the stashed bytes
// for visibility). Accepts a stash commit SHA or a stash@{N} ref; returns
// ErrStashNotFound when the entry no longer exists.
func StashRebase(ctx context.Context, dir string, stashRef string) error {
	if stashRef == "" {
		stashRef = "stash@{0}"
	}
	entry, err := resolveStashEntry(ctx, dir, stashRef)
	if err != nil {
		return err
	}
	var conflicts []string

	tracked, err := stashTrackedFiles(ctx, dir, entry)
	if err != nil {
		return err
	}
	for _, rel := range tracked {
		conflicted, err := rebaseStashedFile(ctx, dir, entry, rel)
		if err != nil {
			return err
		}
		if conflicted {
			conflicts = append(conflicts, rel)
		}
	}

	untracked, err := stashUntrackedFiles(ctx, dir, entry)
	if err != nil {
		return err
	}
	for _, rel := range untracked {
		workPath := filepath.Join(dir, filepath.FromSlash(rel))
		if _, statErr := os.Stat(workPath); os.IsNotExist(statErr) {
			if err := restoreFromTree(ctx, dir, entry+"^3", rel); err != nil {
				return err
			}
			continue
		}
		theirs, err := ShowFile(ctx, dir, entry+"^3", rel)
		if err != nil {
			return err
		}
		conflicted, err := mergeIntoWorkingFile(ctx, dir, rel, nil, theirs)
		if err != nil {
			return err
		}
		if conflicted {
			conflicts = append(conflicts, rel)
		}
	}

	if len(conflicts) > 0 {
		return &StashConflictError{Files: conflicts}
	}
	result, err := Run(ctx, dir, nil, "stash", "drop", entry)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func stashTrackedFiles(ctx context.Context, dir string, stashRef string) ([]string, error) {
	result, err := Run(ctx, dir, nil, "-c", "core.quotepath=false", "diff", "--name-only", "--no-renames", stashRef+"^1", stashRef)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	return splitLines(result.Stdout), nil
}

func stashUntrackedFiles(ctx context.Context, dir string, stashRef string) ([]string, error) {
	parents, err := Run(ctx, dir, nil, "rev-list", "--parents", "-n", "1", stashRef)
	if err != nil {
		return nil, err
	}
	if parents.Code != 0 {
		return nil, errors.New(strings.TrimSpace(parents.Stderr))
	}
	fields := strings.Fields(parents.Stdout)
	if len(fields) < 4 {
		return nil, nil
	}
	result, err := Run(ctx, dir, nil, "-c", "core.quotepath=false", "ls-tree", "-r", "--name-only", fields[3])
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	return splitLines(result.Stdout), nil
}

// LooksBinary mirrors git's heuristic: a NUL within the first 8000 bytes.
func LooksBinary(content []byte) bool {
	probe := content
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	return bytes.IndexByte(probe, 0) != -1
}

// restoreFromTree writes a file from a tree-ish into the working tree,
// preserving the executable bit recorded there.
func restoreFromTree(ctx context.Context, dir string, treeish string, rel string) error {
	content, err := ShowFile(ctx, dir, treeish, rel)
	if err != nil {
		return err
	}
	perm, err := filePermAtTree(ctx, dir, treeish, rel)
	if err != nil {
		return err
	}
	workPath := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(workPath), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(workPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(workPath); err != nil {
			return err
		}
	}
	return os.WriteFile(workPath, content, perm)
}

func filePermAtTree(ctx context.Context, dir string, treeish string, rel string) (os.FileMode, error) {
	mode, err := FileModeAtCommit(ctx, dir, treeish, rel)
	if err != nil {
		return 0, err
	}
	if mode == "100755" {
		return 0o755, nil
	}
	return 0o644, nil
}

func chmodFromTree(ctx context.Context, dir string, treeish string, rel string) error {
	perm, err := filePermAtTree(ctx, dir, treeish, rel)
	if err != nil {
		return err
	}
	return os.Chmod(filepath.Join(dir, filepath.FromSlash(rel)), perm)
}

func rebaseStashedFile(ctx context.Context, dir string, stashRef string, rel string) (bool, error) {
	// Distinguish "path absent at ref" from real git failures before reading
	// content — conflating them could misread an outage as a deletion.
	baseExists, err := FileExistsAtCommit(ctx, dir, stashRef+"^1", rel)
	if err != nil {
		return false, err
	}
	var base []byte
	if baseExists {
		if base, err = ShowFile(ctx, dir, stashRef+"^1", rel); err != nil {
			return false, err
		}
	}
	theirsExists, err := FileExistsAtCommit(ctx, dir, stashRef, rel)
	if err != nil {
		return false, err
	}
	workPath := filepath.Join(dir, filepath.FromSlash(rel))
	if !theirsExists {
		// Stash recorded a deletion. Re-delete when the tree still matches
		// the stash base; otherwise keep the newer content and flag it.
		current, readErr := os.ReadFile(workPath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return false, nil
			}
			return false, readErr
		}
		if baseExists && bytes.Equal(current, base) {
			return false, os.Remove(workPath)
		}
		return true, nil
	}
	theirs, err := ShowFile(ctx, dir, stashRef, rel)
	if err != nil {
		return false, err
	}
	if _, statErr := os.Stat(workPath); os.IsNotExist(statErr) {
		// The file is gone from the tree (e.g. a patch deleted it) while the
		// stash modified it: a modify/delete conflict. Restore the stashed
		// content so the user can see and decide, and keep the stash entry.
		if err := restoreFromTree(ctx, dir, stashRef, rel); err != nil {
			return false, err
		}
		return baseExists, nil
	}
	conflicted, err := mergeIntoWorkingFile(ctx, dir, rel, base, theirs)
	if err != nil || conflicted {
		return conflicted, err
	}
	return false, chmodFromTree(ctx, dir, stashRef, rel)
}

// mergeIntoWorkingFile 3-way merges stashed content into the working file.
// A nil base means the stashed file had no ancestor (brand-new file).
func mergeIntoWorkingFile(ctx context.Context, dir string, rel string, base []byte, theirs []byte) (bool, error) {
	workPath := filepath.Join(dir, filepath.FromSlash(rel))
	current, err := os.ReadFile(workPath)
	if err != nil {
		return false, err
	}
	if bytes.Equal(current, theirs) {
		return false, nil
	}
	// merge-file cannot merge binary content (exit 255). Treat diverged
	// binaries as a conflict the user resolves by hand — never a hard error
	// that would wedge sync retries.
	if LooksBinary(current) || LooksBinary(theirs) || LooksBinary(base) {
		return true, nil
	}

	tmpDir, err := os.MkdirTemp("", "browseros-patch-stash")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmpDir)
	basePath := filepath.Join(tmpDir, "base")
	theirsPath := filepath.Join(tmpDir, "stashed")
	if err := os.WriteFile(basePath, base, 0o644); err != nil {
		return false, err
	}
	if err := os.WriteFile(theirsPath, theirs, 0o644); err != nil {
		return false, err
	}
	result, err := Run(ctx, dir, nil,
		"merge-file",
		"-L", "local", "-L", "base", "-L", "stashed",
		workPath, basePath, theirsPath)
	if err != nil {
		return false, err
	}
	// merge-file exits with the conflict count; large codes signal errors.
	if result.Code < 0 || result.Code > 127 {
		return false, errors.New(strings.TrimSpace(result.Stderr))
	}
	return result.Code > 0, nil
}

func PullRebase(ctx context.Context, dir string, remote string, branch string) error {
	args := []string{"pull", "--rebase"}
	if remote != "" {
		args = append(args, remote)
		if branch != "" {
			args = append(args, branch)
		}
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func ResetHard(ctx context.Context, dir string) error {
	result, err := Run(ctx, dir, nil, "reset", "--hard")
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func ResetSoft(ctx context.Context, dir string, ref string) error {
	result, err := Run(ctx, dir, nil, "reset", "--soft", ref)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func CheckoutDetached(ctx context.Context, dir string, ref string) error {
	result, err := Run(ctx, dir, nil, "checkout", "--detach", ref)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func CheckoutBranch(ctx context.Context, dir string, branch string) error {
	result, err := Run(ctx, dir, nil, "checkout", branch)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func ForceBranch(ctx context.Context, dir string, branch string, ref string) error {
	result, err := Run(ctx, dir, nil, "branch", "-f", branch, ref)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

// AddPaths stages existing paths with git add.
func AddPaths(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

// AddAllPaths stages additions, modifications, and deletions under paths.
func AddAllPaths(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "-A", "--"}, paths...)
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func Commit(ctx context.Context, dir string, message string) error {
	result, err := Run(ctx, dir, nil, "commit", "-m", message)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func Push(ctx context.Context, dir string, remote string, branch string) error {
	args := []string{"push"}
	if remote != "" {
		args = append(args, remote)
	}
	if branch != "" {
		args = append(args, branch)
	}
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(strings.TrimSpace(result.Stderr))
	}
	return nil
}

func runNameStatus(ctx context.Context, dir string, args ...string) ([]FileChange, error) {
	result, err := Run(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	var changes []FileChange
	for _, line := range splitLines(result.Stdout) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		change := FileChange{Status: parts[0][:1], Path: parts[len(parts)-1]}
		if change.Status == "R" || change.Status == "C" {
			if len(parts) >= 3 {
				change.OldPath = parts[1]
			}
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func splitLines(raw string) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
