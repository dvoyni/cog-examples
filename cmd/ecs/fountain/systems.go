package main

import (
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// setup bakes the two meshes through scene's lookup and spawns everything that
// is not a mote. It runs once, on the init event.
func setup(
	k kernel.Kernel,
	lookup *ecs.Write[*scene.Lookup],
	state *ecs.Write[*Fountain],
	nozzles *ecs.Spawn[nozzle],
	foxes *ecs.Spawn[fox],
	basins *ecs.Spawn[basin],
	lamps *ecs.Spawn[lamp],
	eyes *ecs.Spawn[eye],
) {
	f, la := state.Get(), scene.NewLookupAccess(k, lookup.Get())
	cubeVertices, cubeIndices := cubeGeometry()
	f.cube = la.BakeMesh(cubeVertices, cubeIndices, gfx.TopologyTriangleList)
	discVertices, discIndices := discGeometry()
	f.disc = la.BakeMesh(discVertices, discIndices, gfx.TopologyTriangleList)

	nozzles.New(nozzle{
		Place: m.Transform{
			Position: m.Vec3{Y: nozzleLift},
			Scale:    m.NewVec3(nozzleScale),
		},
		Model: ecsscene.Model{Ref: scene.ModelRef{Path: nozzlePath}},
	})
	foxes.New(fox{
		Place: foxPlace(0),
		Model: ecsscene.Model{Ref: scene.ModelRef{Path: foxPath}},
		Gait: ecsscene.Animation{Plays: [ecsscene.MaxPlays]scene.ClipPlay{
			{Clip: foxWalk, Loop: true, Weight: 1},
			{Clip: foxRun, Loop: true},
		}},
	})
	basins.New(basin{
		Draw:  ecsscene.Mesh{Ref: f.disc, Bounds: basinBounds},
		Shade: basinMaterial(),
	})
	lamps.New(lamp{
		Place: m.Transform{Position: m.Vec3{Y: lampHeight}},
		Light: ecsscene.Light{Kind: scene.LightPoint, Color: lampColor, Intensity: lampIntensity, Range: lampRange},
	})
	lamps.New(lamp{
		Place: spotPlace(0),
		Light: ecsscene.Light{
			Kind: scene.LightSpot, Color: spotColor, Intensity: spotIntensity,
			InnerCone: spotInner, OuterCone: spotOuter,
		},
	})
	eyes.New(eye{Place: cameraPlace(0), Camera: camera()})
}

// hatch advances the clock and throws the motes this step is owed.
func hatch(motes *ecs.Spawn[mote], state *ecs.Write[*Fountain]) {
	f := state.Get()
	f.step++
	f.sinceSpawn += fixedStep
	for f.sinceSpawn >= spawnInterval {
		f.sinceSpawn -= spawnInterval
		motes.New(f.spray())
		f.spawned++
	}
}

type fallQuery struct {
	Motion *Velocity
}

func accelerate(q *ecs.Query[fallQuery]) {
	for _, it := range q.All() {
		it.Motion.V.Y -= gravity * fixedStep
	}
}

type driftQuery struct {
	Place  *m.Transform
	Tint   *ecsscene.Params
	Age    *Life
	Motion Velocity
}

// drift moves, stretches, ages and fades every mote.
func drift(q *ecs.Query[driftQuery]) {
	for _, it := range q.All() {
		it.Age.Remaining -= fixedStep
		*it.Place = stretch(it.Place.Position.Add(it.Motion.V.MulS(fixedStep)), it.Motion)
		it.Tint.Values.Set(0, gfx.ColorParam(moteTintParam, tint(*it.Age)))
	}
}

type reapQuery struct {
	Age Life
}

func reap(q *ecs.Query[reapQuery], dead *ecs.WriteableEntities, state *ecs.Write[*Fountain]) {
	f := state.Get()
	for e, it := range q.All() {
		if it.Age.Remaining <= 0 && dead.Despawn(e) {
			f.retired++
		}
	}
}

type (
	foxQuery struct {
		Place *m.Transform
		Gait  *ecsscene.Animation
	}
	spotQuery struct {
		Place *m.Transform
		Light ecsscene.Light
	}
)

// prowl walks the fox round its circle and keeps the spot on it.
//
// Clip time is advanced here, by the game, because scene and the binding are
// both stateless about animation: each clip runs at the rate that matches its
// stride to the fox's ground speed, which only the game knows.
func prowl(foxes *ecs.Query[foxQuery], lights *ecs.Query[spotQuery], state *ecs.Read[*Fountain]) {
	t := state.Get().clock()
	speed := foxSpeed(t)
	walk, run := foxBlend(speed)
	for _, it := range foxes.All() {
		*it.Place = foxPlace(t)
		plays := &it.Gait.Plays
		plays[0].Time += fixedStep * speed / walkPace
		plays[0].Weight = walk
		plays[1].Time += fixedStep * speed / runPace
		plays[1].Weight = run
	}
	for _, it := range lights.All() {
		if it.Light.Kind == scene.LightSpot {
			*it.Place = spotPlace(t)
		}
	}
}

type cameraQuery struct {
	Place  *m.Transform
	Camera ecsscene.Camera
}

func orbit(q *ecs.Query[cameraQuery], state *ecs.Read[*Fountain]) {
	t := state.Get().clock()
	for _, it := range q.All() {
		*it.Place = cameraPlace(t)
	}
}

type (
	moteCount   struct{ Age Life }
	foxCount    struct{ Gait ecsscene.Animation }
	lightCount  struct{ Light ecsscene.Light }
	cameraCount struct{ Camera ecsscene.Camera }
)

// hud counts the world and prints it. Scene publishes a frame's passes at the
// end of the tick that recorded it, so what is shown is the previous step's
// census beside the flush that drew it, and this step's census waits a tick.
func hud(
	motes *ecs.Query[moteCount],
	foxes *ecs.Query[foxCount],
	lights *ecs.Query[lightCount],
	cameras *ecs.Query[cameraCount],
	state *ecs.Write[*Fountain],
	drawn *ecs.Read[*scene.OpQueue],
	overlay *ecs.Write[*canvas.OpQueue],
) {
	f := state.Get()
	f.views = drawn.Get().Passes(f.views[:0])
	f.shown = f.counted
	f.shown.Passes, f.shown.Drawn, f.shown.Batches, f.shown.Culled = len(f.views), 0, 0, 0
	for i := range f.views {
		f.shown.Drawn += f.views[i].Instances
		f.shown.Batches += len(f.views[i].Batches)
		f.shown.Culled += f.views[i].Culled
	}

	f.counted = HUD{Step: f.step, Spawned: f.spawned, Retired: f.retired}
	for range motes.All() {
		f.counted.Motes++
	}
	for range foxes.All() {
		f.counted.Foxes++
	}
	for range lights.All() {
		f.counted.Lights++
	}
	for range cameras.All() {
		f.counted.Cameras++
	}

	f.shown.draw(overlay.Get(), f.step)
}

// HUDCmd answers what the HUD is showing, so a test can read it.
type HUDCmd kernel.Command[HUDRequest, HUD]

type HUDRequest struct{}

func readHUD(state *ecs.Read[*Fountain], answer *ecs.Resp[HUD]) {
	answer.Set(state.Get().shown)
}
