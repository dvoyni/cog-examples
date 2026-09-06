package assets

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/dvoyni/cog/storage"
)

// TestTheVendoredSetIsReachable is this package's whole reason to exist: a
// demo configuring nothing but the app id reads no models at all, and does it
// without an error.
func TestTheVendoredSetIsReachable(t *testing.T) {
	config, err := Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	var mounted fs.FS
	for _, mount := range config.ReadMounts {
		if mount.Id == Mount {
			mounted = mount.FS
		}
	}
	if mounted == nil {
		t.Fatalf("no %q mount in %v", Mount, config.ReadMounts)
	}

	for _, name := range []string{
		"assets/Fox/Fox.glb",
		"assets/WaterBottle/WaterBottle.glb",
		"assets/AnimatedMorphCube/AnimatedMorphCube-Quantized.glb",
		"assets/broken/truncated.glb",
		"assets/ATTRIBUTION.md",
	} {
		data, err := fs.ReadFile(mounted, name)
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

// TestAPathThatDoesNotExistFailsAsNotExist keeps the synchronous half of the
// loading demo's failure pair working: a mount that answered some other error
// would stop storage falling through to the next one.
func TestAPathThatDoesNotExistFailsAsNotExist(t *testing.T) {
	config, err := Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	var mounted fs.FS
	for _, mount := range config.ReadMounts {
		if mount.Id == Mount {
			mounted = mount.FS
		}
	}
	for _, name := range []string{"assets/broken/does-not-exist.glb", "assets/NoSuchModel/x.glb"} {
		if _, err := fs.ReadFile(mounted, name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("read %s: %v, want fs.ErrNotExist", name, err)
		}
	}
}

// TestTheMountIsConfinedToTheAssetDirectory is why the prefix is presented
// rather than the module root being mounted.
func TestTheMountIsConfinedToTheAssetDirectory(t *testing.T) {
	confined := prefixFS{prefix: Dir, inner: fstestDir{}}
	for _, name := range []string{"go.mod", "cmd/prepare-assets/main.go", "README.md"} {
		if _, err := confined.Open(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("open %s: %v, want fs.ErrNotExist", name, err)
		}
	}
	if _, err := confined.Open("assets/anything"); errors.Is(err, fs.ErrInvalid) {
		t.Error("a path under the prefix should reach the inner filesystem")
	}
}

// fstestDir answers every open, so the test above measures only what prefixFS
// forwards.
type fstestDir struct{}

func (fstestDir) Open(string) (fs.File, error) { return nil, nil }
