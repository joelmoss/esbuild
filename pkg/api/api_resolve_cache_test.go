package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// A plugin's Resolve reads directories through the file system that contextImpl creates. Build()
// creates that context, runs a single rebuild and disposes it, so its file system may cache
// directory listings for that build. A Context() outlives its rebuilds, so it must not, or a file
// created between two rebuilds would go unseen. These tests pin both halves.

// A plugin that resolves the same path twice, with the file it names created in between.
func resolveAroundNewFilePlugin(t *testing.T, dir string, results *[2]api.ResolveResult) api.Plugin {
	return api.Plugin{
		Name: "resolve-around-new-file",
		Setup: func(build api.PluginBuild) {
			build.OnStart(func() (api.OnStartResult, error) {
				opts := api.ResolveOptions{Kind: api.ResolveJSImportStatement, ResolveDir: dir}
				results[0] = build.Resolve("./late.js", opts)

				if err := os.WriteFile(filepath.Join(dir, "late.js"), []byte("export {}\n"), 0o644); err != nil {
					t.Error(err)
				}

				results[1] = build.Resolve("./late.js", opts)
				return api.OnStartResult{}, nil
			})
		},
	}
}

func resolveCacheOptions(t *testing.T, results *[2]api.ResolveResult) api.BuildOptions {
	dir := t.TempDir()
	entry := filepath.Join(dir, "entry.js")
	if err := os.WriteFile(entry, []byte("console.log(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return api.BuildOptions{
		EntryPoints:   []string{entry},
		AbsWorkingDir: dir,
		Write:         false,
		LogLevel:      api.LogLevelSilent,
		Plugins:       []api.Plugin{resolveAroundNewFilePlugin(t, dir, results)},
	}
}

func TestBuildCachesDirectoryListingsForPluginResolve(t *testing.T) {
	var results [2]api.ResolveResult

	result := api.Build(resolveCacheOptions(t, &results))

	if len(result.Errors) > 0 {
		t.Fatalf("the build failed: %v", result.Errors)
	}
	if len(results[0].Errors) == 0 {
		t.Fatal("the first Resolve found a file that does not exist yet")
	}
	if len(results[1].Errors) == 0 {
		t.Fatal("the second Resolve saw a file created after the first, so directory listings are " +
			"not cached for the length of a one-shot Build")
	}
}

func TestContextDoesNotCacheDirectoryListingsForPluginResolve(t *testing.T) {
	var results [2]api.ResolveResult

	ctx, err := api.Context(resolveCacheOptions(t, &results))
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Dispose()

	if result := ctx.Rebuild(); len(result.Errors) > 0 {
		t.Fatalf("the rebuild failed: %v", result.Errors)
	}
	if len(results[0].Errors) == 0 {
		t.Fatal("the first Resolve found a file that does not exist yet")
	}
	if len(results[1].Errors) > 0 {
		t.Fatalf("the second Resolve missed a file created after the first, so a long-lived Context "+
			"served a stale directory listing: %v", results[1].Errors)
	}
}
