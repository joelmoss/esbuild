package api_test

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// A panic in a plugin callback used to abort the process that called Build(): esbuild runs the
// callbacks on its own goroutines, and a recover anywhere else cannot see them. Each callback
// wrapper now recovers into a build error - short text, stack in a note - and the build fails the
// way a returned error fails it. These tests pin that for every callback type, for a callback
// reached through build.Resolve, and that the process is still usable afterwards. Before the
// recover existed, the OnLoad test below took the whole test binary down with it.

const panicSentinel = "sentinel-from-plugin"

// An entry importing one dependency, so every callback type has something to fire on.
func panicFixture(t *testing.T) (dir string, entry string) {
	dir = t.TempDir()
	entry = path.Join(dir, "entry.js")
	if err := os.WriteFile(entry, []byte("import './dep.js'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path.Join(dir, "dep.js"), []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, entry
}

func panicOptions(dir string, entry string, plugins ...api.Plugin) api.BuildOptions {
	return api.BuildOptions{
		EntryPoints:   []string{entry},
		AbsWorkingDir: dir,
		Bundle:        true,
		Write:         false,
		LogLevel:      api.LogLevelSilent,
		Plugins:       plugins,
	}
}

func assertPanicMessage(t *testing.T, msgs []api.Message, callback string) {
	t.Helper()

	if len(msgs) != 1 {
		t.Fatalf("expected exactly one error, got %d: %v", len(msgs), msgs)
	}

	e := msgs[0]
	want := "panic: " + panicSentinel + " (in " + callback + " callback)"
	if e.Text != want {
		t.Errorf("Text = %q, want %q", e.Text, want)
	}
	if e.PluginName != "panicking" {
		t.Errorf("PluginName = %q, want %q", e.PluginName, "panicking")
	}
	if len(e.Notes) == 0 || !strings.Contains(e.Notes[0].Text, "api_plugin_panic_test.go") {
		t.Errorf("expected a stack note naming this file, got %v", e.Notes)
	}
}

func TestOnStartPanicBecomesBuildError(t *testing.T) {
	dir, entry := panicFixture(t)

	result := api.Build(panicOptions(dir, entry, api.Plugin{
		Name: "panicking",
		Setup: func(build api.PluginBuild) {
			build.OnStart(func() (api.OnStartResult, error) {
				panic(panicSentinel)
			})
		},
	}))

	assertPanicMessage(t, result.Errors, "OnStart")
}

// The entry-point resolve runs on a goroutine of its own, outside parseFile's recover.
func TestOnResolvePanicBecomesBuildError(t *testing.T) {
	dir, entry := panicFixture(t)

	result := api.Build(panicOptions(dir, entry, api.Plugin{
		Name: "panicking",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind == api.ResolveEntryPoint {
					panic(panicSentinel)
				}
				return api.OnResolveResult{}, nil
			})
		},
	}))

	assertPanicMessage(t, result.Errors, "OnResolve")
}

// parseFile calls the load plugins before it installs its own recover, so this one used to
// abort the process.
func TestOnLoadPanicBecomesBuildError(t *testing.T) {
	dir, entry := panicFixture(t)

	result := api.Build(panicOptions(dir, entry, api.Plugin{
		Name: "panicking",
		Setup: func(build api.PluginBuild) {
			build.OnLoad(api.OnLoadOptions{Filter: `dep\.js$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				panic(panicSentinel)
			})
		},
	}))

	assertPanicMessage(t, result.Errors, "OnLoad")

	if len(result.Errors) == 1 && result.Errors[0].Location == nil {
		t.Errorf("expected the error to point at the importing file")
	}
}

// A callback reached through build.Resolve hands its panic back to the caller as an ordinary
// resolve error, so a plugin that treats resolve errors as "not found" needs to look at the text.
func TestNestedResolvePanicIsReturnedToTheCaller(t *testing.T) {
	dir, entry := panicFixture(t)
	var nested api.ResolveResult

	result := api.Build(panicOptions(dir, entry, api.Plugin{
		Name: "panicking",
		Setup: func(build api.PluginBuild) {
			build.OnStart(func() (api.OnStartResult, error) {
				nested = build.Resolve("./dep.js", api.ResolveOptions{
					Kind:       api.ResolveJSImportStatement,
					ResolveDir: dir,
					PluginData: "nested",
				})
				return api.OnStartResult{}, nil
			})

			build.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.PluginData == "nested" {
					panic(panicSentinel)
				}
				return api.OnResolveResult{}, nil
			})
		},
	}))

	assertPanicMessage(t, nested.Errors, "OnResolve")

	if len(result.Errors) != 0 {
		t.Errorf("the outer build should not fail on its own: %v", result.Errors)
	}
}

// The recover leaves nothing behind: a plain build in the same process still works.
func TestBuildAfterRecoveredPanicSucceeds(t *testing.T) {
	dir, entry := panicFixture(t)

	failed := api.Build(panicOptions(dir, entry, api.Plugin{
		Name: "panicking",
		Setup: func(build api.PluginBuild) {
			build.OnLoad(api.OnLoadOptions{Filter: `dep\.js$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				panic(panicSentinel)
			})
		},
	}))
	if len(failed.Errors) != 1 {
		t.Fatalf("expected the panicking build to fail once, got %v", failed.Errors)
	}

	ok := api.Build(panicOptions(dir, entry))
	if len(ok.Errors) != 0 {
		t.Errorf("a build after a recovered panic should succeed: %v", ok.Errors)
	}
}
