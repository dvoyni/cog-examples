package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
)

// setup spawns the whole courtyard once, on the init event: the crates, the
// bottles, the screens, the ground, the lamp with its marker, and the camera.
// Nothing it spawns moves afterwards but the camera, which is what keeps every
// frame at a given pose the same frame.
func setup(
	crates *ecs.Spawn[crate],
	props *ecs.Spawn[prop],
	grounds *ecs.Spawn[ground],
	markers *ecs.Spawn[marker],
	lamps *ecs.Spawn[lamp],
	eyes *ecs.Spawn[eye],
	state *ecs.Read[*Demo],
) {
	// The field and the stack are one kind of Entity, spawned in two runs. The
	// stack is apart only in where it stands and how large it is, and neither
	// is in a Batch key, so its crates join the field's one draw.
	serial := int32(0)
	for _, place := range [][]m.Transform{crateTransforms, stackTransforms} {
		for i := range place {
			crates.New(crate{
				Place: place[i],
				Model: scene.Model{Ref: model.ModelRef{Path: cratePath}},
				Crate: Crate{Serial: serial},
			})
			serial++
		}
	}
	for i := range bottleTransforms {
		props.New(prop{Place: bottleTransforms[i], Model: scene.Model{Ref: model.ModelRef{Path: bottlePath}}})
	}
	for i := range paneTransforms {
		props.New(prop{Place: paneTransforms[i], Model: scene.Model{Ref: model.ModelRef{Path: panePath}}})
	}

	grounds.New(ground{Plane: scene.DebugPlane{
		Normal: m.Vec3{Y: 1}, Size: m.Vec2{X: groundSide, Y: groundSide}, Color: groundColor,
	}})
	// The lamp, and a marker on it so the light has somewhere visible to come
	// from. The marker is a debug shape, like the ground, and every debug shape
	// is self-lit, so the lamp does not light its own marker.
	lamps.New(lamp{
		Place: m.At(lampPosition.X, lampPosition.Y, lampPosition.Z),
		Light: scene.Light{Descr: model.LightDescr{
			Kind: model.LightPoint, Color: lampColor, Intensity: lampIntensity, Range: lampRange,
		}},
	})
	markers.New(marker{
		Place:  m.At(lampPosition.X, lampPosition.Y, lampPosition.Z),
		Sphere: scene.DebugSphere{Radius: lampMarkerRadius, Color: lampColor},
	})

	eyes.New(eye{Place: state.Get().place(), Camera: camera()})
}

// camera is the camera Component. Everything not written is left at its zero
// value, and every zero is the default: Projection is Perspective, CullMask is
// every layer, SunIntensity and AmbientIntensity are 1, and Passes is empty,
// which is one default pass at the camera's own id, clearing depth and keeping
// the colour canvas cleared beneath it.
func camera() scene.Camera {
	return scene.Camera{
		ID:            CameraMain,
		FovY:          fieldOfViewY,
		Near:          nearPlane,
		Far:           farPlane,
		SunDirection:  sunDirection,
		SunColor:      sunColor,
		AmbientSky:    ambientSky,
		AmbientGround: ambientGround,
	}
}
