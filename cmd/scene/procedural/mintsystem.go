package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
)

// mint brings the frame's geometry up to date: the three mesh calls, in the
// three lifetimes they exist for.
//
// The ridge is updated in place every frame. The ribbon is baked fresh every
// frame and the previous one released. The beacon is released and re-baked on
// the beat, and the ref the release invalidated is handed to the ghost for
// exactly one frame, so scene can be seen to skip it.
//
// It holds the *model.Lookup write lock because a LookupAccess is the only way
// to bake, update or release a mesh, and it runs before scene's load System,
// which drains what it queued: a ref minted here is uploaded and drawn this
// frame.
func mint(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	meshes *ecs.Set[scene.Mesh],
	state *ecs.Write[*Procedural],
) {
	p, la := state.Get(), model.NewLookupAccess(k, lookup.Get())
	p.stale = model.MeshRef{}
	p.staleID.Store(0)

	// The ridge. UpdateMesh replaces the geometry wholesale at any size while
	// keeping the ref and the buffer id, and refuses a change of vertex layout
	// or topology - both of which the pipeline key and the sort assume are
	// fixed for a ref's life. Only the size changes here, and only every
	// ridgeResizePeriod steps. The Mesh Component does not change at all: the
	// ref it holds is the same, and what it names is new.
	if p.step/ridgeResizePeriod%2 == 1 {
		p.ridgeCellCount = ridgeCoarseCells
	} else {
		p.ridgeCellCount = ridgeCells
	}
	vertices, indices := ridgeGeometry(p.ridgeCellCount, p.time()*ridgeSpeed)
	if la.UpdateMesh(p.ridge, vertices, indices) {
		p.updates++
	}

	// The ribbon. The last frame's is released first, so the fresh bake takes
	// back its slot under the next generation: the id the HUD prints holds
	// still while the generation counts frames. The released buffers are freed
	// at the frame boundary, after the frame that drew them is done with them.
	la.ReleaseMesh(p.ribbon)
	p.releases++
	p.ribbon = p.bakeRibbon(la)
	setMesh(meshes, p.ribbonEntity, p.ribbon)

	// The beacon. A release makes the old ref stale at once - anything drawing
	// it afterwards is reported and skipped - so the beacon and the stray take
	// the new ref in the same run, and the ghost takes the old one.
	ghost := model.MeshRef{}
	if p.advanced && p.step%beaconPeriod == 0 {
		p.stale = p.beacon
		p.staleID.Store(p.beacon.ID())
		la.ReleaseMesh(p.beacon)
		p.releases++
		p.generation++
		p.beacon = p.bakeBeacon(la)
		setMesh(meshes, p.beaconEntity, p.beacon)
		setMesh(meshes, p.strayEntity, p.beacon)
		ghost = p.stale
		p.staleDraws++
	}

	// The one-frame ghost. On the step the beacon was released, the ref that
	// release invalidated is drawn once more, at the beacon's own place. It
	// draws nothing: scene reports it unavailable, once for the ref however
	// many Entities name it, and skips its Batch. On every other step the
	// ghost holds the zero ref, which scene skips without a word. If a second
	// beacon ever appears here, a released ref has started drawing whatever
	// took its slot.
	setMesh(meshes, p.ghostEntity, ghost)
}

// setMesh points one Entity's Mesh at ref, keeping its bounds and cull flag.
func setMesh(meshes *ecs.Set[scene.Mesh], e ecs.Entity, ref model.MeshRef) {
	if mesh, ok := meshes.Ref(e); ok {
		mesh.Ref = ref
	}
}
