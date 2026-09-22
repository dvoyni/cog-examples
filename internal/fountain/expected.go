package fountain

import (
	"fmt"

	"github.com/dvoyni/cog/slots/gfx"
)

// CameraID is the one camera's id, in both fountains. Each declares it again
// as its renderer's own CameraID type, so the two cameras label their passes
// alike.
const CameraID = -100

// ExpectedPass is one of the camera's passes as the frame at ReferenceStep has
// it, whichever renderer drew it.
type ExpectedPass struct {
	// Label is the pass's whole label. scene and ecsscene both spell a camera's
	// pass scene.camera<ID>.<tag>, so the two fountains' labels are equal.
	Label string
	// Instances is how many instances the pass drew.
	Instances int
}

// ReferenceMotes is how many motes the frame at ReferenceStep draws. The HUD in
// that frame shows the step before it, whose census is two motes fewer.
const ReferenceMotes = 114

// ReferencePasses is the camera's passes at ReferenceStep, in run order: the
// ground pass draws the basin alone, and the forward pass every mote, the
// nozzle, the fox and the basin's ripples. Both fountains must match it whole.
var ReferencePasses = []ExpectedPass{
	{Label: passLabel(TagGround), Instances: 1},
	{Label: passLabel(TagForward), Instances: ReferenceMotes + 3},
}

// ReferenceSceneDraws is how many draws each of ReferencePasses makes when
// scene draws the frame: one for every call, since scene never merges separate
// calls.
//
// Draws are not in ReferencePasses because they may differ between the
// renderers. Two motes thrown on one step fade to the same tint, so ecsscene
// batches them where scene draws each alone. cmd/scene/fountain's draws must
// equal these, and cmd/ecs/fountain's must be no higher, pass by pass.
var ReferenceSceneDraws = []int{1, ReferenceMotes + 3}

func passLabel(tag string) string { return fmt.Sprintf("scene.camera%d.%s", CameraID, tag) }

// PassesOf is view's camera passes in the shape ReferencePasses has, so a
// fountain's test compares the two whole.
func PassesOf(view gfx.FrameView) []ExpectedPass {
	var out []ExpectedPass
	for _, pass := range CameraPasses(view) {
		out = append(out, ExpectedPass{Label: pass.Label, Instances: pass.Instances})
	}
	return out
}

// DrawsOf is how many draws each of view's camera passes made, in the shape
// ReferenceSceneDraws has.
func DrawsOf(view gfx.FrameView) []int {
	var out []int
	for _, pass := range CameraPasses(view) {
		out = append(out, pass.Draws)
	}
	return out
}
