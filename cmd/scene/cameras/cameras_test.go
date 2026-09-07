package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/assets"
	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// run starts the demo headless over the vendored asset set and steps until the
// model is resident, which is when the frame it records is the frame the
// reference screenshot was taken of.
//
// The wait is wall clock rather than a frame count on purpose: a load does not
// run on the frame's thread, so a test that waited in frames would be asserting
// the asynchronous path does not exist.
func run(t *testing.T) (*headless.Engine, *Cameras) {
	t.Helper()
	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
	if err != nil {
		t.Fatalf("locate assets: %v", err)
	}
	demo := New()
	engine := headless.NewOver(t, config, demo)
	deadline := time.Now().Add(60 * time.Second)
	for len(demo.targets) < len(cubes)+2 {
		if time.Now().After(deadline) {
			t.Fatalf("the model never became pickable; engine reported %v", engine.Errors())
		}
		engine.Steps(1)
		time.Sleep(time.Millisecond)
	}
	// One more pair of steps: the frame that first sees the model resident is
	// also the first to record its draw, and Passes publishes the frame the
	// last flush consumed.
	engine.Steps(2)
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine, demo
}

// The frame's four passes, named. Scene publishes in camera-id order and then
// in each camera's own declaration order, so this sequence is what the demo
// arranged; the Order values beside it are what gfx actually sorts by, and the
// test asserts both because they are two different facts.
type expectedPass struct {
	camera scene.CameraID
	tag    scene.PassTag
	order  gfx.Order
}

var expectedPasses = [...]expectedPass{
	{CameraMain, scene.TagForward, gfx.Order(CameraMain)},
	{CameraMap, TagDepth, gfx.Order(CameraMap) - 1},
	{CameraMap, scene.TagForward, gfx.Order(CameraMap)},
	{CameraOverlay, scene.TagForward, gfx.Order(CameraOverlay)},
}

func TestTheFrameEmitsFourPassesInOneAscendingOrder(t *testing.T) {
	// Pass.Order is an offset from the camera id, not an absolute, so the depth
	// prepass lands one before its own camera without the demo knowing what
	// number that camera took. The whole frame - three cameras and canvas's
	// layers - shares one flat ordering space with no reserved ranges in it, so
	// the only thing that keeps the prepass before its colour pass and both
	// before the composite is that these numbers ascend.
	engine, _ := run(t)
	passes := engine.Passes()
	if len(passes) != len(expectedPasses) {
		t.Fatalf("published %d passes, want %d", len(passes), len(expectedPasses))
	}
	for i, want := range expectedPasses {
		got := passes[i]
		if got.CameraID != want.camera || got.Tag != want.tag || got.Order != want.order {
			t.Errorf("pass %d = camera %d tag %q order %d, want camera %d tag %q order %d",
				i, got.CameraID, got.Tag, got.Order, want.camera, want.tag, want.order)
		}
		if i > 0 && passes[i-1].Order >= got.Order {
			t.Errorf("pass %d orders %d then %d, which do not ascend", i, passes[i-1].Order, got.Order)
		}
	}
}

func TestEachCameraSeesOnlyTheLayersItsCullMaskNames(t *testing.T) {
	// The overlay layer carries the main camera's own frustum, which is the one
	// thing in the frame that must not appear in the main camera. Recorded is
	// what a camera's cull mask selected, before its frustum has rejected
	// anything, so these numbers are the mask and nothing else.
	engine, _ := run(t)
	passes := engine.Passes()
	for i, view := range passes {
		want := RecordedWorldDraws
		if view.CameraID == CameraOverlay {
			want = RecordedOverlayDraws
		}
		if view.Recorded != want {
			t.Errorf("pass %d (camera %d) recorded %d draws, want %d",
				i, view.CameraID, view.Recorded, want)
		}
	}
}

func TestOnlyTheObeliskAppearsInTheDepthPass(t *testing.T) {
	// Tag participation is purely a material property. Every debug shape and
	// the model take the bundled PBR, which has one forward entry and no depth
	// entry, so the depth pass draws the one thing whose material carries one -
	// and it selected all of them, which is what separates a tag filter from a
	// cull.
	engine, _ := run(t)
	depth := passOf(t, engine, CameraMap, TagDepth)
	if depth.Instances != 1 {
		t.Errorf("the depth pass packed %d instances, want the obelisk alone", depth.Instances)
	}
	if depth.Recorded != RecordedWorldDraws {
		t.Errorf("the depth pass recorded %d draws, want the whole world layer", depth.Recorded)
	}
	forward := passOf(t, engine, CameraMap, scene.TagForward)
	if forward.Instances <= depth.Instances {
		t.Errorf("the forward pass packed %d instances, want more than the depth pass's %d",
			forward.Instances, depth.Instances)
	}
}

