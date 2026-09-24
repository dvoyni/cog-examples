package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// crateSerialParam is the parameter key 2 writes into each crate's Params. No
// shader declares it, so gfx binds it nowhere and the picture is untouched; its
// whole effect is on the Batch key, which hashes a Params value whatever its
// names mean to a shader.
const crateSerialParam = "crateSerial"

type splitQuery struct {
	Tint  *scene.Params
	Crate Crate
}

// split writes the crates' Params when the mode the keys asked for is not the
// one they were last written in, and does nothing otherwise. A written Params
// is a changed Component, which the load System rekeys in the same step.
//
// Sharing is an empty Params, which hashes as none, so every crate is one key.
// Splitting is a Params naming the crate's serial, so every crate is a key of
// its own and a Batch of one.
func split(crates *ecs.Query[splitQuery], state *ecs.Write[*Demo]) {
	d := state.Get()
	if d.perCrate == d.split {
		return
	}
	for _, it := range crates.All() {
		if d.perCrate {
			*it.Tint = scene.Params{Values: m.NewList(
				gfx.FloatParam(crateSerialParam, float32(it.Crate.Serial)))}
		} else {
			*it.Tint = scene.Params{}
		}
	}
	d.split = d.perCrate
}
