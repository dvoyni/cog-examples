// Package assets mounts the vendored demo asset set through storage.
//
// It exists because storage.DefaultConfig on its own does not reach it. The
// default read mount is the executable's own directory, and `go run` builds
// into a temporary directory, so a demo that configures nothing finds no
// models - and finds them by silently loading zero of them, which is the worst
// available failure mode for a set of demos whose whole subject is that a model
// which fails to load is skipped rather than substituted.
//
//	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
//
// Paths keep the assets/ prefix the repository uses, so a demo names a model
// exactly as ATTRIBUTION.md and the manifest do:
//
//	assets/Fox/Fox.glb
//	assets/broken/truncated.glb
//
// The mount is confined to the asset directory even so - the prefix is
// presented, not mounted - so nothing else in the checkout is readable through
// it.
package assets

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dvoyni/cog/storage"
)

const (
	// Mount is the storage mount id the asset set lands on.
	Mount storage.MountId = "assets"
	// Dir is the directory the set lives in, and the prefix every asset path
	// carries.
	Dir = "assets"
	// marker is the file that identifies a directory as this asset set rather
	// than some other directory called "assets".
	marker = "ATTRIBUTION.md"
	// EnvDir overrides the search, for a demo run from somewhere unusual.
	EnvDir = "COG_EXAMPLES_ASSETS"
)

// Config adds the vendored asset set to base as a read mount.
func Config(base storage.Config) (storage.Config, error) {
	dir, err := Locate()
	if err != nil {
		return base, err
	}
	return base.WithReadFS(Mount, storage.DefaultReadPriority, prefixFS{prefix: Dir, inner: os.DirFS(dir)}), nil
}

// Locate returns the directory holding the vendored asset set.
//
// It looks at COG_EXAMPLES_ASSETS first, then walks up from the working
// directory, then from the executable's directory - which covers `go run` from
// the module root, `go test` from a package directory, and a built binary
// shipped beside its assets. A directory only counts if it holds ATTRIBUTION.md,
// so an unrelated assets/ higher up the tree is not mistaken for this one.
func Locate() (string, error) {
	if dir := os.Getenv(EnvDir); dir != "" {
		if isAssetDir(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("assets: %s is set to %q, which holds no %s", EnvDir, dir, marker)
	}

	var searched []string
	for _, start := range startPoints() {
		searched = append(searched, start)
		for dir := start; ; {
			candidate := filepath.Join(dir, Dir)
			if isAssetDir(candidate) {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("assets: no %s/ holding %s above %s - run `go run ./cmd/prepare-assets`, or set %s",
		Dir, marker, strings.Join(searched, " or "), EnvDir)
}

func startPoints() []string {
	var starts []string
	if working, err := os.Getwd(); err == nil {
		starts = append(starts, working)
	}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		starts = append(starts, filepath.Dir(executable))
	}
	return starts
}

func isAssetDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, marker))
	return err == nil && !info.IsDir()
}

// prefixFS presents inner under one directory name. A mount confined to the
// asset directory still answers the assets/-prefixed paths the repository, the
// manifest and the credits all use, so there is one spelling of a model path
// rather than one per context.
type prefixFS struct {
	prefix string
	inner  fs.FS
}

func (p prefixFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	inner := ""
	switch {
	case name == p.prefix:
		inner = "."
	case strings.HasPrefix(name, p.prefix+"/"):
		inner = strings.TrimPrefix(name, p.prefix+"/")
	default:
		// Not ours. fs.ErrNotExist is what lets storage fall through to the next
		// mount rather than failing the read outright.
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return p.inner.Open(inner)
}

var _ fs.FS = prefixFS{}
