package main

import "github.com/dvoyni/cog/bundles/model"

// The demo's own Components. scene's Model, Animation and Mesh draw the row;
// these three say what fills each Animation, and nothing else. They are plain
// data: the Systems named beside each one are what act on them.

// Playlist is the clips an Entity plays on the demo clock, looping, each at its
// own weight. play copies it into the Entity's Animation every tick with the
// clock's time. A slot whose Clip is empty is unused, as in Animation.
//
// It has scene.Animation's own length, so an Entity cannot be offered more
// plays than it can blend: the cap copy's nine offers are cut to four by setup,
// by the rule model applies to a draw.
type Playlist struct {
	Clips   [model.MaxClipPlays]string
	Weights [model.MaxClipPlays]float32
}

// Gait is the fox's gait machine, which gait steps to the clock and copies into
// the fox's Animation.
//
// Start is the machine as rig built it, which a rewind starts again from, and
// Step the demo step Machine stands at. Both are zero until Rigged, which rig
// sets the first tick Fox.glb answers Clips. Last is the most recent state the
// machine entered and the step it entered it at, which the HUD prints.
type Gait struct {
	Machine model.ClipMachine
	Start   model.ClipMachine
	Step    int
	Rigged  bool
	Last    string
}

// Contrast marks the stress station's second copy, and names the clip it plays
// in place of its Playlist's first while the demo's contrast is on. contrast
// writes one or the other into the Playlist, so with the contrast off (key o)
// the two copies play the same clip and move together.
type Contrast struct {
	Clip string
}
