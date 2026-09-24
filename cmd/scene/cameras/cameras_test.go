package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog-examples/internal/headless"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// run starts the demo headless over the vendored asset set, waits for the
// model to be resident, and steps until the model is pickable, which is when
// the frame it draws is the frame the reference screenshot was taken of.
//
// Preload loads without stepping, so the first frame scene draws already has
// the model in it; the loop after it waits for the bounds System, which asks
// on the first step the backend is up.
func run(t *testing.T) (*headless.Engine, *Cameras) {
	t.Helper()
	demo := New()
	engine := headless.New(t, demo)
	engine.LookupDevice(func(la model.LookupDeviceAccess) {
		la.Preload(modelPath)
		if err := la.State(modelPath); err != nil {
			t.Fatalf("Preload left %q unloaded: %v", modelPath, err)
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for !demo.state.resident {
		if time.Now().After(deadline) {
			t.Fatalf("the model never became pickable; engine reported %v", engine.Errors())
		}
		engine.Steps(1)
	}
	if errs := engine.Errors(); len(errs) > 0 {
		t.Fatalf("the engine reported %d errors, first: %v", len(errs), errs[0])
	}
	return engine, demo
}

// frame is what one step sent the backend: its passes in the order gfx began
// them, and its draws, each indexing those passes.
type frame struct {
	passes []gfx.PassDesc
	draws  []headless.DrawCall
}

// step takes one step and keeps what it sent the backend. The backend
// accumulates across every step since the engine started, so this takes the
// tail and rebases the draws onto it.
func step(t *testing.T, engine *headless.Engine) frame {
	t.Helper()
	backend := engine.Backend()
	passes, draws := len(backend.Passes), len(backend.Draws)
	engine.Steps(1)
	f := frame{
		passes: slices.Clone(backend.Passes[passes:]),
		draws:  slices.Clone(backend.Draws[draws:]),
	}
	for i := range f.draws {
		f.draws[i].Pass -= passes
	}
	return f
}

// label is the backend label of one camera's pass under one tag, which is
// scene's spelling: scene.camera<id>.<tag>.
func label(camera scene.CameraID, tag scene.PassTag) string {
	return fmt.Sprintf("scene.camera%d.%s", camera, tag)
}

// pass is the index of the pass carrying a label in this frame, and fails the
// test when there is none.
func (f frame) pass(t *testing.T, label string) int {
	t.Helper()
	for i := range f.passes {
		if f.passes[i].Label == label {
			return i
		}
	}
	t.Fatalf("no pass labelled %q reached the backend: %v", label, f.labels())
	return -1
}

// drawsIn is every draw the frame made in one pass.
func (f frame) drawsIn(pass int) []headless.DrawCall {
	var out []headless.DrawCall
	for _, draw := range f.draws {
		if draw.Pass == pass {
			out = append(out, draw)
		}
	}
	return out
}

func (f frame) labels() []string {
	out := make([]string, len(f.passes))
	for i := range f.passes {
		out[i] = f.passes[i].Label
	}
	return out
}

// The draws each layer makes, one per Entity: every debug shape bakes a mesh
// of its own, and no two world Entities share a mesh and a colour, so nothing
// batches.
const (
	// worldDraws is the ground plane, four cubes, the obelisk's one mesh, and
	// the model's one primitive.
	worldDraws = 1 + len(cubes) + 1 + 1
	// overlayDraws is the eye sphere and the frustum's eight lines, with
	// nothing picked. A pick adds the wire box, which is one mesh.
	overlayDraws = 1 + 8
	// behindTheEye is how many cubes stand behind the main camera at the
	// reference pose, where it is at the middle of the corridor.
	behindTheEye = 2
)

func TestTheFrameBeginsItsCameraPassesInOneAscendingOrderBeforeTheComposite(t *testing.T) {
	// Pass.Order is an offset from the camera id, not an absolute, so the depth
	// prepass lands one before its own camera without the demo knowing what
	// number that camera took. The whole frame - three cameras and canvas's
	// layers - shares one flat ordering space with no reserved ranges in it, so
	// the only thing that keeps the prepass before its colour pass and all of
	// them before the composite is that these numbers ascend. gfx begins passes
	// in that order, so the order the backend saw them in is the sort.
	engine, _ := run(t)
	f := step(t, engine)
	want := []string{
		label(CameraMain, scene.TagForward),
		label(CameraMap, TagDepth),
		label(CameraMap, scene.TagForward),
	}
	var scenePasses []string
	lastScene, firstOther := -1, -1
	for i, got := range f.labels() {
		if strings.HasPrefix(got, "scene.") {
			scenePasses = append(scenePasses, got)
			lastScene = i
		} else if firstOther < 0 {
			firstOther = i
		}
	}
	if !slices.Equal(scenePasses, want) {
		t.Errorf("the camera passes began as %v, want %v", scenePasses, want)
	}
	if firstOther < 0 || firstOther < lastScene {
		t.Errorf("the composite does not follow every camera pass: %v", f.labels())
	}
}

func TestEachCameraSeesOnlyTheLayersItsCullMaskNames(t *testing.T) {
	// The overlay layer carries the main camera's own frustum, which is the one
	// thing in the frame that must not appear in the main camera. The minimap's
	// two cameras draw the two layers into one merged pass, so it holds every
	// Entity of both; the main camera holds the world alone, less what stands
	// behind it.
	engine, _ := run(t)
	f := step(t, engine)
	if got := len(f.drawsIn(f.pass(t, label(CameraMap, scene.TagForward)))); got != worldDraws+overlayDraws {
		t.Errorf("the minimap drew %d draws, want the world's %d and the overlay's %d",
			got, worldDraws, overlayDraws)
	}
	if got := len(f.drawsIn(f.pass(t, label(CameraMain, scene.TagForward)))); got != worldDraws-behindTheEye {
		t.Errorf("the main camera drew %d draws, want the world's %d less the %d behind it and no overlay",
			got, worldDraws, behindTheEye)
	}
}

func TestOnlyTheObeliskAppearsInTheDepthPass(t *testing.T) {
	// Tag participation is purely a material property. Every debug shape and
	// the model take a material serving forward alone, so the depth pass draws
	// the one thing whose Material carries a depth tag - though the minimap's
	// camera sees all of them, which is what separates a tag filter from a
	// cull.
	engine, _ := run(t)
	f := step(t, engine)
	depth := f.drawsIn(f.pass(t, label(CameraMap, TagDepth)))
	if len(depth) != 1 || depth[0].Instances != 1 {
		t.Errorf("the depth pass made %+v, want one draw of the obelisk alone", depth)
	}
	forward := f.drawsIn(f.pass(t, label(CameraMap, scene.TagForward)))
	if len(forward) <= len(depth) {
		t.Errorf("the forward pass made %d draws, want more than the depth pass's %d", len(forward), len(depth))
	}
}

func TestTheMainCameraCullsWhatIsBehindItAndTheMinimapCullsNothing(t *testing.T) {
	// At the reference pose the camera stands at the middle of the corridor, so
	// two of the four cubes are behind it and the minimap, looking straight
	// down from above, holds the whole world at once. The draw counts are the
	// per-camera cull, and they are what proves the two cameras have their own
	// frusta rather than sharing one.
	engine, demo := run(t)
	f := step(t, engine)
	if got := len(f.drawsIn(f.pass(t, label(CameraMap, scene.TagForward)))); got != worldDraws+overlayDraws {
		t.Errorf("the minimap drew %d, want all %d of the world and the overlay: it culls nothing",
			got, worldDraws+overlayDraws)
	}
	if got := len(f.drawsIn(f.pass(t, label(CameraMain, scene.TagForward)))); got != worldDraws-behindTheEye {
		t.Errorf("the main camera drew %d, want %d: the world less the cubes behind it",
			got, worldDraws-behindTheEye)
	}

	// Which ones, against the frustum of the matrix scene draws the main
	// camera through: asserting that a specific cube is rejected by a specific
	// frustum is what the count cannot say.
	main := panels(demo.state.time())[0]
	viewProjection, ok := main.viewProjection()
	if !ok {
		t.Fatal("scene refused the main camera's view-projection")
	}
	frustum := m.FrustumFromMat4(viewProjection)
	behind := 0
	for i := range cubes {
		sphere := cubeSphere(i)
		visible := frustum.ContainsSphere(sphere.Center, sphere.Radius)
		ahead := cubes[i].position.Z > main.place.Position.Z
		if visible != ahead {
			t.Errorf("cube %q at z %+.1f is %v to the frustum of a camera at z %+.2f",
				cubes[i].name, cubes[i].position.Z, visible, main.place.Position.Z)
		}
		if !visible {
			behind++
		}
	}
	if behind != behindTheEye {
		t.Errorf("%d cubes are outside the main frustum, want %d", behind, behindTheEye)
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
	//
	// A merged run is encoded under the label of the pass that opened it, so
	// the overlay camera's label is absent from the GPU stream exactly when it
	// merged, and that is a sharper observable than a count.
	engine, _ := run(t)
	f := step(t, engine)
	overlay := fmt.Sprintf("scene.camera%d", CameraOverlay)
	for _, got := range f.labels() {
		if strings.HasPrefix(got, overlay) {
			t.Fatalf("gfx encoded %q as a pass of its own, so it did not merge", got)
		}
	}
	// And the pass it merged into did reach the GPU carrying the overlay's
	// draws, so the absence above is a merge rather than a pass that never
	// happened.
	head := f.pass(t, label(CameraMap, scene.TagForward))
	if got := len(f.drawsIn(head)); got != worldDraws+overlayDraws {
		t.Errorf("the merged pass made %d draws, want the minimap's %d and the overlay's %d",
			got, worldDraws, overlayDraws)
	}
	if f.passes[head].DepthStore != gfx.StoreKeep {
		t.Error("the minimap's colour pass discards its depth, so nothing could merge onto it")
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

func TestTwoCamerasHoldingOneIdReportAndDrawOnce(t *testing.T) {
	// A Camera's id is its identity, so two Entities holding one means two
	// Systems each believe they own that camera. The demo provokes it on
	// purpose, which is also why it installs an error handler of its own: the
	// kernel's default terminates the engine on the first report.
	engine, demo := run(t)
	engine.Input(input.KeyChange(input.KeyD, 0, true))
	engine.Steps(1)
	f := step(t, engine)
	if !demo.state.duplicate {
		t.Fatal("holding D did not reach the demo, so no second camera was spawned")
	}

	errs := engine.Errors()
	if len(errs) == 0 {
		t.Fatal("two cameras under one id reported nothing")
	}
	duplicate, ok := errs[len(errs)-1].(scene.ErrCameraAlreadyRecorded)
	if !ok || duplicate.Camera != CameraMain {
		t.Fatalf("last report = %v, want a duplicate of camera %d", errs[len(errs)-1], CameraMain)
	}
	// One of the two is drawn and the other refused, so the frame still begins
	// the main camera's pass once. Which one is scene's walk of its Camera
	// Store, which the ECS leaves unspecified, so it is not asserted.
	mainPasses := 0
	for _, got := range f.labels() {
		if got == label(CameraMain, scene.TagForward) {
			mainPasses++
		}
	}
	if mainPasses != 1 {
		t.Errorf("the main camera's pass began %d times with D held, want once", mainPasses)
	}

	// Letting go despawns the impostor, and the reports stop.
	engine.Input(input.KeyChange(input.KeyD, 0, false))
	engine.Steps(1)
	reported := len(engine.Errors())
	engine.Steps(2)
	if got := len(engine.Errors()); got != reported {
		t.Errorf("the engine went on reporting after D was let go: %v", engine.Errors()[reported:])
	}
}

func TestANameplateRoundTripsBackToTheCubeThroughEitherPanel(t *testing.T) {
	// The nameplate goes world to screen and the click goes screen to world, so
	// a sign the two do not share is the bug both criteria are watching for.
	// This asserts the structural half of it - that the two are inverses per
	// panel, at that panel's own target size - and leaves the visible half,
	// that a plate is glued to its cube on screen, to eyes.
	_, demo := run(t)
	for _, panel := range panels(demo.state.time()) {
		hits := 0
		for i := range cubes {
			// The centre rather than the plate's anchor: the anchor is above
			// the top face and outside the cube's bounding sphere, so a ray
			// through it is meant to miss. What this asserts is that the two
			// helpers are inverses of each other for this camera at this size.
			texel, ok := panel.worldToScreen(cubes[i].position)
			if !ok || !panel.view.contains(texel) {
				continue
			}
			hits++
			ray, ok := panel.screenToRay(texel)
			if !ok {
				t.Errorf("no ray through %q's own plate", cubes[i].name)
				continue
			}
			hit, ok := pick(ray, demo.state.targets)
			if !ok || demo.state.targets[hit].name != cubes[i].name {
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
	views := panels(demo.state.time())
	main, minimap := views[0], views[1]
	behind := main.place.Position.Sub(m.Vec3{Z: 4})
	if _, ok := main.worldToScreen(behind); ok {
		t.Error("a point four units behind the eye came back with a screen position")
	}
	if _, ok := minimap.worldToScreen(behind); !ok {
		t.Error("the orthographic camera refused a point it can see")
	}
	// And a point in front but off the side of the target stays true, because
	// an off-screen indicator arrow is the second most common use of this.
	aside := main.place.Position.Add(m.Vec3{X: 40, Z: 6})
	screen, ok := main.worldToScreen(aside)
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
	own := panels(demo.state.time())[1]
	foreign := own
	foreign.view = mainPanel
	anchor := nameplateAnchor(0)
	ownAt, ok := own.worldToScreen(anchor)
	if !ok {
		t.Fatal("the minimap cannot see the first cube")
	}
	foreignAt, ok := foreign.worldToScreen(anchor)
	if !ok {
		t.Fatal("the minimap's camera refused the main panel's size")
	}
	if ownAt == foreignAt {
		t.Errorf("both sizes answered %v, so the viewport is not being read", ownAt)
	}
}

func TestAClickInEitherPanelPicksThroughThatPanelsOwnCamera(t *testing.T) {
	// The criterion, driven the way a hand drives it: a pointer position in
	// window pixels and a mouse button, through the demo's own System.
	//
	// It is the same cube through both panels, and the minimap is the harder
	// half - the click has to be mapped into 512 texels of a target composited
	// into 400 canvas units before an orthographic camera is asked about it,
	// and a scale dropped anywhere in that chain lands the ray somewhere else.
	engine, demo := run(t)
	for _, panel := range panels(demo.state.time()) {
		texel, ok := panel.worldToScreen(cubes[3].position)
		if !ok || !panel.view.contains(texel) {
			t.Fatalf("panel %v cannot see the cube this test clicks", panel.view.rect)
		}
		click(engine, panel.view.canvas(texel))
		if got := demo.state.pickedName(); got != cubes[3].name {
			t.Fatalf("clicking %q through panel %v picked %q", cubes[3].name, panel.view.rect, got)
		}
	}
	// The pick reaches the frame: the wire box around it joins the minimap's
	// overlay.
	f := step(t, engine)
	if got := len(f.drawsIn(f.pass(t, label(CameraMap, scene.TagForward)))); got != worldDraws+overlayDraws+1 {
		t.Errorf("the minimap drew %d with a cube picked, want %d and the wire box",
			got, worldDraws+overlayDraws)
	}
	// And a click in the gap between the panels picks nothing, rather than
	// picking through whichever camera is nearer.
	click(engine, m.Vec2{X: (mainPanel.rect.X + mainPanel.rect.Width + mapPanel.rect.X) / 2, Y: 300})
	if got := demo.state.pickedName(); got != "" {
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
