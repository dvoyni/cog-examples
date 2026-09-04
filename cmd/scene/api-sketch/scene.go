// This file is the api-sketch stub of the scene plugin's recording surface. It
// is deliberately not the plugin: nothing here talks to gfx or the GPU. Every
// call appends a record so main.go can print what a frame recorded, which is
// all this sketch needs to answer "does scene code read as cleanly as canvas
// code does?".
//
// Types whose *contents* belong to a later ticket are stubbed down to the
// fields the usages actually need, and marked with the issue that decides them.
package main

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// ---------------------------------------------------------------------------
// Identity and masks
// ---------------------------------------------------------------------------

// CameraID identifies a camera and orders cameras against each other: it is the
// default gfx pass Order for every pass the camera declares. Canvas's implicit
// default pass sits at Order 0, so a scene camera drawn under a canvas HUD
// conventionally takes a negative id.
type CameraID int32

// LayerMask selects which cameras see a draw. A model is recorded once with a
// mask; each camera carries a cull mask and renders the draws it intersects.
type LayerMask uint32

// Layer builds a single-bit mask. Note Layer(0) is bit zero, not the zero mask:
// the zero mask reads as LayersAll, so a demo that does not care about layers
// never mentions them.
func Layer(index uint) LayerMask { return 1 << index }

// LayersAll is every layer, and the reading of a zero mask.
const LayersAll LayerMask = ^LayerMask(0)

func effectiveMask(mask LayerMask) LayerMask {
	if mask == 0 {
		return LayersAll
	}
	return mask
}

// ---------------------------------------------------------------------------
// Transform
// ---------------------------------------------------------------------------

// Transform is TRS, not m.Mat4: scene decomposes for the instance record and
// for bounding-sphere culling anyway, and glTF nodes are TRS at rest.
//
// The zero Transform is the identity. Scale is a scalar whose zero value means
// 1, matching canvas.SpriteTransform.Scale exactly; non-uniform scale goes
// through Matrix, which replaces the whole transform when non-nil. Keeping
// scale uniform keeps normals on the cheap path.
type Transform struct {
	Position m.Vec3
	Rotation m.Quat
	Scale    float32
	Matrix   *m.Mat4
}

// At is the common case: a position, no rotation, unit scale.
func At(x, y, z float32) Transform {
	return Transform{Position: m.Vec3{X: x, Y: y, Z: z}}
}

// WithScale sets the uniform scale.
func (t Transform) WithScale(s float32) Transform {
	t.Scale = s
	return t
}

// WithRotation sets the rotation.
func (t Transform) WithRotation(q m.Quat) Transform {
	t.Rotation = q
	return t
}

// LookAt builds the transform of an object at eye facing target. It is the
// camera's convenience: scene inverts the camera transform to get the view
// matrix, so callers never handle one.
func LookAt(eye, target, up m.Vec3) Transform {
	view := m.LookAt4(eye, target, up)
	return Transform{Position: eye, Rotation: quatFromView(view), Scale: 1}
}

// quatFromView is a placeholder for m.QuatFromMat4 on the inverse view basis;
// the sketch never evaluates it.
func quatFromView(m.Mat4) m.Quat { return m.Quat{W: 1} }

// ---------------------------------------------------------------------------
// Camera  (contents decided by #13)
// ---------------------------------------------------------------------------

// ProjectionKind selects how CameraDescr's projection fields are read.
type ProjectionKind uint8

// Projection kinds.
const (
	Perspective ProjectionKind = iota
	Orthographic
)

// PassTag maps a camera pass to the shader a material registered for it, the
// Unity LightMode pattern. A model whose material has no shader for the tag is
// skipped in that pass. Tag participation is a property of the material alone:
// a draw gets no say in it.
type PassTag string

// TagForward is the tag the default pass carries.
const TagForward PassTag = "forward"

// Pass is one camera pass: where it draws, under which tag, and where it sorts.
//
// Order is an offset from the camera's id in the gfx Order space that canvas
// layers and scene cameras share (#8). Zero — the common case — puts the pass
// at the camera's own id; a shadow pass writes a negative offset to sort ahead
// of the forward pass without moving the camera.
type Pass struct {
	Tag        PassTag
	Target     TargetRef // zero value means the screen sentinel
	ClearColor *m.Color  // nil preserves the target's contents
	ClearDepth *float32
	Order      int
}

// TargetRef stands in for the gfx target handle from #8: the screen sentinel, a
// durable ResourceQueue texture, or a frame-local TemporaryTarget. Scene passes
// it through untouched — bridging a camera's output texture into canvas is
// canvas's problem, not a shared naming registry.
type TargetRef struct{ Name string }

