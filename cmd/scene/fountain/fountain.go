package main

import (
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
)

// Fountain is the demo plugin and the fountain's whole state: the spray, the
// live motes, the meshes baked at startup, the tallies and what the HUD shows.
//
// Every handler that touches it is ordered against the others - the step
// handler runs before scene's flush, which runs before gfx's present, which
// runs before rearm - so no two of them ever run at once.
type Fountain struct {
	spray fountain.Spray
	motes []fountain.Mote

	spawned, retired int

	cube, disc model.MeshRef

	moteMaterial, basinMaterial scene.Material
	foxPlays                    [2]model.ClipPlay

	// tint and passes are reused call to call: scene copies what a call is
	// given before the call returns.
	tint   []gfx.ParameterDescr
	passes []scene.Pass

	counted fountain.HUD // this step's census, shown beside the next step's snapshot
	shown   fountain.HUD

	snapshots fountain.Snapshots
}

// peakMotes is the population the slice is sized for: the spray settles near
// a hundred and twenty.
const peakMotes = 256

// New builds the demo plugin at step zero.
func New() *Fountain {
	return &Fountain{
		spray: fountain.NewSpray(),
		motes: make([]fountain.Mote, 0, peakMotes),
		moteMaterial: scene.Material{{
			Tag:   scene.TagForward,
			Descr: gfx.MaterialWithState(fountain.MoteShader(), fountain.MoteState()),
		}},
		basinMaterial: scene.Material{
			{
				Tag:   scene.PassTag(fountain.TagGround),
				Descr: gfx.MaterialWithState(fountain.StoneShader(), fountain.StoneState()),
			},
			{
				Tag:   scene.TagForward,
				Descr: gfx.MaterialWithState(fountain.RippleShader(), fountain.RippleState()),
			},
		},
		foxPlays: [2]model.ClipPlay{
			{Clip: fountain.FoxWalk, Loop: true, Weight: 1},
			{Clip: fountain.FoxRun, Loop: true},
		},
		tint: make([]gfx.ParameterDescr, 0, 1),
	}
}

// setup bakes the two meshes through scene's lookup. It runs once, on the init
// event.
func (p *Fountain) setup() (kernel.Lock, kernel.Observe[app.InitEvent]) {
	var lookup kernel.Write[*model.Lookup]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*model.Lookup]()
		}, func(k kernel.Kernel, _ app.InitEvent) {
			la := model.NewLookupAccess(k, lookup.Get())
			cubeVertices, cubeIndices := fountain.CubeGeometry()
			p.cube = la.BakeMesh(cubeVertices, cubeIndices, gfx.TopologyTriangleList)
			discVertices, discIndices := fountain.DiscGeometry()
			p.disc = la.BakeMesh(discVertices, discIndices, gfx.TopologyTriangleList)
		}
}

// step is one step of the fountain: it throws what the step is owed, moves
// and retires the motes, records the frame, and prints the HUD.
func (p *Fountain) step() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var sceneQueue kernel.Write[*scene.OpQueue]
	var canvasQueue kernel.Write[*canvas.OpQueue]
	return func(access kernel.ResourceAccess) {
			sceneQueue = access.GetWrite[*scene.OpQueue]()
			canvasQueue = access.GetWrite[*canvas.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) {
			p.simulate()
			census := p.record(sceneQueue.Get())
			p.hud(canvasQueue.Get(), census)
		}
}

// simulate throws, falls, drifts and reaps, in the order cmd/ecs/fountain's
// Systems run: a mote thrown this step also falls and drifts this step.
func (p *Fountain) simulate() {
	for range p.spray.Advance() {
		p.motes = append(p.motes, p.spray.Throw())
		p.spawned++
	}
	for i := range p.motes {
		fountain.Fall(&p.motes[i].Motion)
	}
	for i := range p.motes {
		mote := &p.motes[i]
		fountain.Drift(&mote.Place, &mote.Age, mote.Motion)
	}
	live := p.motes[:0]
	for _, mote := range p.motes {
		if fountain.Spent(mote.Age) {
			p.retired++
			continue
		}
		live = append(live, mote)
	}
	clear(p.motes[len(live):])
	p.motes = live

	t := p.spray.Clock()
	walk, run := fountain.FoxGait(t, &p.foxPlays[0].Time, &p.foxPlays[1].Time)
	p.foxPlays[0].Weight, p.foxPlays[1].Weight = walk, run
}

