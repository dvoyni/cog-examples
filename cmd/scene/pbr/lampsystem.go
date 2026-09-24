package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// lamps spawns every punctual light in the frame, all twenty-one at once, on
// the first step the lights station's file answers ModelLights; on every step
// after that it does nothing.
//
// It holds the device facade's three resources because ModelLights goes
// through the device facade, and every query on that facade loads the file it
// names. It runs after scene's load System, which has loaded the file by then
// if it can be, so in practice the query only reads.
//
// The lamps wait for the file rather than the demo's own thirteen going first,
// because the cap's tie-break is offering order and the order is spawn order.
// scene offers its Light Entities in the order its Query walks them, which is
// the Light Store's rows from the last to the first: the Store is the Query's
// shortest, so it drives the walk, and the walk runs backwards so a walk that
// removes as it goes visits everyone. So the lamps are spawned in reverse of
// the order they are ranked in, and the first offered is the fill.
//
// The ECS calls that order unspecified, and this rests on it anyway, because
// nothing else can say which of fifteen zero-scoring lights the cap keeps.
// pbr_test.go pins the outcome - the five deep lamps are the five dropped at
// the reference pose - so a change to the walk fails there rather than quietly
// moving pools of light round the still life.
func lamps(
	k kernel.Kernel,
	lookup *ecs.Write[*model.Lookup],
	files *ecs.Read[storage.FileSystem],
	resources *ecs.Write[*gfx.ResourceQueue],
	spawn *ecs.Spawn[lamp],
	state *ecs.Write[*Pbr],
) {
	p := state.Get()
	if p.declared > 0 || resources.Get() == nil || !resources.Get().Ready() {
		return
	}
	la := model.NewLookupDeviceAccess(k, lookup.Get(), files.Get(), resources.Get())
	fileLights, ok := la.ModelLights(stations[stationLights].path, nil)
	if !ok {
		return
	}
	ranked := rankedLamps(fileLights)
	for i := len(ranked) - 1; i >= 0; i-- {
		spawn.New(ranked[i])
	}
	p.declared = len(ranked)
}

// rankedLamps is every lamp in the order the cap is meant to prefer them among
// equals: the fill, the two spots, the station's own lights and the five rim
// lamps, which are the sixteen the reference pose keeps, then the five deep
// lamps, which are the five it drops.
func rankedLamps(fileLights []model.ModelLight) []lamp {
	out := make([]lamp, 0, DeclaredLights)

	// The fill light. Range is left at zero, which means infinite: it is not
	// culled by any frustum and its falloff window is open everywhere, so it is
	// the one light in the frame with a real score at the reference pose.
	out = append(out, pointLamp(m.Vec3{X: 0, Y: fillHeight, Z: fillDepth}, model.LightDescr{
		Color:     fillColor,
		Intensity: fillIntensity,
	}))

	out = append(out, spotLamp(
		placements[stationBottle].center.Add(m.Vec3{Y: spotHeight}), m.Vec3{Y: -1},
		model.LightDescr{
			Color:     bottleSpot,
			Intensity: spotIntensity,
			Range:     spotRange,
			InnerCone: spotInner,
			OuterCone: spotOuter,
		}))
	out = append(out, spotLamp(
		placements[stationAlpha].center.Add(m.Vec3{Y: spotHeight, Z: -1}), m.Vec3{Y: -1, Z: 0.25},
		model.LightDescr{
			Color:     alphaSpot,
			Intensity: spotIntensity,
			Range:     spotRange,
			InnerCone: spotInner,
			OuterCone: spotOuter,
		}))

	out = appendFileLamps(out, fileLights)

	for i := range rimColors {
		out = append(out, pointLamp(m.Vec3{X: spread(i, rimCount, rimSpacing), Y: rimY, Z: rimZ},
			model.LightDescr{Color: rimColors[i], Intensity: rimIntensity, Range: rimRange}))
	}
	for i := range deepColors {
		out = append(out, pointLamp(m.Vec3{X: spread(i, deepCount, deepSpacing), Y: deepY, Z: deepZ},
			model.LightDescr{Color: deepColors[i], Intensity: deepIntensity, Range: deepRange}))
	}
	return out
}

// appendFileLamps appends the lights station's own KHR_lights_punctual lights.
//
// They come out of the file as data, in the file's own space, and nothing in
// scene converts one: a lamp prop placed forty times would blow the cap
// silently, and which of a file's lights matter is the app's judgement. So this
// carries each one through the station's world matrix - the position through
// the matrix, the direction through its basis, and the Range through the scale,
// because a range authored in model units is a different distance once the model
// is drawn three quarters the size.
//
// A file's directional light is skipped: scene has no Component for one,
// because the single directional light it shades with is the camera's own sun.
// This file declares none, and the branch is here because a demo reading a
// file's lights as data has to decide what to do with the kind it cannot
// declare.
func appendFileLamps(out []lamp, fileLights []model.ModelLight) []lamp {
	station := &placements[stationLights]
	world := station.model.Mat4()
	for i := range fileLights {
		if fileLights[i].Directional {
			continue
		}
		descr := fileLights[i].Descr
		position := world.TransformPoint(descr.Position)
		direction := world.TransformDirection(descr.Direction)
		descr.Range *= station.scale
		if descr.Kind == model.LightSpot {
			out = append(out, spotLamp(position, direction, descr))
		} else {
			out = append(out, pointLamp(position, descr))
		}
	}
	return out
}

// pointLamp is a point light standing at position. A Light's own Position is
// ignored - the Entity's Transform places it.
func pointLamp(position m.Vec3, descr model.LightDescr) lamp {
	descr.Kind = model.LightPoint
	return lamp{Place: m.Transform{Position: position}, Light: scene.Light{Descr: descr}}
}

// spotLamp is a spot light standing at position and shining along direction. A
// spot shines down its Transform's -Z, the way m.LookAt faces, so the rotation
// is the one that looks from the lamp one step along its beam; m.LookAt falls
// back to another up for a beam that points straight down.
func spotLamp(position, direction m.Vec3, descr model.LightDescr) lamp {
	descr.Kind = model.LightSpot
	return lamp{
		Place: m.LookAt(position, position.Add(direction), m.Vec3{Y: 1}),
		Light: scene.Light{Descr: descr},
	}
}

// spread is the x of the i-th of n lamps in a row centred on the origin.
func spread(i, n int, spacing float32) float32 {
	return (float32(i) - float32(n-1)/2) * spacing
}
