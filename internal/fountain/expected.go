package fountain

import "github.com/dvoyni/cog/slots/gfx"

// ExpectedPass is one of the camera's passes as the frame at ReferenceStep has
// it, whichever renderer drew it.
type ExpectedPass struct {
	// Tag is the tag the pass's label ends in.
	Tag string
	// Instances is how many instances the pass drew.
	Instances int
}

// ReferenceMotes is how many motes the frame at ReferenceStep draws. The HUD in
// that frame shows the step before it, whose census is two motes fewer.
const ReferenceMotes = 114

// ReferencePasses is the camera's passes at ReferenceStep, in run order: the
// ground pass draws the basin alone, and the forward pass every mote, the
// nozzle, the fox and the basin's ripples.
//
// Draws are not here. They may differ between the renderers: two motes thrown
// on one step fade to the same tint, so ecsscene may batch them where scene
// never merges separate calls. What must be equal is the passes, their labels
// and the instances each draws.
var ReferencePasses = []ExpectedPass{
	{Tag: TagGround, Instances: 1},
	{Tag: TagForward, Instances: ReferenceMotes + 3},
}

// PassesOf is view's camera passes in the shape ReferencePasses has, so a
// fountain's test compares the two whole.
func PassesOf(view gfx.FrameView) []ExpectedPass {
	var out []ExpectedPass
	for _, pass := range CameraPasses(view) {
		out = append(out, ExpectedPass{Tag: PassTag(pass.Label), Instances: pass.Instances})
	}
	return out
}