// record records the frame: one call per mote, the basin, the nozzle and the
// fox, the two lights and the camera. It hands back the census of what it
// recorded.
func (p *Fountain) record(q *scene.OpQueue) fountain.HUD {
	t := p.spray.Clock()
	census := fountain.HUD{Step: p.spray.Step(), Spawned: p.spawned, Retired: p.retired}

	q.Model(0, fountain.NozzlePath, scene.ModelDraw{Transform: fountain.NozzlePlace()})
	q.Model(0, fountain.FoxPath, scene.ModelDraw{Transform: fountain.FoxPlace(t), Plays: p.foxPlays[:]})
	census.Foxes++

	for i := range p.motes {
		mote := &p.motes[i]
		p.tint = append(p.tint[:0], gfx.ColorParam(fountain.MoteTintParam, fountain.Tint(mote.Age)))
		q.Mesh(0, p.cube, scene.MeshDraw{
			Transform: mote.Place,
			Material:  p.moteMaterial,
			Params:    p.tint,
			Bounds:    fountain.MoteBounds,
		})
		census.Motes++
	}
	q.Mesh(0, p.disc, scene.MeshDraw{Material: p.basinMaterial, Bounds: fountain.BasinBounds})

	q.PointLight(0, model.LightDescr{
		Position:  m.Vec3{Y: fountain.LampHeight},
		Color:     fountain.LampColor,
		Intensity: fountain.LampIntensity,
		Range:     fountain.LampRange,
	})
	spot := fountain.SpotPlace(t)
	q.SpotLight(0, model.LightDescr{
		Position:  spot.Position,
		Direction: spot.Rotation.Rotate(fountain.Facing),
		Color:     fountain.SpotColor,
		Intensity: fountain.SpotIntensity,
		InnerCone: fountain.SpotInner,
		OuterCone: fountain.SpotOuter,
	})
	census.Lights += 2

	// A ground pass that clears colour and depth, then the forward pass that
	// clears depth and keeps the ground's colour.
	p.passes = append(p.passes[:0],
		scene.Pass{
			Tag:        scene.PassTag(fountain.TagGround),
			ClearColor: m.Some(fountain.BackdropColor),
			ClearDepth: m.Some[float32](1),
		},
		scene.Pass{Tag: scene.TagForward, ClearDepth: m.Some[float32](1), Order: 1},
	)
	q.Camera(CameraMain, scene.CameraDescr{
		Transform:     fountain.CameraPlace(t),
		FovY:          fountain.FieldOfViewY,
		Near:          fountain.Near,
		Far:           fountain.Far,
		SunDirection:  fountain.SunDirection,
		SunColor:      fountain.SunColor,
		SunIntensity:  fountain.SunIntensity,
		AmbientSky:    fountain.SkyColor,
		AmbientGround: fountain.EarthColor,
		Passes:        p.passes,
	})
	census.Cameras++
	return census
}

// hud prints the previous step's census beside the snapshot of the frame that
// step drew. The snapshot of a tick comes back at that tick's very end, so
// this step's census waits a step for its frame.
func (p *Fountain) hud(q *canvas.OpQueue, census fountain.HUD) {
	p.shown = p.counted.WithFrame(p.snapshots.Latest().Frame)
	p.counted = census
	p.shown.Draw(q, layerHUD, p.spray.Step())
}

// rearm collects the snapshot of the tick that has just ended and arms the
// next one.
func (p *Fountain) rearm() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var arm func(kernel.Kernel, gfx.ArmFrameRequest) gfx.ArmFrameResponse
	return func(access kernel.ResourceAccess) {
			arm = access.Uses[gfx.ArmFrameCmd]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) {
			p.snapshots.Rearm(k, arm)
		}
}
