package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/dvoyni/cog-examples/internal/looptag"
	"github.com/jfreymuth/oggvorbis"
)

// The committed clip's facts, as ATTRIBUTION.md records them.
const (
	clipFrames = 229568
	// sourcePath is the whole recording the clip was cut from, vendored at
	// assets/music by the mixer demo's branch.
	sourcePath = "../../../assets/music/pleasant-moments.ogg"
)

// The bar the demo draws is its own constants, because sound never shows a game
// a Clip's region. This is what keeps those constants honest: the clip's own
// tags read back as exactly the region the demo says it has, and that region
// sits inside the clip with an intro of about a second and a loop of a few.
func TestTheClipDeclaresTheRegionTheDemoDraws(t *testing.T) {
	region, ok, err := looptag.ReadRegion(clipOgg)
	if err != nil || !ok {
		t.Fatalf("the clip declares no Loop Region (%v)", err)
	}
	if want := (looptag.Region{Start: loopStart, Length: loopLength}); region != want {
		t.Fatalf("the clip's tags say %+v, and the demo draws %+v", region, want)
	}
	length, format, err := oggvorbis.GetLength(bytes.NewReader(clipOgg))
	if err != nil {
		t.Fatal(err)
	}
	if length != clipFrames || format.SampleRate != clipRate {
		t.Fatalf("the clip is %d frames at %d Hz, want %d at %d", length, format.SampleRate, clipFrames, clipRate)
	}
	if region.End() > length {
		t.Fatalf("the region ends at frame %d, past the clip's %d", region.End(), length)
	}
	if intro := loopFrom; intro < 0.5 || intro > 1.5 {
		t.Fatalf("the intro is %.3fs, and a wrap nobody can hear is not a demo", intro)
	}
	if loop := loopTo - loopFrom; loop < 2 || loop > 6 {
		t.Fatalf("the loop is %.3fs, want a few seconds", loop)
	}
}

// The clip is cmd/looptag's output over the public-domain recording, with the
// parameters ATTRIBUTION.md records: cut at the loop end, then tagged. Rebuilt
// here from the recording it must come out byte for byte, and it must decode to
// the same frame count as the untagged cut it was tagged from, which is the
// proof the tagging destroyed nothing.
//
// The recording is vendored at assets/music by another branch; until that lands
// in a checkout the test says so and skips rather than failing on a file this
// demo does not own.
func TestTheClipIsTheToolsOutputOverTheRecording(t *testing.T) {
	recording, err := os.ReadFile(sourcePath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is not in this checkout; it arrives with the mixer demo", sourcePath)
	}
	if err != nil {
		t.Fatal(err)
	}
	region := looptag.Region{Start: loopStart, Length: loopLength}
	cut, err := looptag.Cut(recording, region.End())
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := looptag.Tag(cut, region)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tagged, clipOgg) {
		t.Fatalf("rebuilding the clip from the recording gives %d bytes that differ from the committed %d",
			len(tagged), len(clipOgg))
	}
	report, err := looptag.Verify(cut, clipOgg, region)
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != clipFrames {
		t.Fatalf("the clip and its untagged cut both decode to %d frames, want %d", report.Frames, clipFrames)
	}
}