func TestTheMainCameraCullsWhatIsBehindItAndTheMinimapCullsNothing(t *testing.T) {
	// At the reference pose the camera stands at the middle of the corridor, so
	// two of the four cubes are behind it and the minimap, looking straight
	// down from above, holds the whole world at once. This is the per-camera
	// cull count, and it is the number that proves the two cameras have their
	// own frusta rather than sharing one.
	engine, demo := run(t)
	main := passOf(t, engine, CameraMain, scene.TagForward)
	minimap := passOf(t, engine, CameraMap, scene.TagForward)
	if minimap.Culled != 0 {
		t.Errorf("the minimap culled %d draws; it looks down at the whole world", minimap.Culled)
	}
	if main.Culled == 0 {
		t.Error("the main camera culled nothing at the reference pose, where two cubes stand behind it")
	}

	// Which ones, against the pass's own published frustum rather than against
	// the count: asserting that a specific cube was rejected by a specific
	// frustum is the whole point of publishing it.
	camera := mainCamera(demo.time())
	for i := range cubes {
		sphere := cubeSphere(i)
		visible := main.Frustum.ContainsSphere(sphere.Center, sphere.Radius)
		ahead := cubes[i].position.Z > camera.Transform.Position.Z
		if visible != ahead {
			t.Errorf("cube %q at z %+.1f is %v to the frustum of a camera at z %+.2f",
				cubes[i].name, cubes[i].position.Z, visible, camera.Transform.Position.Z)
		}
	}
}

func TestTheMinimapsTwoCamerasMergeIntoOneGpuPass(t *testing.T) {
	// The minimap's colour pass and the overlay camera's pass name the same
	// target and the same depth texture and neither of the overlay's loads
	// clears, so gfx collapses them. The depth store is what makes it possible:
	// scene infers StoreKeep exactly when a pass names a depth texture of its
	// own, and the merge predicate needs StoreKeep on both of the
	// predecessor's attachments. A DepthAuto pass stores discard and would not
	// have merged.
	engine, _ := run(t)
	if scenePasses := len(engine.Passes()); scenePasses != len(expectedPasses) {
		t.Fatalf("scene published %d passes, want %d", scenePasses, len(expectedPasses))
	}
	// A merged run is encoded under the label of the pass that opened it, so
	// the overlay camera's label is absent from the GPU stream exactly when it
	// merged. That is a sharper observable than a count: the backend
	// accumulates passes across every frame since the engine started, so a
	// count would have to know how many frames ran, and the label says which
	// pass rather than how many.
	overlay := fmt.Sprintf("scene.camera%d", CameraOverlay)
	for _, label := range passLabels(engine) {
		if strings.HasPrefix(label, overlay) {
			t.Fatalf("gfx encoded %q as a pass of its own, so it did not merge", label)
		}
	}
	// And the pass it merged into did reach the GPU, so the absence above is a
	// merge rather than a pass that never happened.
	head := fmt.Sprintf("scene.camera%d.%s", CameraMap, scene.TagForward)
	if !slices.Contains(passLabels(engine), head) {
		t.Errorf("no pass labelled %q reached the backend: %v", head, passLabels(engine))
	}
}

func TestTheDepthOnlyPassBuildsAPipelineWithNoColourTarget(t *testing.T) {
	// A render pass declares its attachments and a pipeline declares its
	// targets, and the two are validated against each other. The depth prepass
	// has no colour attachment, so the obelisk's depth entry has to build a
	// pipeline with no colour target - and a pipeline that declared one would
	// be rejected at setPipeline, taking the frame's whole command buffer with
	// it and reporting nothing.
	engine, _ := run(t)
	colourless := 0
	for _, desc := range engine.Backend().Pipelines {
		if desc.NoColorTarget {
			colourless++
		}
	}
	if colourless != 1 {
		t.Errorf("pipelines with no colour target = %d, want the depth pass's one", colourless)
	}
}

