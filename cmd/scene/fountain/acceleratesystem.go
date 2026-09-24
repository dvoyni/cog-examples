package main

import "github.com/dvoyni/cog/bundles/ecs"

type fallQuery struct {
	Motion *Velocity
}

// accelerate pulls every mote down by one step of gravity.
func accelerate(q *ecs.Query[fallQuery]) {
	for _, it := range q.All() {
		Fall(it.Motion)
	}
}
