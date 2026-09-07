package assets_test

import (
	"testing"

	"github.com/dvoyni/cog/scene"
)

// The vendored files the morph path is judged against.
//
// MorphStressTest is the real one: eight named shapes on a two-primitive mesh,
// three weights-only clips, and a target list an exporter wrote rather than a
// test. AnimatedMorphCube is the mask case - it authors a tangent and its
// quantized twin does not, so the two files disagree about a record's stride
// while describing the same cube.
const (
	morphStressAsset    = "assets/MorphStressTest/MorphStressTest.glb"
	morphCubeAsset      = "assets/AnimatedMorphCube/AnimatedMorphCube.glb"
	morphCubeQuantized  = "assets/AnimatedMorphCube/AnimatedMorphCube-Quantized.glb"
	morphStressVertices = 24 + 1504
)

// morphTargetsOf reads one resident model's flattened target list.
func morphTargetsOf(t *testing.T, path string, draw scene.ModelDraw) ([]string, int) {
	t.Helper()
	e := drawing(t, path, draw)
	var names []string
	var bytes int
	var ok bool
	e.Lookup(func(la scene.LookupAccess) {
		names, ok = la.MorphTargets(path, nil)
		bytes, _ = la.MorphBytes(path)
	})
	if !ok {
		t.Fatalf("%s is not resident, so it has no targets to report", path)
	}
	return names, bytes
}

// Target names live in the mesh's extras rather than anywhere the format
// reserves for them, which is a convention rather than a schema - so the one
// thing worth pinning is a file an exporter actually wrote.
func TestMorphStressTestDeclaresItsEightNamedShapes(t *testing.T) {
	names, bytes := morphTargetsOf(t, morphStressAsset, scene.ModelDraw{})
	want := []string{"Key 1", "Key 2", "Key 3", "Key 4", "Key 5", "Key 6", "Key 7", "Key 8"}
	if len(names) != len(want) {
		t.Fatalf("MorphTargets = %v, want the file's eight shapes", names)
	}
	for i, expected := range want {
		if names[i] != expected {
			t.Errorf("target %d is %q, want %q", i, names[i], expected)
		}
	}
	// One node, so one run of eight slots - not eight per primitive. glTF
	// requires every primitive of a mesh to carry the same targets in the same
	// order, which is why a two-primitive eight-shape mesh is eight slots.
	//
	// The deltas are the other half: both primitives carry POSITION and NORMAL
	// deltas over an authored NORMAL, so a record is two slots of 16 bytes and
	// both primitives' targets concatenate into the model's one buffer.
	if want := morphStressVertices * 2 * 16 * len(names); bytes != want {
		t.Errorf("MorphBytes = %d, want %d: two slots a vertex across both primitives", bytes, want)
	}
}

// The mask is intersected with what the base primitive authored, and these two
// files are the same cube disagreeing about exactly that: the plain one authors
// a tangent and carries tangent deltas, the quantized one authors neither. A
// mask taken from the targets alone would give both the same stride.
func TestTheMorphCubeAndItsQuantizedTwinDifferByTheirAuthoredTangent(t *testing.T) {
	plain, plainBytes := morphTargetsOf(t, morphCubeAsset, scene.ModelDraw{})
	quantized, quantizedBytes := morphTargetsOf(t, morphCubeQuantized, scene.ModelDraw{})
	// Neither file names its shapes, and an unnamed target is an empty string
	// rather than a gap: the list is indexed by slot, not searched.
	if len(plain) != 2 || len(quantized) != 2 {
		t.Fatalf("targets = %d and %d, want the cube's two either way", len(plain), len(quantized))
	}
	// 24 vertices, two targets: three slots a record against two.
	if want := 24 * 3 * 16 * 2; plainBytes != want {
		t.Errorf("MorphBytes = %d, want %d for position, normal and tangent", plainBytes, want)
	}
	if want := 24 * 2 * 16 * 2; quantizedBytes != want {
		t.Errorf("quantized MorphBytes = %d, want %d for position and normal", quantizedBytes, want)
	}
}

// The quantized cube's deltas are SHORT positions and BYTE normals, which is
// what KHR_mesh_quantization does to a morph target as well as to a vertex.
// They go through the same decode every attribute does, so the file draws
// rather than failing on a component type the core specification forbids there.
func TestTheQuantizedMorphCubeLoadsAndDraws(t *testing.T) {
	e := drawing(t, morphCubeQuantized, scene.ModelDraw{})
	if batches(t, e) == 0 {
		t.Error("the quantized morph cube drew nothing")
	}
	if errs := e.Errors(); len(errs) != 0 {
		t.Errorf("a quantized file reported %v", errs)
	}
}

// A weights channel is a clip like any other: it produces no joint, so the
// model bakes an empty pose buffer, and it still plays by name.
func TestMorphStressTestPlaysItsWeightsOnlyClips(t *testing.T) {
	e := drawing(t, morphStressAsset, scene.ModelDraw{})
	clips := clipsOf(t, e, morphStressAsset)
	names := map[string]float32{}
	for _, clip := range clips {
		names[clip.Name] = clip.Duration
	}
	for _, want := range []string{"Individuals", "TheWave", "Pulse"} {
		duration, ok := names[want]
		if !ok {
			t.Errorf("MorphStressTest declares no clip %q; it has %v", want, clips)
			continue
		}
		if duration <= 0 {
			t.Errorf("clip %q lasts %v seconds, want a real length", want, duration)
		}
	}
	if len(jointsOf(t, e, morphStressAsset)) != 0 {
		t.Error("a weights-only file baked joints; morph weights reshape a mesh and leave the node")
	}
	var poses int
	e.Lookup(func(la scene.LookupAccess) { poses, _ = la.PoseBytes(morphStressAsset) })
	if poses != 0 {
		t.Errorf("PoseBytes = %d, want none for a file with no rig", poses)
	}
	// Playing one still draws both primitives as one batch each: a clip changes
	// the weights an instance carries, not how many draws there are.
	playing := drawing(t, morphStressAsset, scene.ModelDraw{
		Plays: []scene.ClipPlay{{Clip: "TheWave", Time: 0.9, Loop: true, Weight: 1}},
	})
	if got, want := batches(t, playing), batches(t, e); got != want {
		t.Errorf("a playing file drew %d batches and a still one %d; a clip is not a draw",
			got, want)
	}
	if errs := playing.Errors(); len(errs) != 0 {
		t.Errorf("playing one of the file's own clips reported %v", errs)
	}
}

// MorphWeights is positional over the flattened list, so an array that outlives
// an edit to the file is the failure worth naming. The tail is ignored, the
// model still draws, and it reports once rather than once a frame.
func TestAnOverLongMorphWeightsReportsOnceAndStillDraws(t *testing.T) {
	weights := make([]float32, 12)
	weights[0] = 1
	e := drawing(t, morphStressAsset, scene.ModelDraw{MorphWeights: weights})
	e.Steps(3)
	over := 0
	for _, err := range e.Errors() {
		if _, ok := err.(scene.ErrModelMorphWeightsOverLength); ok {
			over++
		}
	}
	if over != 1 {
		t.Errorf("reported %d over-length weight arrays, want one: %v", over, e.Errors())
	}
	if batches(t, e) == 0 {
		t.Error("the model vanished; an over-long array costs the tail, not the model")
	}
}
