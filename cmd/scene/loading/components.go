package main

// Station marks an Entity as one slot of the grid, by its index in the station
// table. It is the one Component this demo declares: everything else a station
// carries - its Transform, its Model, its Params or Material - is scene's.
//
// The index is what lets the unload System find the Models a lever freed:
// scene's Model holds the path, and the table says which stations name it.
type Station struct {
	Index int
}
