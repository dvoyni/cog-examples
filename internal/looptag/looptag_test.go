package looptag

import (
	"bytes"
	"os"
	"testing"

	"github.com/jfreymuth/oggvorbis"
)

// The fixture is the 1.1 s public-domain cut the orbit demo embeds: 48704
// frames of stereo 44.1 kHz, with the recording's own seven comments.
const (
	fixturePath   = "../../cmd/sound/orbit/pianoroll.ogg"
	fixtureFrames = 48704
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return data
}

// The whole claim of the tool: the tagged file decodes to exactly as many
// frames as its source, every page but the comment header's is the source's
// byte for byte, the file's own comments survive in order, and the region reads
// back as written.
func TestATaggedClipIsItsSourceWithARegionAndNothingElse(t *testing.T) {
	source := fixture(t)
	region := Region{Start: 11025, Length: 22050}
	tagged, err := Tag(source, region)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Verify(source, tagged, region)
	if err != nil {
		t.Fatal(err)
	}
	if report.Frames != fixtureFrames {
		t.Fatalf("both decode to %d frames, want the fixture's %d", report.Frames, fixtureFrames)
	}
	if report.Verbatim != report.Pages-1 {
		t.Fatalf("%d of %d pages verbatim, want all but one", report.Verbatim, report.Pages)
	}
	// Decoded sample for sample, not only counted: the audio is the same audio.
	a, _, err := oggvorbis.ReadAll(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := oggvorbis.ReadAll(bytes.NewReader(tagged))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("decoded %d samples from the tagged file and %d from its source", len(b), len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d is %v in the tagged file and %v in its source", i, b[i], a[i])
		}
	}
	// The new tags go last, after the recording's own.
	n := len(report.Comments)
	if n < 2 || report.Comments[n-2] != "LOOPSTART=11025" || report.Comments[n-1] != "LOOPLENGTH=22050" {
		t.Fatalf("the comments end %q, want the two loop tags", report.Comments)
	}
}

// Re-tagging a tagged file replaces its region rather than adding a second one
// an Adapter might read first.
func TestRetaggingReplacesTheRegion(t *testing.T) {
	source := fixture(t)
	once, err := Tag(source, Region{Start: 100, Length: 200})
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Tag(once, Region{Start: 11025, Length: 22050})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Verify(source, twice, Region{Start: 11025, Length: 22050})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range report.Comments {
		if isLoopTag(c) {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("the twice-tagged file carries %d loop tags, want 2: %q", count, report.Comments)
	}
}

// A region an Adapter would drop is refused at authoring time rather than
// committed: a clamped or dropped loop sounds like a working loop with the
// wrong loop point.
func TestTagRefusesARegionAnAdapterWouldDrop(t *testing.T) {
	source := fixture(t)
	for _, region := range []Region{
		{Start: -1, Length: 100},
		{Start: 100, Length: 0},
		{Start: 11025, Length: fixtureFrames},
	} {
		if _, err := Tag(source, region); err == nil {
			t.Errorf("Tag accepted %+v on a %d frame stream", region, fixtureFrames)
		}
	}
}

// Verify is the check, so it has to fail when there is something to catch: a
// file whose audio page was touched is not a tagging of its source.
func TestVerifyCatchesATouchedAudioPage(t *testing.T) {
	source := fixture(t)
	region := Region{Start: 11025, Length: 22050}
	tagged, err := Tag(source, region)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := collect(tagged)
	if err != nil {
		t.Fatal(err)
	}
	last := len(tagged) - len(pages[len(pages)-1])
	damaged := append([]byte(nil), tagged...)
	damaged[last+5] &^= 0x04 // unmark end of stream: one flag bit, CRC left stale
	if _, err := Verify(source, damaged, region); err == nil {
		t.Fatal("Verify passed a file whose last audio page differs from its source")
	}
	if _, err := Verify(source, tagged, Region{Start: 11025, Length: 1}); err == nil {
		t.Fatal("Verify passed a region other than the one asked for")
	}
}

// Cut keeps whole pages up to the first one that reaches the frame asked for
// and ends on a packet boundary. In the fixture the pages that end at frames
// 14912 and 31296 both hand a packet on to the next page, so a cut anywhere
// short of the end lands on the fixture's own last page - which is already
// marked end of stream, so the cut is the fixture byte for byte.
//
// The cut that matters, the demo clip's, is proved in cmd/sound/loopregion
// against the full recording it was taken from.
func TestCutEndsOnlyWhereAPacketEnds(t *testing.T) {
	source := fixture(t)
	for _, frames := range []int64{1, 14912, 20000, fixtureFrames} {
		cut, err := Cut(source, frames)
		if err != nil {
			t.Fatalf("Cut at %d: %v", frames, err)
		}
		if !bytes.Equal(cut, source) {
			t.Fatalf("Cut at %d is %d bytes, want the fixture's own %d", frames, len(cut), len(source))
		}
	}
	if _, err := Cut(source, fixtureFrames+1); err == nil {
		t.Fatal("Cut past the end of the stream succeeded")
	}
}
