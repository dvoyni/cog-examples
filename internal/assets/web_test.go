//go:build js

package assets

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"syscall/js"
	"testing"
)

// These are the node run's whole view of the asset set. The headless demo tests
// are built for the desktop only, since under GOOS=js they would repeat the
// desktop run over the same fake Backend; what is left that only js can check is
// this: the preloaded map index.html leaves behind, read as a mount.

// preload leaves files on Global the way cmd/web/index.html does: a Map from
// the path inside the tar to a Uint8Array of the file's bytes.
func preload(t *testing.T, files map[string]string) {
	t.Helper()
	bundle := js.Global().Get("Map").New()
	for name, data := range files {
		bytes := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(bytes, []byte(data))
		bundle.Call("set", name, bytes)
	}
	js.Global().Set(Global, bundle)
	t.Cleanup(func() { js.Global().Delete(Global) })
}

func TestThePreloadedBundleReadsAsTheAssetMount(t *testing.T) {
	preload(t, map[string]string{
		path.Join(Dir, marker):  "attribution",
		"assets/Fox/Fox.glb":    "glTF bytes",
		"assets/broken/nil.glb": "",
	})
	mount, err := Mount()
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if mount.Id != MountId {
		t.Errorf("Id = %q, want %q", mount.Id, MountId)
	}

	data, err := fs.ReadFile(mount.FS, "assets/Fox/Fox.glb")
	if err != nil || string(data) != "glTF bytes" {
		t.Errorf("ReadFile(assets/Fox/Fox.glb) = %q, %v; want the preloaded bytes", data, err)
	}
	info, err := fs.Stat(mount.FS, "assets/Fox/Fox.glb")
	if err != nil || info.Name() != "Fox.glb" || info.Size() != int64(len("glTF bytes")) || info.IsDir() {
		t.Errorf("Stat(assets/Fox/Fox.glb) = %v, %v; want the file Fox.glb of %d bytes", info, err, len("glTF bytes"))
	}
	empty, err := fs.ReadFile(mount.FS, "assets/broken/nil.glb")
	if err != nil || len(empty) != 0 {
		t.Errorf("ReadFile(assets/broken/nil.glb) = %q, %v; want an empty file", empty, err)
	}

	// storage falls through to the next mount only on fs.ErrNotExist.
	if _, err := mount.FS.Open("assets/Fox/Missing.glb"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open of an absent file = %v, want fs.ErrNotExist", err)
	}
	if _, err := mount.FS.Open("../assets/Fox/Fox.glb"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Open of an invalid path = %v, want fs.ErrInvalid", err)
	}
}

func TestAFileReadsInPieces(t *testing.T) {
	preload(t, map[string]string{
		path.Join(Dir, marker): "attribution",
		"assets/a.bin":         "0123456789",
	})
	mount, err := Mount()
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	file, err := mount.FS.Open("assets/a.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(file, head); err != nil || string(head) != "0123" {
		t.Fatalf("first read = %q, %v; want 0123", head, err)
	}
	rest, err := io.ReadAll(file)
	if err != nil || string(rest) != "456789" {
		t.Errorf("rest = %q, %v; want 456789", rest, err)
	}
}

// Mount takes the bundle once and deletes the global, so the page's copy can be
// released; a second Mount therefore fails rather than mounting nothing.
func TestMountTakesTheBundleOnce(t *testing.T) {
	preload(t, map[string]string{path.Join(Dir, marker): "attribution"})
	if _, err := Mount(); err != nil {
		t.Fatalf("first Mount: %v", err)
	}
	if !js.Global().Get(Global).IsUndefined() {
		t.Errorf("%s is still set after Mount", Global)
	}
	if _, err := Mount(); err == nil {
		t.Error("second Mount succeeded, want the not-preloaded error")
	}
}

func TestNoPreloadIsAnError(t *testing.T) {
	js.Global().Delete(Global)
	if _, err := Mount(); err == nil {
		t.Error("Mount with nothing preloaded succeeded")
	}
}

// A bundle without the marker was packed from the wrong directory, and a demo
// reading it would find none of its models.
func TestABundleWithoutTheMarkerIsAnError(t *testing.T) {
	preload(t, map[string]string{"Fox/Fox.glb": "glTF bytes"})
	if _, err := Mount(); err == nil {
		t.Error("Mount of a bundle with no marker succeeded")
	}
}
