// Command looptag writes a copy of an Ogg Vorbis file carrying LOOPSTART and
// LOOPLENGTH, keeping the file's own comments and every audio page byte for
// byte, and refuses to write it unless the copy provably decodes to the same
// frame count as its source.
//
//	go run ./cmd/looptag -in SOURCE.ogg -out TAGGED.ogg -loopstart FRAMES -looplength FRAMES [-cut FRAMES]
//
// Every number is in the source's sample frames, which is what the tags hold.
// -cut first shortens the source to the first page boundary at or past that
// frame, without re-encoding anything; it is how a clip a few seconds long is
// taken from a whole recording, and the tagging is then checked against that
// cut.
//
// It is a tool beside the demos rather than a demo: it is one level under
// cmd/, so the browser build does not walk it. The logic is internal/looptag,
// where the demo that consumes its output can check that output against the
// recording it came from. cmd/sound/loopregion's ATTRIBUTION.md records the
// exact command its clip was made with.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dvoyni/cog-examples/internal/looptag"
)

func main() {
	in := flag.String("in", "", "the Ogg Vorbis file to tag")
	out := flag.String("out", "", "where to write the tagged copy")
	start := flag.Int64("loopstart", -1, "LOOPSTART, in sample frames")
	length := flag.Int64("looplength", 0, "LOOPLENGTH, in sample frames")
	cut := flag.Int64("cut", 0, "if set, first cut the source at the first page boundary at or past this frame")
	flag.Parse()
	if *in == "" || *out == "" || *start < 0 || *length <= 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, looptag.Region{Start: *start, Length: *length}, *cut); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in, out string, region looptag.Region, cut int64) error {
	source, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	if cut > 0 {
		if source, err = looptag.Cut(source, cut); err != nil {
			return err
		}
	}
	tagged, err := looptag.Tag(source, region)
	if err != nil {
		return err
	}
	report, err := looptag.Verify(source, tagged, region)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, tagged, 0o644); err != nil {
		return err
	}

	rate := float64(report.SampleRate)
	fmt.Printf("looptag: wrote %s, %d bytes\n", out, len(tagged))
	fmt.Printf("  frames      %d decoded from the tagged file, %d from its source (%.4f s, %d Hz, %d channels)\n",
		report.Frames, report.Frames, float64(report.Frames)/rate, report.SampleRate, report.Channels)
	fmt.Printf("  pages       %d of %d byte-identical to the source; the other is the comment header's\n",
		report.Verbatim, report.Pages)
	fmt.Printf("  region      [%d, %d) = [%.4f s, %.4f s): an intro of %.4f s, a loop of %.4f s\n",
		report.Region.Start, report.Region.End(),
		float64(report.Region.Start)/rate, float64(report.Region.End())/rate,
		float64(report.Region.Start)/rate, float64(report.Region.Length)/rate)
	fmt.Println("  comments")
	for _, comment := range report.Comments {
		fmt.Printf("    %q\n", comment)
	}
	return nil
}
