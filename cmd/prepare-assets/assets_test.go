package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
)

// assetRoot is the vendored set, from this package's directory.
const assetRoot = "../../assets"

func openAsset(t *testing.T, name string) *gltf.Document {
	t.Helper()
	doc, err := gltf.Open(filepath.Join(assetRoot, name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return doc
}

// TestEveryOutputIsSelfContained is the claim the packing exists to make: what
// is committed is one file per asset, and a loader that is handed only that
// file needs nothing else. gltf.Open resolves siblings relative to the file, so
// a leftover URI would pass here and fail from a mount - which is why the URIs
// are asserted directly rather than inferred from the open succeeding.
func TestEveryOutputIsSelfContained(t *testing.T) {
	for _, a := range manifest {
		for _, output := range a.Outputs {
			name := filepath.Join(a.Name, output.File)
			doc := openAsset(t, name)
			if len(doc.Buffers) != 1 {
				t.Errorf("%s: %d buffers, want 1", name, len(doc.Buffers))
			}
			for i, buffer := range doc.Buffers {
				if buffer.URI != "" {
					t.Errorf("%s: buffer %d still points at %q", name, i, buffer.URI)
				}
			}
			for i, image := range doc.Images {
				if image.URI != "" {
					t.Errorf("%s: image %d still points at %q", name, i, image.URI)
				}
				if image.MimeType == "" {
					t.Errorf("%s: image %d has no media type, which glTF requires of a buffer-view image", name, i)
				}
			}
		}
	}
}

// TestTheTwoFailureCasesAreBothAvailable is what the loading demo needs to tell
// apart: one path fails before anything is read, the other fails only once the
// file is already open and being decoded. A model that fails is skipped, never
// substituted, and that contract has no demonstration without both.
func TestTheTwoFailureCasesAreBothAvailable(t *testing.T) {
	missing := filepath.Join(assetRoot, "broken", "does-not-exist.glb")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("%s should not exist: %v", missing, err)
	}

	truncated := filepath.Join(assetRoot, "broken", brokenSource.File)
	if _, err := os.Stat(truncated); err != nil {
		t.Fatalf("%s must exist - the asynchronous failure has no other source: %v", truncated, err)
	}
	if _, err := gltf.Open(truncated); err == nil {
		t.Fatal("the truncated file decoded successfully")
	}
}

// TestMeshPrimitiveModesCarriesEveryTopology is the only reason that asset is
// packed at all. No .glb anywhere in the Khronos repository holds a fan, strip,
// loop, line or point primitive; if this ever comes back triangles-only, the
// StripIndexFormat path has quietly become unreachable again.
func TestMeshPrimitiveModesCarriesEveryTopology(t *testing.T) {
	doc := openAsset(t, "MeshPrimitiveModes/MeshPrimitiveModes.glb")
	found := map[gltf.PrimitiveMode]bool{}
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			found[primitive.Mode] = true
		}
	}
	for _, mode := range []gltf.PrimitiveMode{
		gltf.PrimitivePoints, gltf.PrimitiveLines, gltf.PrimitiveLineLoop, gltf.PrimitiveLineStrip,
		gltf.PrimitiveTriangles, gltf.PrimitiveTriangleStrip, gltf.PrimitiveTriangleFan,
	} {
		if !found[mode] {
			t.Errorf("no %v primitive", mode)
		}
	}
}

// TestMultipleScenesIsStillTheOnlyMultiSceneFile guards the one asset that can
// exercise the Scene selector at all.
func TestMultipleScenesIsStillTheOnlyMultiSceneFile(t *testing.T) {
	doc := openAsset(t, "MultipleScenes/MultipleScenes.glb")
	if len(doc.Scenes) != 2 {
		t.Fatalf("scenes = %d, want 2", len(doc.Scenes))
	}
	for _, a := range manifest {
		for _, output := range a.Outputs {
			if a.Name == "MultipleScenes" {
				continue
			}
			if other := openAsset(t, filepath.Join(a.Name, output.File)); len(other.Scenes) > 1 {
				t.Errorf("%s/%s has %d scenes too", a.Name, output.File, len(other.Scenes))
			}
		}
	}
}