func TestRecordingTheSameCameraTwiceReportsAndKeepsTheFirst(t *testing.T) {
	// Camera is a registration, not a free parameter, so a repeated id means
	// two systems each believe they own that camera. The demo provokes it on
	// purpose, which is also why it installs an error handler of its own: the
	// kernel's default terminates the engine on the first report.
	engine, demo := run(t)
	before := len(engine.Passes())
	engine.Input(input.KeyChange(input.KeyD, 0, true))
	engine.Steps(2)
	engine.Input(input.KeyChange(input.KeyD, 0, false))
	if !demo.duplicate {
		t.Fatal("holding D did not reach the demo, so nothing was recorded twice")
	}

	errs := engine.Errors()
	if len(errs) == 0 {
		t.Fatal("recording a camera twice reported nothing")
	}
	duplicate, ok := errs[len(errs)-1].(scene.ErrCameraAlreadyRecorded)
	if !ok || duplicate.Camera != CameraMain {
		t.Fatalf("last report = %v, want a duplicate of camera %d", errs[len(errs)-1], CameraMain)
	}
	// The first record wins, so the frame still has its four passes and the
	// main camera still looks down the corridor rather than up from under the
	// ground, which is what the second record asked for.
	if got := len(engine.Passes()); got != before {
		t.Errorf("passes = %d after the duplicate, want the same %d", got, before)
	}
	main := passOf(t, engine, CameraMain, scene.TagForward)
	if main.Culled == 0 {
		t.Error("the main camera's frustum changed, so the second record won")
	}
}

func TestANameplateRoundTripsBackToTheCubeThroughEitherPanel(t *testing.T) {
	// The nameplate goes world to screen and the click goes screen to world, so
	// a sign the two do not share is the bug both criteria are watching for.
	// This asserts the structural half of it - that the two are inverses per
	// panel, at that panel's own target size - and leaves the visible half,
	// that a plate is glued to its cube on screen, to eyes.
	_, demo := run(t)
	for _, panel := range demo.panels() {
		hits := 0
		for i := range cubes {
			// The centre rather than the plate's anchor: the anchor is above
			// the top face and outside the cube's bounding sphere, so a ray
			// through it is meant to miss. What this asserts is that the two
			// helpers are inverses of each other for this camera at this size.
			screen, ok := scene.WorldToScreen(panel.camera, panel.view.size, cubes[i].position)
			if !ok {
				continue
			}
			texel := m.Vec2{X: screen.X, Y: screen.Y}
			if !panel.view.contains(texel) {
				continue
			}
			hits++
			ray, ok := scene.ScreenToRay(panel.camera, panel.view.size, texel)
			if !ok {
				t.Errorf("no ray through %q's own plate", cubes[i].name)
				continue
			}
			hit, ok := pick(ray, demo.targets)
			if !ok || demo.targets[hit].name != cubes[i].name {
				t.Errorf("the ray through %q's plate picked %v (ok %v)",
					cubes[i].name, hit, ok)
			}
		}
		if hits == 0 {
			t.Errorf("panel %v showed no plates at all, so it asserted nothing", panel.view.rect)
		}
	}
}

func TestAPointBehindThePerspectiveCameraHasNoScreenPositionAndTheOrthographicOneAlwaysDoes(t *testing.T) {
	// Behind the camera never returns a coordinate: dividing by a negative w
	// yields a plausible, mirrored, confidently wrong point, and that is the
	// single classic bug in this helper. An orthographic camera's clip w is 1
	// everywhere, so it never fails the test - which is why the minimap keeps a
	// plate the main view has dropped.
	_, demo := run(t)
	camera := mainCamera(demo.time())
	behind := camera.Transform.Position.Sub(m.Vec3{Z: 4})
	if _, ok := scene.WorldToScreen(camera, mainPanel.size, behind); ok {
		t.Error("a point four units behind the eye came back with a screen position")
	}
	if _, ok := scene.WorldToScreen(mapCamera(LayerWorld), mapPanel.size, behind); !ok {
		t.Error("the orthographic camera refused a point it can see")
	}
	// And a point in front but off the side of the target stays true, because
	// an off-screen indicator arrow is the second most common use of this.
	aside := camera.Transform.Position.Add(m.Vec3{X: 40, Z: 6})
	screen, ok := scene.WorldToScreen(camera, mainPanel.size, aside)
	if !ok {
		t.Error("a point in front of the camera but off the side was refused")
	} else if screen.X >= 0 && screen.X <= mainPanel.size.X {
		t.Errorf("a point forty units to the side landed at x %.1f, inside the target", screen.X)
	}
}

