package assets_test

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
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
	e.LookupDevice(func(la model.LookupDeviceAccess) {
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
	// The deltas are the other half, and they are stored sparse: a block is a
	// range per slot and a base/first/count per target, then only the records
	// inside each target's live span. Both primitives carry POSITION and NORMAL
	// deltas over an authored NORMAL, so a record is three words.
	//
	// This file is the case sparsity was for. Primitive 0 is eight targets that
	// are entirely zero - it stores nothing but its header. Primitive 1's eight
	// targets each touch 187 of its 1,504 vertices, so it stores 187 records
	// rather than 1,504. Dense, the two came to 391,168 bytes.
	const (
		headerWords = 2*3 + 8*3 // two slots' ranges, eight targets' headers
		liveSpan    = 187       // primitive 1; primitive 0's targets are all zero
		recordWords = 3         // position and normal
	)
	wantBytes := 4 * (headerWords + (headerWords + 8*liveSpan*recordWords))
	if bytes != wantBytes {
		t.Errorf("MorphBytes = %d, want %d: a header a primitive, records only where a target reaches", bytes, wantBytes)
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
	// 24 vertices, two targets: three slots a record against two, and a record
	// is one word a slot plus one more for the position's second half.
	//
	// The live counts are the sparsity, and they are not what the stride would
	// suggest: the plain cube stores 33 records between its two targets and the
	// quantized twin 36. The quantized file's deltas are SHORT and BYTE, so
	// fewer of them land on exactly zero and its spans are wider - it stores
	// more records in narrower slots and still comes out smaller. Dense, the two
	// were 2,304 and 1,536 bytes.
	if want := 4 * (3*3 + 2*3 + 33*4); plainBytes != want {
		t.Errorf("MorphBytes = %d, want %d for position, normal and tangent", plainBytes, want)
	}
	if want := 4 * (2*3 + 2*3 + 36*3); quantizedBytes != want {
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
	e.LookupDevice(func(la model.LookupDeviceAccess) { poses, _ = la.PoseBytes(morphStressAsset) })
	if poses != 0 {
		t.Errorf("PoseBytes = %d, want none for a file with no rig", poses)
	}
	// Playing one still draws both primitives as one batch each: a clip changes
	// the weights an instance carries, not how many draws there are.
	playing := drawing(t, morphStressAsset, scene.ModelDraw{
		Plays: []model.ClipPlay{{Clip: "TheWave", Time: 0.9, Loop: true, Weight: 1}},
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
		if _, ok := err.(model.ErrModelMorphWeightsOverLength); ok {
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