// TestTheQuantizedCubeStillRequiresQuantization keeps the two AnimatedMorphCube
// outputs from collapsing into the same file. Only one of them is the
// KHR_mesh_quantization exercise, and it is the one upstream ships as .gltf.
func TestTheQuantizedCubeStillRequiresQuantization(t *testing.T) {
	quantized := openAsset(t, "AnimatedMorphCube/AnimatedMorphCube-Quantized.glb")
	if !contains(quantized.ExtensionsRequired, "KHR_mesh_quantization") {
		t.Errorf("extensionsRequired = %v, want KHR_mesh_quantization", quantized.ExtensionsRequired)
	}
	plain := openAsset(t, "AnimatedMorphCube/AnimatedMorphCube.glb")
	if len(plain.ExtensionsRequired) != 0 {
		t.Errorf("the plain cube requires %v, want nothing", plain.ExtensionsRequired)
	}
}

// TestInterpolationTestCoversThreeContractsAtOnce is why an 8 KB file is in a
// set otherwise chosen for what it renders: nine clips over the three
// interpolations, no skins key at all, and u8 indices.
func TestInterpolationTestCoversThreeContractsAtOnce(t *testing.T) {
	doc := openAsset(t, "InterpolationTest/InterpolationTest.glb")
	if len(doc.Animations) != 9 {
		t.Errorf("animations = %d, want 9", len(doc.Animations))
	}
	if len(doc.Skins) != 0 {
		t.Errorf("skins = %d, want none - this is the asset that animates nodes that are not joints", len(doc.Skins))
	}
	interpolations := map[gltf.Interpolation]bool{}
	for _, animation := range doc.Animations {
		for _, sampler := range animation.Samplers {
			interpolations[sampler.Interpolation] = true
		}
	}
	for _, want := range []gltf.Interpolation{gltf.InterpolationStep, gltf.InterpolationLinear, gltf.InterpolationCubicSpline} {
		if !interpolations[want] {
			t.Errorf("no %v sampler", want)
		}
	}
	if !hasIndexType(doc, gltf.ComponentUbyte) {
		t.Error("no u8 indices - the widening gap loses its asset")
	}
}

// TestFoxCarriesThreeClipsOverOneSkin is the crossfade's whole basis, and the
// reason CesiumMan was dropped.
func TestFoxCarriesThreeClipsOverOneSkin(t *testing.T) {
	doc := openAsset(t, "Fox/Fox.glb")
	if len(doc.Animations) != 3 {
		t.Errorf("animations = %d, want 3 - one clip cannot demonstrate a crossfade", len(doc.Animations))
	}
	if len(doc.Skins) != 1 || len(doc.Skins[0].Joints) != 24 {
		t.Errorf("skins = %d, joints = %d, want 1 skin over 24 joints", len(doc.Skins), len(doc.Skins[0].Joints))
	}
}

func hasIndexType(doc *gltf.Document, component gltf.ComponentType) bool {
	for _, mesh := range doc.Meshes {
		for _, primitive := range mesh.Primitives {
			if primitive.Indices != nil && doc.Accessors[*primitive.Indices].ComponentType == component {
				return true
			}
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestAttributionCoversTheVendoredSet checks the committed file rather than the
// renderer: a stale ATTRIBUTION.md is a licensing failure, not a formatting one.
func TestAttributionCoversTheVendoredSet(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(assetRoot, attributionFile))
	if err != nil {
		t.Fatalf("read %s: %v", attributionFile, err)
	}
	if got := renderAttribution(manifest); string(committed) != got {
		t.Errorf("%s is stale - re-run `go run ./cmd/prepare-assets`", attributionFile)
	}
	text := string(committed)
	for _, a := range manifest {
		if !strings.Contains(text, a.Title) {
			t.Errorf("%s does not mention %s", attributionFile, a.Title)
		}
		if !strings.Contains(text, a.Commit) {
			t.Errorf("%s does not carry %s's source commit", attributionFile, a.Name)
		}
	}
}
