package main

import (
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
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
	cubeVertices, cubeIndices := fountain.CubeGeometry()
	f.cube = la.BakeMesh(cubeVertices, cubeIndices, gfx.TopologyTriangleList)
	discVertices, discIndices := fountain.DiscGeometry()
	f.disc = la.BakeMesh(discVertices, discIndices, gfx.TopologyTriangleList)

	nozzles.New(nozzle{
		Place: fountain.NozzlePlace(),
		Model: ecsscene.Model{Ref: model.ModelRef{Path: fountain.NozzlePath}},
	})
	foxes.New(fox{
		Place: fountain.FoxPlace(0),
		Model: ecsscene.Model{Ref: model.ModelRef{Path: fountain.FoxPath}},
		Gait: ecsscene.Animation{Plays: [model.MaxClipPlays]model.ClipPlay{
			{Clip: fountain.FoxWalk, Loop: true, Weight: 1},
			{Clip: fountain.FoxRun, Loop: true},
		}},
	})
	basins.New(basin{
		Draw:  ecsscene.Mesh{Ref: f.disc, Bounds: fountain.BasinBounds},
		Shade: basinMaterial(),
	})
	lamps.New(lamp{
		Place: m.Transform{Position: m.Vec3{Y: fountain.LampHeight}},
		Light: ecsscene.Light{Descr: model.LightDescr{
			Kind: model.LightPoint, Color: fountain.LampColor,
			Intensity: fountain.LampIntensity, Range: fountain.LampRange,
		}},
	})
	lamps.New(lamp{
		Place: fountain.SpotPlace(0),
		Light: ecsscene.Light{Descr: model.LightDescr{
			Kind: model.LightSpot, Color: fountain.SpotColor, Intensity: fountain.SpotIntensity,
			InnerCone: fountain.SpotInner, OuterCone: fountain.SpotOuter,
		}},
	})
	eyes.New(eye{Place: fountain.CameraPlace(0), Camera: camera()})
}

// hatch advances the spray and throws the motes this step is owed.
func hatch(motes *ecs.Spawn[mote], state *ecs.Write[*Fountain]) {
	f := state.Get()
	for range f.spray.Advance() {
		motes.New(f.throw())
		f.spawned++
	}
}

type fallQuery struct {
	Motion *fountain.Velocity
}

func accelerate(q *ecs.Query[fallQuery]) {
	for _, it := range q.All() {
		fountain.Fall(it.Motion)
	}
}

type driftQuery struct {
	Place  *m.Transform
	Tint   *ecsscene.Params
	Age    *fountain.Life
	Motion fountain.Velocity
}

// drift moves, stretches, ages and fades every mote.
func drift(q *ecs.Query[driftQuery]) {
	for _, it := range q.All() {
		tint := fountain.Drift(it.Place, it.Age, it.Motion)
		it.Tint.Values.Set(0, gfx.ColorParam(fountain.MoteTintParam, tint))
	}
}

type reapQuery struct {
	Age fountain.Life
}

func reap(q *ecs.Query[reapQuery], dead *ecs.WriteableEntities, state *ecs.Write[*Fountain]) {
	f := state.Get()
	for e, it := range q.All() {
		if fountain.Spent(it.Age) && dead.Despawn(e) {
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
	t := state.Get().spray.Clock()
	for _, it := range foxes.All() {
		*it.Place = fountain.FoxPlace(t)
		plays := &it.Gait.Plays
		plays[0].Weight, plays[1].Weight = fountain.FoxGait(t, &plays[0].Time, &plays[1].Time)
	}
	for _, it := range lights.All() {
		if it.Light.Descr.Kind == model.LightSpot {
			*it.Place = fountain.SpotPlace(t)
		}
	}
}

type cameraQuery struct {
	Place  *m.Transform
	Camera ecsscene.Camera
}

func orbit(q *ecs.Query[cameraQuery], state *ecs.Read[*Fountain]) {
	t := state.Get().spray.Clock()
	for _, it := range q.All() {
		*it.Place = fountain.CameraPlace(t)
	}
}