func TestEachPanelAnswersAtItsOwnTargetSize(t *testing.T) {
	// "Where is this point on screen" is ill-formed for a camera with two
	// targets, so the caller names the size it means. Asking the minimap's
	// camera at the main panel's size has to give a different answer from
	// asking it at its own, or nothing in this demo is per target.
	_, demo := run(t)
	camera := mapCamera(LayerWorld)
	anchor := nameplateAnchor(0)
	own, ok := scene.WorldToScreen(camera, mapPanel.size, anchor)
	if !ok {
		t.Fatal("the minimap cannot see the first cube")
	}
	foreign, ok := scene.WorldToScreen(camera, mainPanel.size, anchor)
	if !ok {
		t.Fatal("the minimap's camera refused the main panel's size")
	}
	if own == foreign {
		t.Errorf("both sizes answered %v, so the viewport is not being read", own)
	}
	_ = demo
}

func TestAClickInEitherPanelPicksThroughThatPanelsOwnCamera(t *testing.T) {
	// The criterion, driven the way a hand drives it: a pointer position in
	// window pixels and a mouse button, through the demo's own handler.
	//
	// It is the same cube through both panels, and the minimap is the harder
	// half - the click has to be mapped into 512 texels of a target composited
	// into 400 canvas units before an orthographic camera is asked about it,
	// and a scale dropped anywhere in that chain lands the ray somewhere else.
	engine, demo := run(t)
	for _, panel := range demo.panels() {
		screen, ok := scene.WorldToScreen(panel.camera, panel.view.size, cubes[3].position)
		if !ok || !panel.view.contains(m.Vec2{X: screen.X, Y: screen.Y}) {
			t.Fatalf("panel %v cannot see the cube this test clicks", panel.view.rect)
		}
		click(engine, panel.view.canvas(m.Vec2{X: screen.X, Y: screen.Y}))
		if got := demo.pickedName(); got != cubes[3].name {
			t.Fatalf("clicking %q through panel %v picked %q", cubes[3].name, panel.view.rect, got)
		}
	}
	// And a click in the gap between the panels picks nothing, rather than
	// picking through whichever camera is nearer.
	click(engine, m.Vec2{X: (mainPanel.rect.X + mainPanel.rect.Width + mapPanel.rect.X) / 2, Y: 300})
	if got := demo.pickedName(); got != "" {
		t.Errorf("a click between the panels picked %q", got)
	}
}

// click drives one press at a point in the demo's logical screen, converting it
// back into the window pixels the pointer actually arrives in.
//
// The headless viewport is 960x540 and the demo lays itself out in 1280x720
// fitted into it, so this conversion is not the identity - which is the point:
// a test that clicked in logical coordinates would never exercise the fit.
func click(engine *headless.Engine, at m.Vec2) {
	logical := canvas.WorldToScreen(
		m.Rect{Width: screenWidth, Height: screenHeight}, canvas.AspectInscribe,
		m.Vec2{X: headless.WindowWidth, Y: headless.WindowHeight}, at)
	engine.Input(
		input.PointerChange(input.Pos{X: float64(logical.X), Y: float64(logical.Y)}),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	)
	engine.Steps(1)
	engine.Input(input.KeyChange(input.KeyMouseLeft, 0, false))
	engine.Steps(1)
}

// passOf finds one published pass by camera and tag.
func passOf(t *testing.T, engine *headless.Engine, camera scene.CameraID, tag scene.PassTag) scene.PassView {
	t.Helper()
	for _, view := range engine.Passes() {
		if view.CameraID == camera && view.Tag == tag {
			return view
		}
	}
	t.Fatalf("no pass for camera %d tag %q", camera, tag)
	return scene.PassView{}
}

// passLabels is the labels of the GPU passes the last frame encoded. The
// backend accumulates across every frame since the engine started, so this
// takes the tail: one frame's worth, ending at the frame's present.
func passLabels(engine *headless.Engine) []string {
	all := engine.Backend().Passes
	labels := make([]string, 0, len(all))
	for _, desc := range all {
		labels = append(labels, desc.Label)
	}
	return labels
}
