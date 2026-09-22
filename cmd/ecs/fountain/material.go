package main

import (
	"github.com/dvoyni/cog-examples/internal/fountain"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/libs/m"
)

// tagGround is the camera's first pass. Only the basin's material has an entry
// for it, so it draws the basin alone, beneath everything the forward pass
// draws.
const tagGround = ecsscene.PassTag(fountain.TagGround)

// sharedMote is the one mote material. Every mote's Material Component holds
// the same value, so ecsscene keys them to one material and batches equal tints, and the per-mote colour
// rides in Params instead: a colour inside the Material would make every mote a
// material of its own, fading every frame.
var sharedMote = ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{
	Tag:    ecsscene.TagForward,
	Shader: fountain.MoteShader(),
	State:  fountain.MoteState(),
})}

func moteMaterial() ecsscene.Material { return sharedMote }

// basinMaterial serves two pass tags: lit stone in the ground pass, and
// ripples blended over it in the forward pass, where the motes and the fox
// occlude them.
func basinMaterial() ecsscene.Material {
	return ecsscene.Material{Tags: m.NewList(
		ecsscene.MaterialTag{
			Tag:    tagGround,
			Shader: fountain.StoneShader(),
			State:  fountain.StoneState(),
		},
		ecsscene.MaterialTag{
			Tag:    ecsscene.TagForward,
			Shader: fountain.RippleShader(),
			State:  fountain.RippleState(),
		},
	)}
}
