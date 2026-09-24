package main

import "github.com/dvoyni/cog/bundles/ecs"

type contrastQuery struct {
	Clips    *Playlist
	Contrast Contrast
}

// contrast writes the stress station's second copy's first clip: its own while
// the demo's contrast is on, and its neighbour's while it is off. That is the
// A/B stated as a picture - two copies of one file disagreeing, then moving
// together - and the draw count does not change either way, because which clip
// an Entity plays is not a draw.
func contrast(copies *ecs.Query[contrastQuery], state *ecs.Read[*Demo]) {
	on := state.Get().contrast
	for _, it := range copies.All() {
		it.Clips.Clips[0] = stressClip
		if on {
			it.Clips.Clips[0] = it.Contrast.Clip
		}
	}
}
