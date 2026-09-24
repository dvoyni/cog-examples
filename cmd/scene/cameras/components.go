package main

// The demo's own Components. Everything drawn is one of scene's; these only say
// which Entity plays which part, so the Systems can find it.

// Cube marks one corridor cube, by its index into cubes: its name, its colour
// and its place are the table's, and the tint System reads the name to decide
// whether the cube wears the highlight.
type Cube struct {
	Index int
}

// Rider marks an Entity that stands wherever the main camera stands: the
// camera itself, and the eye and the frustum outline the minimap shows. The
// track System moves them together, so the outline is baked once in the
// camera's own space and flies with it rather than being rebuilt every frame.
type Rider struct{}

// Outline marks the wire box drawn around whatever the last click picked.
type Outline struct{}

// Impostor marks the second camera that holds the main camera's id while D is
// held.
type Impostor struct{}