// CameraDescr is the whole camera. Sun and ambient live here because they are
// per-camera by charting decision; point and spot lights are recorded per frame
// instead.
type CameraDescr struct {
	Transform  Transform // the camera as a positioned object; scene inverts it
	Projection ProjectionKind
	FovY       float32 // Perspective: vertical field of view, radians
	Height     float32 // Orthographic: world units across the viewport height
	Near, Far  float32

	Viewport m.Rect // within the target, normalised 0..1; zero value is the whole target

	CullMask LayerMask // zero value reads as LayersAll

	SunDirection m.Vec3
	SunColor     m.Color
	Ambient      m.Color

	// Passes is ordered. Empty means exactly one pass: tag TagForward, the
	// screen, Order = the camera id, and no clear on either attachment. A
	// camera that wants a clear writes a Pass literal — clears live in one
	// place, never on the camera.
	Passes []Pass
}

// defaultPass is what an empty Passes list means.
func defaultPass(id CameraID) Pass { return Pass{Tag: TagForward, Order: int(id)} }

// ---------------------------------------------------------------------------
// Animation  (contents decided by #15 / #16)
// ---------------------------------------------------------------------------

// ClipPlay is stateless: the caller owns time. Scene never advances it.
type ClipPlay struct {
	Clip   string // clip name inside the model file
	Time   float32
	Weight float32
}

// ---------------------------------------------------------------------------
// Draws
// ---------------------------------------------------------------------------

// ModelDraw is everything about drawing one glTF model that is not its path or
// its layer mask. It is a struct rather than a widening argument list because
// the animated case needs six knobs where the static case needs one.
//
// Every slice field is borrowed for the duration of the call: scene copies into
// its frame arena before returning, so a caller in a hot loop may reuse one
// backing array across frames.
type ModelDraw struct {
	Transform Transform

	// Scene names an entry in the file's scenes array; empty is the file's
	// default scene. Node names a subtree within that scene; empty is the whole
	// scene. What a name resolves to is #14's.
	Scene string
	Node  string

	// Plays is nil for the rest pose. MorphWeights is flat across the whole
	// draw; what index i addresses is #16's to fix.
	Plays        []ClipPlay
	MorphWeights []float32

	// Material overrides every primitive's material for this draw. Nil keeps
	// the materials the file declared.
	Material *gfx.MaterialDescr

	// OverrideParams broadcasts to every material this draw binds — all six of
	// FlightHelmet's, not one. Named for the broadcast because the common
	// per-draw override (team colour, hit flash, fade) wants exactly that.
	OverrideParams []gfx.ParameterDescr
}

// MeshRef stands in for the buffer-built mesh handle from #22.
type MeshRef struct{ Name string }

// MeshDraw is the buffer-built counterpart of ModelDraw. Material is optional —
// nil is the bundled PBR — so a mesh draw needs nothing but a handle.
//
// Bounds is the local-space culling sphere (xyz centre, w radius) that scene
// cannot derive for geometry it did not load. Its zero value means **never
// cull**: a frame that draws too much is a performance problem you can see, a
// frame that silently drops geometry at certain camera angles is not.
type MeshDraw struct {
	Transform Transform
	Material  *gfx.MaterialDescr
	Params    []gfx.ParameterDescr
	Bounds    m.Vec4
}

// LightKind distinguishes the two punctual lights scene supports; plain
// directional lights beyond the per-camera sun were dropped while charting.
type LightKind uint8

// Light kinds.
const (
	LightPoint LightKind = iota
	LightSpot
)

// LightDescr is one punctual light, recorded per frame with a mask like a
// model. One struct backs both kinds so the per-camera light buffer (#17) is
// homogeneous; the two recording methods keep the call site explicit, and a
// point light simply leaves Direction and the cone angles zero.
type LightDescr struct {
	Position  m.Vec3
	Direction m.Vec3 // LightSpot only
	Color     m.Color
	Intensity float32
	Range     float32
	InnerCone float32 // LightSpot only
	OuterCone float32 // LightSpot only
	Kind      LightKind
}

// ---------------------------------------------------------------------------
// OpQueue
// ---------------------------------------------------------------------------

// OpQueue is the frame-local recording surface, the scene twin of
// canvas.OpQueue. Gameplay binds it write-locked on app.UpdateEvent and records
// the whole frame; scene consumes and resets it.
type OpQueue struct {
	cameras []cameraOp
	draws   []drawOp
	lights  []lightOp
}

type cameraOp struct {
	descr CameraDescr
	id    CameraID
}

