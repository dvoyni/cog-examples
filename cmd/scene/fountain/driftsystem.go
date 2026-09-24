package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

type driftQuery struct {
	Place  *m.Transform
	Tint   *scene.Params
	Age    *Life
	Motion Velocity
}

// drift moves, stretches, ages and fades every mote.
func drift(q *ecs.Query[driftQuery]) {
	for _, it := range q.All() {
		tint := Drift(it.Place, it.Age, it.Motion)
		it.Tint.Values.Set(0, gfx.ColorParam(MoteTintParam, tint))
	}
}
