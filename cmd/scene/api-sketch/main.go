// Command api-sketch is a paper prototype of the scene plugin's recording API
// (dvoyni/cog#12). It draws nothing: it records three representative frames
// against the stub in scene.go and prints them, so the question "does scene
// code read as cleanly as canvas code does?" can be answered by reading
// helloBox, helloTriangle and animatedCharacter below side by side with canvas
// usage.
//
//	go run ./cmd/scene/api-sketch
package main

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// The demo's two cameras. A CameraID is the default gfx Order for the camera's
// passes, and canvas's implicit default pass sits at Order 0 — so a scene
// camera that draws under a canvas HUD takes a negative id.
const (
	cameraMain CameraID = -100
	cameraUI   CameraID = -50
)

// layerCharacter is the one layer this sketch names; everything else records
// into the zero mask, which reads as LayersAll.
var layerCharacter = Layer(1)

func main() {
	var q OpQueue

	helloBox(&q)
	dump("hello box", &q)

	q.Reset()
	helloTriangle(&q)
	dump("hello triangle", &q)

	q.Reset()
	animatedCharacter(&q, 3.25)
	dump("animated character", &q)
}

// helloBox is the floor of the API and the readability test: two statements to
// a lit cube on screen, no mesh handle, no shader, no material. It is the scene
// twin of the hello demo's Clear + FillRect.
func helloBox(q *OpQueue) {
	q.Camera(cameraMain, CameraDescr{
		Transform:    LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:         1.0472, // 60 degrees
		Near:         0.1,
		Far:          100,
		SunDirection: m.Vec3{X: -0.3, Y: -1, Z: -0.2},
		SunColor:     m.Color{R: 1, G: 1, B: 1, A: 1},
		Passes: []Pass{{
			Tag:        TagForward,
			ClearColor: &m.Color{R: 0.06, G: 0.07, B: 0.09, A: 1},
		}},
	})

	q.Box(0, At(0, 0, 0), m.Color{R: 0.42, G: 0.71, B: 0.94, A: 1})
}

