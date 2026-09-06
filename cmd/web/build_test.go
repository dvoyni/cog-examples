// Package web_test guards the browser build. The page itself is three
// generated files and a shell script, so there is nothing here to unit test;
// what there is to protect is the property the whole recipe rests on - that
// every demo cross-compiles under GOOS=js with no line of its own about the
// browser.
//
// It is a test rather than something build.sh checks because the failure it
// catches belongs to whoever wrote the demo, not to whoever runs the build: an
// import that only exists on desktop, or a call into internal/assets that
// assumed a filesystem, costs nothing on the desktop run and is only found the
// next time someone opens a browser. The web canary is one ticket away from
// depending on this, and between the two it would rot unwatched.
package web_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Every directory under cmd/scene builds for the browser. The list is walked
// rather than spelled out because the point is that a *new* demo works without
// anyone remembering this file exists.
func TestEveryDemoCrossCompilesForTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiling every demo takes a couple of seconds")
	}
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "cmd", "scene"))
	if err != nil {
		t.Fatalf("read cmd/scene: %v", err)
	}
	demos := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		demos++
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "main.wasm"),
				"./cmd/scene/"+entry.Name())
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("GOOS=js go build ./cmd/scene/%s: %v\n%s", entry.Name(), err, out)
			}
		})
	}
	if demos == 0 {
		t.Fatal("no demos under cmd/scene")
	}
}

// repoRoot is the module root, two directories above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	root := filepath.Join(working, "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod above %s: %v", working, err)
	}
	return root
}
