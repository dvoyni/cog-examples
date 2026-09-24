package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// referenceStep is the step the assertions below are written against: two
// seconds of demo time, far enough in that the spin and the sphere's orbit have
// both moved off their starting values. Demo time is accumulated fixed steps,
// so this is the same frame on every machine.
const referenceStep = 120

// debugShaderPath is the shader every debug shape draws with, which is what
// picks the shapes' draws out of a frame that also drew the HUD.
const debugShaderPath = "builtin/scene/debug.wgsl"

// cameraLabel is the label scene gives CameraMain's passes, up to the tag.
var cameraLabel = fmt.Sprintf("scene.camera%d.", CameraMain)

// frame is what one step sent the backend: its passes in order, and its draws
// with Pass rebased onto them.
type frame struct {
	backend *headless.Backend
	passes  []gfx.PassDesc
	draws   []headless.DrawCall
}

// referenceFrame drives the demo to the step before referenceStep, then takes
// one more and keeps what that step sent the backend.
func referenceFrame(t *testing.T) frame {
	t.Helper()
	engine := headless.New(t, New())
	engine.Steps(referenceStep - 1)
	backend := engine.Backend()
	passes, draws := len(backend.Passes), len(backend.Draws)
	engine.Steps(1)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	if backend.Presents == 0 {
		t.Fatal("the backend was never asked to present")
	}
	f := frame{
		backend: backend,
		passes:  slices.Clone(backend.Passes[passes:]),
		draws:   slices.Clone(backend.Draws[draws:]),
	}
	// Draws index Passes across the whole run; rebase them onto this step's.
	for i := range f.draws {
		f.draws[i].Pass -= passes
	}
	return f
}

// cameraPasses is the indexes of the step's passes CameraMain emitted.
func (f frame) cameraPasses() []int {
	var out []int
	for i, pass := range f.passes {
		if strings.HasPrefix(pass.Label, cameraLabel) {
			out = append(out, i)
		}
	}
	return out
}

// shapeDraws is the step's draws through the debug shader.
func (f frame) shapeDraws() []headless.DrawCall {
	var out []headless.DrawCall
	for _, draw := range f.draws {
		if f.backend.ShaderPath(f.backend.PipelineOf(draw.Pipeline).Shader) == debugShaderPath {
			out = append(out, draw)
		}
	}
	return out
}

// A camera with an empty Passes emits exactly one pass: the implicit forward
// pass, at the camera's own id, which is the contract box exists to pin. The
// id is the pass's order, so it lands after canvas's backdrop clear at -200
// and before the HUD at 0, and it preserves the colour the backdrop cleared.
func TestAnEmptyPassesEmitsOneForwardPassAtTheCameraID(t *testing.T) {
	f := referenceFrame(t)
	camera := f.cameraPasses()
	if len(camera) != 1 {
		t.Fatalf("the camera emitted %d passes, want the one implicit pass", len(camera))
	}
	at := camera[0]
	pass := f.passes[at]
	if want := cameraLabel + string(scene.TagForward); pass.Label != want {
		t.Errorf("the camera's pass is %q, want %q", pass.Label, want)
	}
	if pass.Load == gfx.LoadClear {
		t.Error("the implicit pass clears colour; it must preserve what the backdrop cleared")
	}
	if at == 0 || f.passes[at-1].Load != gfx.LoadClear {
		t.Errorf("the pass before the camera's does not clear colour: passes %v", labels(f.passes))
	}
	if at == len(f.passes)-1 {
		t.Errorf("nothing draws after the camera, so the HUD is missing: passes %v", labels(f.passes))
	}
}

// Each shape Entity is one draw of one instance, all in the camera's pass,
// all through the debug shader's opaque material, since every Color is opaque.
// The wire box's twelve edges are one mesh, so it is one draw like the rest;
// the index counts tell the kinds apart: a box or a line is 36 indices, the
// plane 6, the sphere 1056 and the wire box twelve boxes' 432.
//
// At the documented pose the whole scene is in front of the camera, so this is
// also the assertion that nothing is culled, and that every shape the demo
// spawns has a size: a degenerate shape bakes no mesh and draws nothing.
func TestEveryShapeIsOneDrawAtTheDocumentedPose(t *testing.T) {
	f := referenceFrame(t)
	camera := f.cameraPasses()
	if len(camera) != 1 {
		t.Fatalf("the camera emitted %d passes, want one", len(camera))
	}
	draws := f.shapeDraws()
	if len(draws) != DrawnShapes {
		t.Fatalf("the shapes made %d draws, want %d, one for each shape", len(draws), DrawnShapes)
	}
	kinds := map[int]int{}
	for _, draw := range draws {
		if draw.Pass != camera[0] {
			t.Errorf("a shape drew in pass %d, want the camera's %d", draw.Pass, camera[0])
		}
		if draw.Instances != 1 || !draw.Indexed {
			t.Errorf("a shape drew %d instances indexed %v, want one indexed instance",
				draw.Instances, draw.Indexed)
		}
		if blend := f.backend.PipelineOf(draw.Pipeline).State.Blend; blend != gfx.BlendOpaque {
			t.Errorf("a shape drew with blend %v, want opaque", blend)
		}
		kinds[draw.Count]++
	}
	want := map[int]int{
		36:   2 + 3, // the two boxes and the three axis lines
		6:    1,     // the ground plane
		1056: 1,     // the sphere
		432:  1,     // the wire box
	}
	if !maps.Equal(kinds, want) {
		t.Errorf("the shapes drew index counts %v, want %v", kinds, want)
	}
}

