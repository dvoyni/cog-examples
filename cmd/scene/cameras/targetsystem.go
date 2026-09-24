package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

type cameraQuery struct {
	Camera *scene.Camera
}

// The depth and colour clears the passes name. A Pass takes each as an
// m.Maybe, whose zero value preserves, so a clear is spelled m.Some(value).
//
// The depth clear is 1.0, which is worth saying out loud: depth here is
// conventional, near maps to 0 and far to 1 and the compare is Less, so the
// naive ClearDepth of zero clears to the near plane and hides the whole scene.
var (
	clearFar     = m.Some[float32](1)
	clearMain    = m.Some(mainClearColor)
	clearMinimap = m.Some(mapClearColor)
)

// target allocates this frame's render targets and writes them into every
// Camera's Passes, and publishes the two colour textures for the composite.
//
// It holds gfx's queue, which no other System of this demo does. Minting a
// render target takes the gfx queue, and scene deliberately offers no allocator
// of its own: a Pass's Target is the gfx handle passed through untouched, so an
// app that renders a camera into a temporary target mints it and hands it
// across in the Component. The targets are frame-local, so the handover is
// every frame, and it runs Before scene reads the Cameras.
func target(cameras *ecs.Query[cameraQuery], queue *ecs.Write[*gfx.OpQueue], state *ecs.Write[*State]) {
	g, s := queue.Get(), state.Get()
	if g == nil {
		return
	}
	mainTarget, mainTexture := g.TemporaryTarget(
		int(mainPanel.size.X), int(mainPanel.size.Y), gfx.FormatRGBA8Srgb)
	mapTarget, mapTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatRGBA8Srgb)
	// Two depth textures, and they are deliberately not the same one.
	//
	// mapDepth is the minimap's own, named rather than pooled because
	// DepthStore is inferred as StoreKeep exactly when a pass names a depth
	// texture - which is what lets the minimap's colour pass and the overlay
	// camera's pass merge into one GPU pass.
	//
	// prepassDepth is the depth-only pass's, and nothing else in the frame
	// touches it. That independence is the point: a NoTarget() pass has no
	// colour attachment to take a size from, so it must name a depth texture or
	// it is a reported error, and this is the shape a shadow map takes - render
	// depth from somewhere, sample it later. scene has no shadows, so nothing
	// samples it, and a backend that cannot encode a colourless pass therefore
	// drops it without changing a pixel of the frame. Feeding it into the
	// minimap's colour pass instead would have made that skip render the whole
	// minimap against undefined depth.
	_, mapDepthTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatDepth32F)
	_, prepassDepthTexture := g.TemporaryTarget(
		int(mapPanel.size.X), int(mapPanel.size.Y), gfx.FormatDepth32F)
	mapDepth := gfx.DepthTarget(mapDepthTexture)
	prepassDepth := gfx.DepthTarget(prepassDepthTexture)
	s.mainTexture, s.mapTexture = mainTexture, mapTexture

	for _, it := range cameras.All() {
		switch it.Camera.ID {
		case CameraMain:
			// The impostor holds this id too, and gets the same pass: whichever
			// of the two scene draws, it draws into this frame's target.
			writePasses(&it.Camera.Passes, scene.Pass{
				Target: mainTarget, ClearColor: clearMain, ClearDepth: clearFar,
				// Depth is left at its zero value, which is DepthAuto: a pooled
				// texture shared with every other same-size automatic pass in
				// the frame. That is why it must clear depth - it would
				// otherwise inherit whatever the last pass at this size left
				// there.
			})
		case CameraMap:
			writePasses(&it.Camera.Passes,
				scene.Pass{
					// The depth prepass: no colour target at all, its own depth
					// texture, one pass earlier than the camera. Order is an
					// offset from the camera id rather than an absolute, so -1
					// here means "just before this camera" without the demo
					// knowing what number the camera took.
					Tag: TagDepth, Target: gfx.NoTarget(), Depth: prepassDepth,
					ClearDepth: clearFar, Order: -1,
				},
				scene.Pass{
					Target: mapTarget, Depth: mapDepth,
					ClearColor: clearMinimap, ClearDepth: clearFar,
				},
			)
		case CameraOverlay:
			// The overlay camera: the same pose and projection as the minimap,
			// a different cull mask, and a pass that clears nothing. Clearing
			// nothing is what makes it merge with the pass above into one GPU
			// pass - same target, same depth texture, both loads preserving -
			// so the second camera costs a pass in scene and none on the GPU.
			writePasses(&it.Camera.Passes, scene.Pass{Target: mapTarget, Depth: mapDepth})
		}
	}
}

// writePasses writes a camera's passes into its stored List: in place when the
// List already holds that many, which is every frame but the first, so the
// steady frame allocates nothing for them.
func writePasses(list *m.List[scene.Pass], passes ...scene.Pass) {
	if list.Len() != len(passes) {
		*list = m.NewList(passes...)
		return
	}
	for i, pass := range passes {
		list.Set(i, pass)
	}
}
