package ast

import "testing"

// A local CSS name has to name the same file the same way on every platform, because the class in
// the stylesheet and the class the page references are produced by different code - and, for
// consumers that rebuild these names outside esbuild, by a different language - from nothing but
// the path string. A Windows build used to hash `C:\a\x.module.css` while everything mirroring it
// hashed `C:/a/x.module.css`, so the two disagreed and the style silently did not apply.
//
// These run everywhere, not only on Windows: what is folded is decided from the path, so a Windows
// path can be passed as a literal on any host.
func TestCssLocalNamesFoldOnlyWindowsSeparators(t *testing.T) {
	const (
		drive     = "C:/Users/j/app/lib/x.module.css"
		driveBack = `C:\Users\j\app\lib\x.module.css`
		unc       = "//server/share/app/lib/x.module.css"
		uncBack   = `\\server\share\app\lib\x.module.css`
	)

	t.Run("hash folds a Windows absolute path", func(t *testing.T) {
		if got, want := CssLocalHash(driveBack), CssLocalHash(drive); got != want {
			t.Errorf("CssLocalHash(%q) = %q, want %q (the same file, spelled for Windows)",
				driveBack, got, want)
		}

		if got, want := CssLocalHash(uncBack), CssLocalHash(unc); got != want {
			t.Errorf("CssLocalHash(%q) = %q, want %q", uncBack, got, want)
		}

		// Different files still get different names. Without this the checks above would pass on
		// a hash that ignored its input.
		if CssLocalHash(drive) == CssLocalHash("C:/Users/j/app/lib/y.module.css") {
			t.Error("two different paths hashed alike")
		}
	})

	// Outside Windows a backslash is an ordinary file name character, so these are two different
	// files, and a mirror that hashes the path as written has to get the same answer.
	t.Run("hash leaves a backslash alone in a Unix path", func(t *testing.T) {
		const (
			slash     = "/app/a/b.module.css"
			backslash = `/app/a\b.module.css`
		)

		if CssLocalHash(backslash) == CssLocalHash(slash) {
			t.Errorf("CssLocalHash(%q) collided with %q, a different file", backslash, slash)
		}

		// sha1 of the bytes as written. Folding gave e0c1d546, the same as the slash path.
		if got, want := CssLocalHash(backslash), "621564d8"; got != want {
			t.Errorf("CssLocalHash(%q) = %q, want %q", backslash, got, want)
		}
	})

	t.Run("appendice drops the extension and folds separators", func(t *testing.T) {
		if got, want := CssLocalAppendice(driveBack), CssLocalAppendice(drive); got != want {
			t.Errorf("CssLocalAppendice(%q) = %q, want %q", driveBack, got, want)
		}

		if got, want := CssLocalAppendice("lib/x.module.css"), "lib-x-module"; got != want {
			t.Errorf("CssLocalAppendice = %q, want %q", got, want)
		}
	})
}
