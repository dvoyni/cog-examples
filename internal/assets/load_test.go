package assets_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// The vendored set, by the path a demo names. Every entry is expected to reach
// residency; broken/truncated.glb is expected to fail, and is asserted
// separately.
//
// The list is spelled out rather than walked so that an asset dropped from the
// set fails this test instead of silently shrinking it - which is exactly the
// failure the loading demo exists to catch and the one a `range dir` would hide.
var vendored = []string{
	"assets/AlphaBlendModeTest/AlphaBlendModeTest.glb",
	"assets/AnimatedMorphCube/AnimatedMorphCube.glb",
	"assets/AnimatedMorphCube/AnimatedMorphCube-Quantized.glb",
	"assets/BoxVertexColors/BoxVertexColors.glb",
	"assets/CesiumMilkTruck/CesiumMilkTruck.glb",
	"assets/CompareBaseColor/CompareBaseColor.glb",
	"assets/EmissiveStrengthTest/EmissiveStrengthTest.glb",
	"assets/Fox/Fox.glb",
	"assets/InterpolationTest/InterpolationTest.glb",
	"assets/MeshPrimitiveModes/MeshPrimitiveModes.glb",
	"assets/MorphStressTest/MorphStressTest.glb",
	"assets/MultipleScenes/MultipleScenes.glb",
	"assets/PointLightIntensityTest/PointLightIntensityTest.glb",
	"assets/TextureSettingsTest/TextureSettingsTest.glb",
	"assets/WaterBottle/WaterBottle.glb",
}

const brokenAsset = "assets/broken/truncated.glb"

// engine starts a headless engine with the vendored set mounted.
func engine(t *testing.T) *headless.Engine {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	return headless.NewOver(t, config)
}

// Every vendored asset loads. This is the assertion the loader is actually
// judged by: the unit tests in cog/scene build documents in memory and can only
// exercise the shapes their author thought of, where the Khronos set carries
// u8 indices, a non-indexed primitive, all seven topologies, quantised meshes,
// nine textures over three images, and an extension nobody implemented.
func TestEveryVendoredAssetBecomesResident(t *testing.T) {
	e := engine(t)
	preload(t, e, vendored...)
	for _, path := range vendored {
		t.Run(path, func(t *testing.T) {
			if !resident(t, e, path) {
				t.Fatalf("%s never became resident; engine reported %v", path, e.Errors())
			}
		})
	}
}

// The one asset that must not load. A truncated file has no geometry to fall
// back to, so it fails wholesale rather than becoming a resident model with
// nothing in it.
func TestTheTruncatedAssetFailsWholesale(t *testing.T) {
	e := engine(t)
	preload(t, e, brokenAsset)
	var unavailable scene.ErrModelUnavailable
	deadline := time.Now().Add(10 * time.Second)
	for !anyErrorAs(e.Errors(), &unavailable) {
		if time.Now().After(deadline) {
			t.Fatalf("errors = %v, want a model-unavailable report", e.Errors())
		}
		e.Steps(1)
		time.Sleep(time.Millisecond)
	}
	if residentNow(t, e, brokenAsset) {
		t.Fatal("a truncated file must not become resident")
	}
	if unavailable.Model != brokenAsset {
		t.Errorf("report names %q, want %q", unavailable.Model, brokenAsset)
	}
}

// Loading the whole set reports nothing beyond what the set is known to
// contain. The known reports are named rather than counted, so a new one shows
// up as a failure with its own text rather than as an off-by-one.
func TestTheVendoredSetLoadsWithOnlyItsKnownReports(t *testing.T) {
	e := engine(t)
	preload(t, e, vendored...)
	for _, path := range vendored {
		if !resident(t, e, path) {
			t.Fatalf("%s never became resident", path)
		}
	}
	for _, err := range e.Errors() {
		var skipped scene.ErrModelPrimitiveSkipped
		if errors.As(err, &skipped) {
			// MeshPrimitiveModes is the only route to POINTS anywhere in the
			// Khronos repository, and gfx carries no point topology: triangle
			// list, triangle strip and line list, and nothing else.
			if skipped.Model == "assets/MeshPrimitiveModes/MeshPrimitiveModes.glb" {
				continue
			}
		}
		t.Errorf("unexpected report: %v", err)
	}
}

// PointLightIntensityTest is the set's only file with KHR_lights_punctual, and
// the lights reach an app as data: nothing in scene converts one.
func TestPunctualLightsReachTheAppAsData(t *testing.T) {
	const path = "assets/PointLightIntensityTest/PointLightIntensityTest.glb"
	e := engine(t)
	preload(t, e, path)
	if !resident(t, e, path) {
		t.Fatalf("%s never became resident", path)
	}
	var lights []scene.ModelLight
	e.Lookup(func(la scene.LookupAccess) { lights, _ = la.ModelLights(path, nil) })
	if len(lights) == 0 {
		t.Fatal("the file declares punctual lights; none reached the app")
	}
	for _, light := range lights {
		if light.Descr.Intensity <= 0 {
			t.Errorf("light %q has intensity %v", light.Name, light.Descr.Intensity)
		}
	}
}

// preload fires the load every query and every draw fires, so the set is in
// flight before anything waits on it.
func preload(t *testing.T, e *headless.Engine, paths ...string) {
	t.Helper()
	e.Lookup(func(la scene.LookupAccess) {
		for _, path := range paths {
			la.Preload(path)
		}
	})
}

// resident steps the engine until path is resident or the deadline passes.
//
// The wait is wall clock rather than a frame count because the whole point of
// the design is that a load does not run on the frame's thread: decoding
// WaterBottle's five 1024px textures takes longer than a few hundred headless
// frames of doing nothing else, which is exactly the hitch the asynchronous
// path exists to keep out of the frame.
func resident(t *testing.T, e *headless.Engine, path string) bool {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if residentNow(t, e, path) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		e.Steps(1)
		time.Sleep(time.Millisecond)
	}
}

// residentNow asks whether path is resident right now. ModelLights is the
// predicate because its ok is exactly "this value is real", which is false for
// a missing, loading or failed path alike.
func residentNow(t *testing.T, e *headless.Engine, path string) bool {
	t.Helper()
	var ok bool
	e.Lookup(func(la scene.LookupAccess) { _, ok = la.ModelLights(path, nil) })
	return ok
}

func anyErrorAs(reported []error, target any) bool {
	for _, err := range reported {
		if errors.As(err, target) {
			return true
		}
	}
	return false
}

// TextureSettingsTest is three images backing nine textures through five
// samplers, and it is the only honest exercise of the texture cache in the set:
// every other model owns a private directory and no two files ever resolve to
// the same key. Nine uploads would mean the cache is not deduping at all.
func TestTheTextureCacheDedupesImagesWithinAModel(t *testing.T) {
	const path = "assets/TextureSettingsTest/TextureSettingsTest.glb"
	e := engine(t)
	preload(t, e, path)
	if !resident(t, e, path) {
		t.Fatalf("%s never became resident", path)
	}
	// Plus the two 1x1 defaults every empty slot binds, which are baked once
	// for the whole engine.
	const defaults = 2
	if baked := e.Backend().BakedTextures; baked > 3+defaults {
		t.Fatalf("baked %d textures for three images and nine glTF textures, want at most %d",
			baked, 3+defaults)
	}
}