// helloTriangle is the same frame from caller-built geometry: the mesh handle
// is the only thing it has to supply, because a nil material is the bundled
// PBR and a zero Bounds means never cull.
func helloTriangle(q *OpQueue) {
	q.Camera(cameraMain, CameraDescr{
		Transform: LookAt(m.Vec3{Z: 3}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      1.0472,
		Near:      0.1,
		Far:       100,
		Passes:    []Pass{{Tag: TagForward, ClearColor: &m.Color{R: 0.06, G: 0.07, B: 0.09, A: 1}}},
	})

	q.Mesh(0, triangleMesh, MeshDraw{
		Params: []gfx.ParameterDescr{
			gfx.ColorParam("baseColorFactor", m.Color{R: 0.42, G: 0.71, B: 0.94, A: 1}),
		},
	})
}

// animatedCharacter is the ceiling: a lit camera, a glTF model cross-fading two
// clips with morph weights alongside, a second model drawn from one node of a
// shared file, a point light, a ground plane, a debug wire box around the
// character's bounds, and a second camera rendering the same recording to a
// texture for a UI portrait.
func animatedCharacter(q *OpQueue, time float32) {
	q.Camera(cameraMain, CameraDescr{
		Transform:    LookAt(m.Vec3{X: 4, Y: 2.5, Z: 6}, m.Vec3{Y: 1}, m.Vec3{Y: 1}),
		FovY:         1.0472,
		Near:         0.1,
		Far:          200,
		SunDirection: m.Vec3{X: -0.3, Y: -1, Z: -0.2},
		SunColor:     m.Color{R: 1, G: 0.97, B: 0.9, A: 1},
		Ambient:      m.Color{R: 0.05, G: 0.06, B: 0.08, A: 1},
		Passes: []Pass{{
			Tag:        TagForward,
			ClearColor: &m.Color{R: 0.02, G: 0.02, B: 0.03, A: 1},
			ClearDepth: ptr[float32](1),
		}},
	})

	// A portrait camera rendering only the character into a texture the UI
	// draws. Same recording, different cull mask: nothing is re-recorded.
	q.Camera(cameraUI, CameraDescr{
		Transform: LookAt(m.Vec3{Y: 1.6, Z: 2}, m.Vec3{Y: 1.5}, m.Vec3{Y: 1}),
		FovY:      0.6,
		Near:      0.05,
		Far:       10,
		CullMask:  layerCharacter,
		Ambient:   m.Color{R: 0.3, G: 0.3, B: 0.35, A: 1},
		Passes: []Pass{{
			Tag:        TagForward,
			Target:     TargetRef{Name: "portrait"},
			ClearColor: &m.Color{A: 0},
		}},
	})

	q.Model(layerCharacter, "models/CesiumMan.glb", ModelDraw{
		Transform: At(0, 0, 0),
		Plays: []ClipPlay{
			{Clip: "walk", Time: time, Weight: 0.7},
			{Clip: "wave", Time: time * 0.5, Weight: 0.3},
		},
		MorphWeights: []float32{0.4, 0, 0.1},
		OverrideParams: []gfx.ParameterDescr{
			gfx.ColorParam("baseColorFactor", m.Color{R: 1, G: 0.8, B: 0.7, A: 1}),
		},
	})

	q.Model(0, "models/props.glb", ModelDraw{
		Transform: At(2, 0, -1).WithScale(0.5),
		Node:      "crate",
	})

	q.PointLight(0, LightDescr{
		Position:  m.Vec3{X: 1.5, Y: 2, Z: 1},
		Color:     m.Color{R: 1, G: 0.6, B: 0.3, A: 1},
		Intensity: 40,
		Range:     12,
	})

	q.Plane(0, m.Vec3{}, m.Vec2{X: 20, Y: 20}, m.Color{R: 0.2, G: 0.21, B: 0.23, A: 1})
	q.WireBox(0, m.Vec3{Y: 0.9}, m.Vec3{X: 0.8, Y: 1.8, Z: 0.6}, 0.01,
		m.Color{R: 0, G: 1, B: 0.4, A: 1})
}

// triangleMesh stands in for the durable mesh handle #22 decides.
var triangleMesh = MeshRef{Name: "triangle"}

func ptr[T any](v T) *T { return &v }

// dump prints a recorded frame so the sketch has something to run.
func dump(title string, q *OpQueue) {
	fmt.Printf("== %s: %d cameras, %d ops ==\n", title, len(q.cameras), q.OpCount())
	for _, c := range q.cameras {
		passes := c.descr.Passes
		if len(passes) == 0 {
			passes = []Pass{defaultPass(c.id)}
		}
		fmt.Printf("  camera %d cull=%#x\n", c.id, effectiveMask(c.descr.CullMask))
		for _, p := range passes {
			fmt.Printf("    pass %q order=%d target=%s clear=%t\n",
				p.Tag, int(c.id)+p.Order, targetName(p.Target), p.ClearColor != nil)
		}
	}
	for _, d := range q.draws {
		if d.isMesh {
			fmt.Printf("  mesh %q layers=%#x cull=%t\n",
				d.mesh.Name, effectiveMask(d.layers), d.buffer.Bounds != (m.Vec4{}))
			continue
		}
		fmt.Printf("  model %q node=%q layers=%#x plays=%d morphs=%d\n",
			d.path, d.model.Node, effectiveMask(d.layers),
			len(d.model.Plays), len(d.model.MorphWeights))
	}
	for _, l := range q.lights {
		kind := "point"
		if l.light.Kind == LightSpot {
			kind = "spot"
		}
		fmt.Printf("  %s light layers=%#x\n", kind, effectiveMask(l.layers))
	}
	fmt.Println()
}

func targetName(t TargetRef) string {
	if t.Name == "" {
		return "screen"
	}
	return t.Name
}
