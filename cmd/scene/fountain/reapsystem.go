package main

import "github.com/dvoyni/cog/bundles/ecs"

type reapQuery struct {
	Age Life
}

// reap retires every mote whose life has run out, and tallies it.
func reap(q *ecs.Query[reapQuery], dead *ecs.WriteableEntities, state *ecs.Write[*Fountain]) {
	f := state.Get()
	for e, it := range q.All() {
		if Spent(it.Age) && dead.Despawn(e) {
			f.retired++
		}
	}
}
