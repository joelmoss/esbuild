package api_test

import (
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// Output files are written to a temporary file and renamed into place, and a file already
// holding the same bytes is not written at all. Writing in place truncated each file before
// filling it, so a server building on demand into a shared output directory handed out empty
// chunks whenever a request read one while another build was rewriting it. These tests pin that.

// An entry point and its lazy import share a CommonJS module, so splitting puts it in a chunk
// of its own that carries the __commonJS runtime helper.
func splitBuildOptions(t *testing.T) api.BuildOptions {
	dir := t.TempDir()
	files := map[string]string{
		"shared.cjs": "module.exports = { shared: 'shared-marker' };\n",
		"lazy.js":    "import { shared } from './shared.cjs';\nexport default shared;\n",
		"entry.js":   "import { shared } from './shared.cjs';\nconsole.log(shared);\nimport('./lazy.js');\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(path.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return api.BuildOptions{
		EntryPoints:   []string{path.Join(dir, "entry.js")},
		AbsWorkingDir: dir,
		Outdir:        path.Join(dir, "out"),
		Bundle:        true,
		Splitting:     true,
		Format:        api.FormatESModule,
		Write:         true,
		LogLevel:      api.LogLevelSilent,
	}
}

// Builds once, and returns the shared chunk's path and contents.
func buildSharedChunk(t *testing.T, options api.BuildOptions) (string, []byte) {
	t.Helper()

	result := api.Build(options)
	if len(result.Errors) > 0 {
		t.Fatalf("the build failed: %v", result.Errors)
	}

	for _, file := range result.OutputFiles {
		if strings.Contains(file.Path, "chunk-") {
			if !strings.Contains(string(file.Contents), "__commonJS") {
				t.Fatalf("expected the shared chunk to carry __commonJS, got: %s", file.Contents)
			}
			return file.Path, file.Contents
		}
	}

	t.Fatalf("expected a shared chunk among %d output files", len(result.OutputFiles))
	return "", nil
}

func TestBuildDoesNotRewriteOutputAlreadyOnDisk(t *testing.T) {
	options := splitBuildOptions(t)
	chunk, _ := buildSharedChunk(t, options)

	before, err := os.Stat(chunk)
	if err != nil {
		t.Fatal(err)
	}

	buildSharedChunk(t, options)

	after, err := os.Stat(chunk)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("expected a second build to leave %s alone, but it was rewritten", chunk)
	}
}

// Several builds race to create a missing chunk, and then keep building over it. A read can find
// nothing there at first, but whatever it finds must be the whole chunk.
func TestBuildOutputIsNeverReadHalfWritten(t *testing.T) {
	options := splitBuildOptions(t)
	chunk, want := buildSharedChunk(t, options)
	if err := os.Remove(chunk); err != nil {
		t.Fatal(err)
	}

	var done atomic.Bool
	var reads, seen, bad int
	reader := sync.WaitGroup{}
	reader.Add(1)
	go func() {
		defer reader.Done()
		for !done.Load() {
			contents, err := os.ReadFile(chunk)
			reads++
			if err != nil {
				continue
			}
			seen++
			if string(contents) != string(want) {
				bad++
			}
		}
	}()

	builders := sync.WaitGroup{}
	for i := 0; i < 4; i++ {
		builders.Add(1)
		go func() {
			defer builders.Done()
			for j := 0; j < 50; j++ {
				if result := api.Build(options); len(result.Errors) > 0 {
					t.Errorf("the build failed: %v", result.Errors)
					return
				}
			}
		}()
	}

	builders.Wait()
	done.Store(true)
	reader.Wait()

	if seen == 0 {
		t.Fatalf("none of %d reads found %s, so nothing was checked", reads, chunk)
	}
	if bad > 0 {
		t.Fatalf("%d of %d reads of %s were not the whole chunk", bad, seen, chunk)
	}
}

// A file that cannot be replaced fails the build, and leaves no temporary file behind.
func TestBuildLeavesNoTemporaryFileWhenAWriteFails(t *testing.T) {
	options := splitBuildOptions(t)
	chunk, _ := buildSharedChunk(t, options)

	// A directory where the chunk goes: the temporary file is written, and renaming it fails.
	if err := os.Remove(chunk); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path.Join(chunk, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := api.Build(options)

	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Text, "Failed to write to output file") {
		t.Fatalf("expected the write to fail, got: %v", result.Errors)
	}

	// Chunks land at the top of outdir, under the default chunk names.
	entries, err := os.ReadDir(options.Outdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".esbuild-") {
			t.Errorf("expected no temporary file left behind, found %s", entry.Name())
		}
	}
}
