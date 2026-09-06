//go:build js

package assets

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"syscall/js"
	"time"

	"github.com/dvoyni/cog/storage"
)

// Global is the JavaScript global cmd/web/index.html leaves the unpacked asset
// bundle on, a Map keyed by the path inside the tar. The tar is built from the
// repository root over the assets directory, so its keys already carry the
// assets/ prefix and no wrapper is needed to present one - which is the whole
// reason the disk mount presents the prefix rather than mounting it.
const Global = "__cogAssets"

// Config adds the preloaded asset bundle to base as a read mount.
//
// The bundle is taken once and the global deleted, so the browser can release
// the copy the page decompressed into as soon as the map is unreachable from
// JavaScript. It is a hard failure rather than a warning: a demo with no assets
// renders an empty room and blames the loader, which is exactly the failure the
// loading demo exists to catch.
func Config(base storage.Config) (storage.Config, error) {
	files := js.Global().Get(Global)
	if files.Type() != js.TypeObject {
		return base, fmt.Errorf(
			"assets: %s was not preloaded - cmd/web/index.html unpacks assets.tar.gz into it before the module boots",
			Global)
	}
	js.Global().Delete(Global)
	bundle := webFS{files: files}
	if _, err := fs.Stat(bundle, path.Join(Dir, marker)); err != nil {
		return base, fmt.Errorf("assets: the bundle holds no %s/%s, so it was built from the wrong directory", Dir, marker)
	}
	return base.WithReadFS(Mount, storage.DefaultReadPriority, bundle), nil
}

// Locate has no answer under GOOS=js. It is kept so the package presents one
// surface on both platforms, and it says why rather than returning a path that
// cannot be opened.
func Locate() (string, error) {
	return "", errors.New("assets: there is no directory to locate in a browser; the set arrives as the preloaded bundle")
}

// webFS reads the unpacked tar. It is flat by construction - the unpacker keeps
// regular files only - so it answers Open and nothing else, which is all storage
// and the glTF decoder ask of a read mount.
type webFS struct {
	files js.Value
}

func (w webFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	value := w.files.Call("get", name)
	if value.IsUndefined() {
		// fs.ErrNotExist is what lets storage fall through to the next mount
		// rather than failing the read outright.
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	data := make([]byte, value.Get("byteLength").Int())
	if copied := js.CopyBytesToGo(data, value); copied != len(data) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return &webFile{Reader: bytes.NewReader(data), name: name}, nil
}

type webFile struct {
	*bytes.Reader
	name string
}

func (f *webFile) Close() error { return nil }

func (f *webFile) Stat() (fs.FileInfo, error) {
	return webFileInfo{name: path.Base(f.name), size: f.Size()}, nil
}

type webFileInfo struct {
	name string
	size int64
}

func (i webFileInfo) Name() string       { return i.name }
func (i webFileInfo) Size() int64        { return i.size }
func (i webFileInfo) Mode() fs.FileMode  { return 0444 }
func (i webFileInfo) ModTime() time.Time { return time.Time{} }
func (i webFileInfo) IsDir() bool        { return false }
func (i webFileInfo) Sys() any           { return nil }

var _ fs.FS = webFS{}
