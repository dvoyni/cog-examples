package main

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

type spinQuery struct {
	Place *m.Transform
	Spin  *Spin
}

// spinSystem turns every Spin Entity about Y at a radian a second of wall
// time. The spin is there to show that every face of the cube is where it
// should be; the frame is otherwise the same forever.
func spinSystem(q *ecs.Query[spinQuery], dt *ecs.In[float64]) {
	step := float32(dt.Get())
	for _, it := range q.All() {
		it.Spin.Angle += step
		it.Place.Rotation = m.QuatAxisAngle(m.Vec3{Y: 1}, it.Spin.Angle)
	}
}