// The HUD's frustum question, asked the way hudSystem asks it: through
// scene.ViewProjection for the camera at its documented pose. The orbiting
// sphere is inside it at referenceStep; a point far behind the camera is not.
func TestTheSphereIsInTheFrustumAtTheDocumentedPose(t *testing.T) {
	s := NewState()
	s.step = referenceStep
	viewport := m.Vec2{X: headless.WindowWidth, Y: headless.WindowHeight}
	in, err := sphereInFrustum(camera(), s.eyePlace(), viewport, s.spinCenter(), sphereRadius)
	if err != nil {
		t.Fatalf("the camera cannot be projected: %v", err)
	}
	if !in {
		t.Error("the frustum rejects the orbiting sphere, which is on screen")
	}
	behind := s.eye().Sub(orbitTarget).MulS(2).Add(orbitTarget)
	if in, _ := sphereInFrustum(camera(), s.eyePlace(), viewport, behind, sphereRadius); in {
		t.Errorf("the frustum contains %v, which is behind the camera at %v", behind, s.eye())
	}
}

// Demo time is accumulated fixed steps, never wall clock, so the same step
// number is the same frame: two engines driven the same number of steps send
// the backend the same passes and the same draws.
func TestTheSameStepIsTheSameFrame(t *testing.T) {
	first, second := referenceFrame(t), referenceFrame(t)
	if !slices.Equal(labels(first.passes), labels(second.passes)) {
		t.Fatalf("two runs of %d steps began passes %v and %v",
			referenceStep, labels(first.passes), labels(second.passes))
	}
	if !slices.Equal(first.draws, second.draws) {
		t.Errorf("two runs of %d steps drew\n%+v\nand\n%+v", referenceStep, first.draws, second.draws)
	}
}

// Every zero value is the default. The camera writes neither CullMask nor the
// two intensities nor Passes nor Projection, and the resting box writes no
// Scale at all: what the demo spawns is exactly what it typed, and scene reads
// the zeroes as the defaults rather than the demo filling them in. The frame
// above, drawn from these very values, is what shows scene read them so.
func TestTheDemoLeavesTheDefaultsUnwritten(t *testing.T) {
	c := camera()
	if c.CullMask != 0 {
		t.Errorf("the camera writes CullMask %v; the demo means to leave it zero", c.CullMask)
	}
	if c.SunIntensity != 0 || c.AmbientIntensity != 0 {
		t.Errorf("the camera writes intensities %v and %v; the demo means to leave them zero",
			c.SunIntensity, c.AmbientIntensity)
	}
	if c.Passes.Len() != 0 {
		t.Errorf("the camera declares %d passes; the demo means to declare none", c.Passes.Len())
	}
	if c.Projection != 0 {
		t.Errorf("the camera writes Projection %v; the demo means to leave it zero", c.Projection)
	}
	if box := resting(); box.Place.Scale != (m.Vec3{}) || box.Place.Rotation != (m.Quat{}) {
		t.Errorf("the resting box writes Scale %v and Rotation %v; an all-zero Transform means the identity",
			box.Place.Scale, box.Place.Rotation)
	}
}

// The spot is placed by its Transform alone: its rotation turns -Z, the way
// an unrotated light faces, to straight down onto the resting box.
func TestTheSpotFacesStraightDown(t *testing.T) {
	facing := spot().Place.Rotation.Rotate(m.Vec3{Z: -1})
	if facing.Sub(m.Vec3{Y: -1}).Length() > 1e-5 {
		t.Errorf("the spot faces %v, want straight down", facing)
	}
}

func labels(passes []gfx.PassDesc) []string {
	out := make([]string, len(passes))
	for i := range passes {
		out[i] = passes[i].Label
	}
	return out
}
