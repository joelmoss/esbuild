package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/evanw/esbuild/internal/fs"
)

// Windows refuses a rename while another build's rename of the same output is in flight, and
// the output cannot be read in that window either. A build retries rather than failing, and
// leaves no temporary file behind whichever way it ends.
func withRename(t *testing.T, retry bool, rename func(string, string) error) {
	t.Helper()
	oldRename, oldRetry := renameFile, retryRenames
	renameFile, retryRenames = rename, retry
	t.Cleanup(func() { renameFile, retryRenames = oldRename, oldRetry })
}

var errDenied = errors.New("Access is denied.")

// Refuses the first "refusals" renames, then renames.
func refusing(refusals int, calls *int) func(string, string) error {
	return func(from string, to string) error {
		*calls++
		if *calls <= refusals {
			return errDenied
		}
		return os.Rename(from, to)
	}
}

func writeOutput(t *testing.T) (string, error) {
	t.Helper()
	dir := t.TempDir()
	realFS, err := fs.RealFS(fs.RealFSOptions{AbsWorkingDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return dir, writeFileAtomically(realFS, filepath.Join(dir, "chunk.js"), []byte("chunk"), 0o644)
}

func noTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, ".esbuild-*"))
	if len(matches) > 0 {
		t.Errorf("expected no temporary file left behind, found %v", matches)
	}
}

func TestRenameRetriesWhileRefused(t *testing.T) {
	calls := 0
	withRename(t, true, refusing(3, &calls))

	dir, err := writeOutput(t)

	if err != nil {
		t.Fatalf("expected the write to succeed once the rename stopped being refused: %v", err)
	}
	if contents, _ := os.ReadFile(filepath.Join(dir, "chunk.js")); string(contents) != "chunk" {
		t.Errorf("expected the output to hold its contents, got %q", contents)
	}
	if calls != 4 {
		t.Errorf("expected 4 rename attempts, got %d", calls)
	}
	noTemporaryFiles(t, dir)
}

func TestRenameGivesUpAfterItsAttempts(t *testing.T) {
	calls := 0
	withRename(t, true, refusing(renameAttempts, &calls))

	dir, err := writeOutput(t)

	if !errors.Is(err, errDenied) {
		t.Fatalf("expected the refusal back, got %v", err)
	}
	if calls != renameAttempts {
		t.Errorf("expected %d rename attempts, got %d", renameAttempts, calls)
	}
	noTemporaryFiles(t, dir)
}

// Off Windows a refused rename is not transient, so it is not retried.
func TestRenameIsNotRetriedOffWindows(t *testing.T) {
	calls := 0
	withRename(t, false, refusing(1, &calls))

	dir, err := writeOutput(t)

	if !errors.Is(err, errDenied) || calls != 1 {
		t.Fatalf("expected one attempt and the refusal back, got %d and %v", calls, err)
	}
	noTemporaryFiles(t, dir)
}