type drawOp struct {
	path   string
	mesh   MeshRef
	model  ModelDraw
	buffer MeshDraw
	layers LayerMask
	isMesh bool
}

type lightOp struct {
	light  LightDescr
	layers LayerMask
}

// Camera declares or updates the camera at id. Recording the same id twice in a
// frame replaces it, the way SetLayerTransform does for a canvas layer.
func (q *OpQueue) Camera(id CameraID, descr CameraDescr) {
	for i := range q.cameras {
		if q.cameras[i].id == id {
			q.cameras[i].descr = descr
			return
		}
	}
	q.cameras = append(q.cameras, cameraOp{id: id, descr: descr})
}

// Model draws a glTF model loaded lazily by path, as canvas.Sprite does for a
// texture.
func (q *OpQueue) Model(layers LayerMask, path string, draw ModelDraw) {
	q.draws = append(q.draws, drawOp{layers: layers, path: path, model: draw})
}

// Mesh draws geometry the caller built.
func (q *OpQueue) Mesh(layers LayerMask, mesh MeshRef, draw MeshDraw) {
	q.draws = append(q.draws, drawOp{layers: layers, mesh: mesh, buffer: draw, isMesh: true})
}

// PointLight records a point light for this frame.
func (q *OpQueue) PointLight(layers LayerMask, light LightDescr) {
	light.Kind = LightPoint
	q.lights = append(q.lights, lightOp{layers: layers, light: light})
}

// SpotLight records a spot light for this frame.
func (q *OpQueue) SpotLight(layers LayerMask, light LightDescr) {
	light.Kind = LightSpot
	q.lights = append(q.lights, lightOp{layers: layers, light: light})
}

// Reset discards the recorded frame.
func (q *OpQueue) Reset() {
	q.cameras = q.cameras[:0]
	q.draws = q.draws[:0]
	q.lights = q.lights[:0]
}

// OpCount reports how many draws and lights were recorded.
func (q *OpQueue) OpCount() int { return len(q.draws) + len(q.lights) }

// ---------------------------------------------------------------------------
// Primitives
//
// The scene twin of canvas's FillRect / StrokeRect / Line: sugar over one
// scene-owned unit mesh and the bundled PBR, so they cost the spec a paragraph
// and give every demo a floor and a debug vocabulary.
//
// Box, Sphere and Plane are lit — they are cheap real geometry. Line3D and
// WireBox are self-lit (base colour black, emissive the given colour) through
// the *same* shader, so a debug line stays visible in a frame with no sun,
// which is exactly the frame you are debugging.
//
// A line is a long thin box, not a line-list primitive: WebGPU has no
// line-width control at all, so GPU lines rasterise one physical pixel wide and
// vanish on a hidpi display. A stretched box has thickness the caller controls,
// keeps scene to one shader and one topology, and batches with every other
// primitive. Its thickness is world-space, so a distant line thins out.
// ---------------------------------------------------------------------------

// Box draws a lit unit cube under transform.
func (q *OpQueue) Box(layers LayerMask, transform Transform, color m.Color) {
	q.primitive(layers, "box", transform, color, false)
}

// Sphere draws a lit sphere.
func (q *OpQueue) Sphere(layers LayerMask, center m.Vec3, radius float32, color m.Color) {
	q.primitive(layers, "sphere", Transform{Position: center, Scale: radius}, color, false)
}

// Plane draws a lit ground plane of the given size, centred on center.
func (q *OpQueue) Plane(layers LayerMask, center m.Vec3, size m.Vec2, color m.Color) {
	q.primitive(layers, "plane", Transform{Position: center}, color, false)
	_ = size
}

// Line3D draws a self-lit box stretched from start to end.
func (q *OpQueue) Line3D(layers LayerMask, start, end m.Vec3, thickness float32, color m.Color) {
	q.primitive(layers, "line", Transform{Position: start}, color, true)
	_, _ = end, thickness
}

// WireBox draws the twelve edges of a box as self-lit lines.
func (q *OpQueue) WireBox(layers LayerMask, center, size m.Vec3, thickness float32, color m.Color) {
	q.primitive(layers, "wirebox", Transform{Position: center}, color, true)
	_, _ = size, thickness
}

func (q *OpQueue) primitive(layers LayerMask, name string, transform Transform, color m.Color, flat bool) {
	param := gfx.ColorParam("baseColorFactor", color)
	if flat {
		param = gfx.ColorParam("emissiveFactor", color)
	}
	q.Mesh(layers, MeshRef{Name: name}, MeshDraw{
		Transform: transform,
		Params:    []gfx.ParameterDescr{param},
	})
}
