package ast

import "testing"

// A local CSS name has to name the same file the same way on every platform, because the class in
// the stylesheet and the class the page references are produced by different code - and, for
// consumers that rebuild these names outside esbuild, by a different language - from nothing but
// the path string. A Windows build used to hash "a\b\x.module.css" while everything mirroring it
// hashed "a/b/x.module.css", so the two disagreed and the style silently did not apply.
//
// These run everywhere, not only on Windows: the normalisation is unconditional, so a backslash
// path can be passed as a literal on any host.
func TestCssLocalNamesAreSeparatorIndependent(t *testing.T) {
	const (
		slash     = "/Users/j/app/lib/x.module.css"
		backslash = `\Users\j\app\lib\x.module.css`
		drive     = "C:/Users/j/app/lib/x.module.css"
		driveBack = `C:\Users\j\app\lib\x.module.css`
	)

	t.Run("hash", func(t *testing.T) {
		if got, want := CssLocalHash(backslash), CssLocalHash(slash); got != want {
			t.Errorf("CssLocalHash(%q) = %q, want %q (the same file, spelled for Windows)",
				backslash, got, want)
		}

		if got, want := CssLocalHash(driveBack), CssLocalHash(drive); got != want {
			t.Errorf("CssLocalHash(%q) = %q, want %q", driveBack, got, want)
		}

		// Different files still get different names. Without this the test above would pass on a
		// hash that ignored its input.
		if CssLocalHash(slash) == CssLocalHash("/Users/j/app/lib/y.module.css") {
			t.Error("two different paths hashed alike")
		}
	})

	t.Run("appendice", func(t *testing.T) {
		if got, want := CssLocalAppendice(backslash), CssLocalAppendice(slash); got != want {
			t.Errorf("CssLocalAppendice(%q) = %q, want %q", backslash, got, want)
		}

		if got, want := CssLocalAppendice(driveBack), CssLocalAppendice(drive); got != want {
			t.Errorf("CssLocalAppendice(%q) = %q, want %q", driveBack, got, want)
		}
	})

	// The extension is dropped by a "path" package that only knows "/", so a Windows path has to
	// reach it already normalised. Reading this against a raw backslash path would find no
	// directory separator, and ".module.css" would still be trimmed - but "lib\x" would not, so
	// assert the whole answer rather than just that the two agree.
	t.Run("appendice drops the extension and folds separators", func(t *testing.T) {
		if got, want := CssLocalAppendice(backslash), "-Users-j-app-lib-x-module"; got != want {
			t.Errorf("CssLocalAppendice(%q) = %q, want %q", backslash, got, want)
		}
	})
}
