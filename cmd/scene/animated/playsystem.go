package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
)

type playQuery struct {
	Clips Playlist
	Pose  *scene.Animation
}

// play writes every Playlist into its Entity's Animation at the demo clock's
// time. Every play's Time is the same number, so the whole row is on one
// timeline and a step is a step everywhere.
//
// Weights are handed over as the Playlist states them, not normalised: scene
// normalises an Entity's plays before anything is packed, because the blend is
// a weighted mean of TRS.
func play(players *ecs.Query[playQuery], state *ecs.Read[*Demo]) {
	now := state.Get().Time()
	for _, it := range players.All() {
		for i, clip := range it.Clips.Clips {
			it.Pose.Plays[i] = model.ClipPlay{}
			if clip != "" {
				it.Pose.Plays[i] = model.ClipPlay{
					Clip: clip, Time: now, Loop: true, Weight: it.Clips.Weights[i],
				}
			}
		}
	}
}
