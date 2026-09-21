// Package looptag writes a Loop Region into an Ogg Vorbis file, once, so that a
// demo or a test that wants a looping Clip can commit the result instead of
// doing Ogg surgery in its own code.
//
// A Loop Region is a fact about a Clip: it rides in the Vorbis comment header
// as LOOPSTART and LOOPLENGTH, in sample frames, and every sound Adapter reads
// it in the pass that already reads the duration, the channels and the rate.
// Every clip in this module and in cog was untagged, so the only tagged streams
// anywhere were the ones cog's Adapter tests build in memory - three copies of
// the same arithmetic, one per Extension, because an Extension's internal/ may
// not reach another's.
//
// This is the fourth place the format is understood, and it lives here rather
// than in cog on purpose. It is not an Adapter and it runs at authoring time,
// not in a game: nothing a game composes calls it, and the only thing it hands
// the engine is a file. Moving it into cog would give the three Adapter copies
// something to import - which is the architecture decision cog#483 recorded and
// did not take - and a tool for preparing fixtures is not the reason to take it.
//
// The re-tagging is not hard, just exacting: rebuild the comment packet keeping
// the file's own comments, re-lay the segment table of the one page that packet
// sits in, recompute that page's CRC, and copy every other page verbatim, so the
// audio, its granule positions and its length are untouched. [Verify] is what
// proves that of a given pair of files rather than asserting it of the code.
package looptag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jfreymuth/oggvorbis"
)

// The Vorbis comments a Loop Region is declared with. The field names of a
// Vorbis comment are case-insensitive by the format's own definition; these are
// written in the upper case every tool in the wild reads.
const (
	LoopStartTag  = "LOOPSTART"
	LoopLengthTag = "LOOPLENGTH"
	LoopEndTag    = "LOOPEND"
)

// Region is a Loop Region in the source's own sample frames: a looping Voice
// plays from the start of the Clip, and on reaching Start+Length goes back to
// Start rather than to zero, so everything before Start is an intro heard once.
type Region struct {
	Start, Length int64
}

// End is the first frame after the region.
func (r Region) End() int64 { return r.Start + r.Length }

// Tag returns a copy of an Ogg Vorbis stream whose comment header declares the
// given region.
//
// The file's own comments are kept, in their order, with one exception: any
// LOOPSTART, LOOPLENGTH or LOOPEND it already carried is dropped, because a
// file that named two regions would loop the way whichever tag an Adapter met
// first said, and the one being written is the one meant. The new tags go at
// the end.
//
// Every page but the one the comment header sits in is copied byte for byte.
// Tag checks the region against the stream's length first and refuses one an
// Adapter would drop: a region that is empty, or that ends past the last frame.
func Tag(stream []byte, region Region) ([]byte, error) {
	frames, _, err := oggvorbis.GetLength(bytes.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("looptag: the source is not Ogg Vorbis: %w", err)
	}
	if region.Start < 0 || region.Length <= 0 || region.End() > frames {
		return nil, fmt.Errorf("looptag: region [%d, %d) is not inside the stream's %d frames, and an Adapter would drop it",
			region.Start, region.End(), frames)
	}
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("looptag: reading the comment header: %w", err)
	}
	var comments []string
	for _, comment := range header.Comments {
		if !isLoopTag(comment) {
			comments = append(comments, comment)
		}
	}
	comments = append(comments,
		LoopStartTag+"="+strconv.FormatInt(region.Start, 10),
		LoopLengthTag+"="+strconv.FormatInt(region.Length, 10))
	return replaceCommentHeader(stream, commentPacket(header.Vendor, comments))
}

// Cut returns the stream up to and including the first page whose granule
// position reaches frames and on which no packet runs over into the next, with that page marked end-of-stream and its CRC
// recomputed. It is how a short clip is taken from a long recording without
// re-encoding anything: the pages kept are the pages published, and the one
// page that changes changes in its header flag and checksum and nowhere else.
//
// The result's length is that page's granule position, which is frames or a
// little past it: an Ogg stream can only be cut where a page ends, and only
// where a packet ends with it.
func Cut(stream []byte, frames int64) ([]byte, error) {
	if frames <= 0 {
		return nil, fmt.Errorf("looptag: cannot cut a stream at %d frames", frames)
	}
	for page, err := range pages(stream) {
		if err != nil {
			return nil, err
		}
		granule := int64(binary.LittleEndian.Uint64(page.bytes[6:14]))
		// A page whose last segment is 255 hands its final packet on to the
		// next page, and a stream cannot end inside a packet, so the cut moves
		// on to the first page that both reaches the frame and ends cleanly.
		if granule == -1 || granule < frames || page.table[len(page.table)-1] == 255 {
			continue
		}
		out := append([]byte(nil), stream[:page.at+len(page.bytes)]...)
		last := out[page.at:]
		last[5] |= 0x04 // end of stream
		clear(last[22:26])
		binary.LittleEndian.PutUint32(last[22:26], crc(last))
		return out, nil
	}
	return nil, fmt.Errorf("looptag: the stream ends before frame %d", frames)
}

// ReadRegion reads the Loop Region a stream declares, the way the Adapters do:
// case-insensitively, the first of each tag winning, LOOPLENGTH beating LOOPEND
// and LOOPSTART alone running to the end. It reports false for a stream that
// declares none.
func ReadRegion(stream []byte) (Region, bool, error) {
	header, err := oggvorbis.GetCommentHeader(bytes.NewReader(stream))
	if err != nil {
		return Region{}, false, err
	}
	start, hasStart, err1 := tag(header.Comments, LoopStartTag)
	length, hasLength, err2 := tag(header.Comments, LoopLengthTag)
	end, hasEnd, err3 := tag(header.Comments, LoopEndTag)
	if err := errors.Join(err1, err2, err3); err != nil {
		return Region{}, false, err
	}
	if !hasStart && !hasLength && !hasEnd {
		return Region{}, false, nil
	}
	switch {
	case hasLength:
	case hasEnd:
		length = end - start
	default:
		frames, _, err := oggvorbis.GetLength(bytes.NewReader(stream))
		if err != nil {
			return Region{}, false, err
		}
		length = frames - start
	}
	return Region{Start: start, Length: length}, true, nil
}

// Report is what [Verify] established about a tagged file and its source.
type Report struct {
	// Frames is how many sample frames both files decode to.
	Frames int64
	// SampleRate and Channels are the stream's, the same in both.
	SampleRate, Channels int
	// Pages is how many pages each file has, and Verbatim how many of them are
	// byte-identical between the two - every page but the comment header's.
	Pages, Verbatim int
	// Region is the Loop Region the tagged file reads back as.
	Region Region
	// Comments is the tagged file's comment list, in order.
	Comments []string
}

// Verify proves a tagged file is its source with a Loop Region added and
// nothing else changed. It decodes both to the last sample and compares the
// frame counts, which is the check that the tagging was non-destructive; it
// compares the two page by page and requires every page but the comment
// header's to be identical; it requires the source's own comments to survive in
// order; and it reads the region back.
func Verify(source, tagged []byte, want Region) (Report, error) {
	var report Report
	sourceFrames, rate, channels, err := decodedFrames(source)
	if err != nil {
		return report, fmt.Errorf("looptag: decoding the source: %w", err)
	}
	taggedFrames, taggedRate, taggedChannels, err := decodedFrames(tagged)
	if err != nil {
		return report, fmt.Errorf("looptag: decoding the tagged file: %w", err)
	}
	if taggedFrames != sourceFrames || taggedRate != rate || taggedChannels != channels {
		return report, fmt.Errorf("looptag: the tagged file decodes to %d frames at %d Hz in %d channels, and its source to %d, %d, %d",
			taggedFrames, taggedRate, taggedChannels, sourceFrames, rate, channels)
	}
	report.Frames, report.SampleRate, report.Channels = taggedFrames, rate, channels

	sourcePages, err := collect(source)
	if err != nil {
		return report, err
	}
	taggedPages, err := collect(tagged)
	if err != nil {
		return report, err
	}
	if len(sourcePages) != len(taggedPages) {
		return report, fmt.Errorf("looptag: the tagged file has %d pages and its source %d", len(taggedPages), len(sourcePages))
	}
	report.Pages = len(taggedPages)
	for i := range sourcePages {
		if bytes.Equal(sourcePages[i], taggedPages[i]) {
			report.Verbatim++
		} else if !containsCommentHeader(taggedPages[i]) {
			return report, fmt.Errorf("looptag: page %d differs from its source and carries no comment header", i)
		}
	}
	if report.Verbatim != report.Pages-1 {
		return report, fmt.Errorf("looptag: %d of %d pages differ from the source, and only the comment header's may", report.Pages-report.Verbatim, report.Pages)
	}

	sourceHeader, err := oggvorbis.GetCommentHeader(bytes.NewReader(source))
	if err != nil {
		return report, err
	}
	taggedHeader, err := oggvorbis.GetCommentHeader(bytes.NewReader(tagged))
	if err != nil {
		return report, err
	}
	report.Comments = taggedHeader.Comments
	if taggedHeader.Vendor != sourceHeader.Vendor {
		return report, fmt.Errorf("looptag: the vendor string became %q, from %q", taggedHeader.Vendor, sourceHeader.Vendor)
	}
	kept := taggedHeader.Comments
	for _, comment := range sourceHeader.Comments {
		if isLoopTag(comment) {
			continue
		}
		at := indexOf(kept, comment)
		if at < 0 {
			return report, fmt.Errorf("looptag: the source's comment %q did not survive, or not in order", comment)
		}
		kept = kept[at+1:]
	}

	region, ok, err := ReadRegion(tagged)
	if err != nil || !ok {
		return report, fmt.Errorf("looptag: the tagged file reads back no region (%v)", err)
	}
	report.Region = region
	if region != want {
		return report, fmt.Errorf("looptag: the tagged file reads back [%d, %d), want [%d, %d)", region.Start, region.End(), want.Start, want.End())
	}
	if region.End() > taggedFrames {
		return report, fmt.Errorf("looptag: the region ends at %d, past the stream's %d frames", region.End(), taggedFrames)
	}
	return report, nil
}

// decodedFrames decodes a stream to its last sample and counts the frames,
// rather than trusting the granule position the header pass reads: the check is
// that the audio is all there, and only decoding it says so.
func decodedFrames(stream []byte) (frames int64, rate, channels int, err error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(stream))
	if err != nil {
		return 0, 0, 0, err
	}
	buffer := make([]float32, 8192*reader.Channels())
	for {
		n, err := reader.Read(buffer)
		frames += int64(n / reader.Channels())
		if err == io.EOF {
			return frames, reader.SampleRate(), reader.Channels(), nil
		}
		if err != nil {
			return 0, 0, 0, err
		}
	}
}

func isLoopTag(comment string) bool {
	name, _, _ := strings.Cut(comment, "=")
	return strings.EqualFold(name, LoopStartTag) ||
		strings.EqualFold(name, LoopLengthTag) ||
		strings.EqualFold(name, LoopEndTag)
}

// tag reads one loop comment in frames, first match winning, as the Adapters do.
func tag(comments []string, name string) (int64, bool, error) {
	for _, comment := range comments {
		field, value, ok := strings.Cut(comment, "=")
		if !ok || !strings.EqualFold(field, name) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return n, true, err
	}
	return 0, false, nil
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// page is one Ogg page: where it starts in the stream, the whole of it, and its
// segment table.
type page struct {
	at    int
	bytes []byte
	table []byte
}

// pages walks a stream page by page and refuses anything that is not a run of
// whole Ogg pages.
func pages(stream []byte) func(yield func(page, error) bool) {
	return func(yield func(page, error) bool) {
		for at := 0; at < len(stream); {
			if at+27 > len(stream) || string(stream[at:at+4]) != "OggS" {
				yield(page{}, fmt.Errorf("looptag: not an Ogg page at byte %d", at))
				return
			}
			head := 27 + int(stream[at+26])
			if at+head > len(stream) {
				yield(page{}, fmt.Errorf("looptag: a truncated page at byte %d", at))
				return
			}
			table := stream[at+27 : at+head]
			body := 0
			for _, segment := range table {
				body += int(segment)
			}
			if at+head+body > len(stream) {
				yield(page{}, fmt.Errorf("looptag: a truncated page at byte %d", at))
				return
			}
			if !yield(page{at: at, bytes: stream[at : at+head+body], table: table}, nil) {
				return
			}
			at += head + body
		}
	}
}

func collect(stream []byte) ([][]byte, error) {
	var out [][]byte
	for page, err := range pages(stream) {
		if err != nil {
			return nil, err
		}
		out = append(out, page.bytes)
	}
	return out, nil
}

// packets splits one page's body into the packets that end on it, by its
// segment table: a segment shorter than 255 ends a packet. It yields each
// packet's first and last segment index and its byte span in the body, and a
// final span with last == -1 for a packet that runs on to the next page.
func packets(table []byte, yield func(first, last, from, to int) bool) {
	first, from, offset := 0, 0, 0
	for i, segment := range table {
		offset += int(segment)
		if segment == 255 {
			continue
		}
		if !yield(first, i, from, offset) {
			return
		}
		first, from = i+1, offset
	}
	if first < len(table) {
		yield(first, -1, from, offset)
	}
}

// isCommentHeader reports whether a packet begins the Vorbis comment header:
// packet type 3 and the codec's signature.
func isCommentHeader(packet []byte) bool {
	return len(packet) >= 7 && packet[0] == 3 && string(packet[1:7]) == "vorbis"
}

func containsCommentHeader(p []byte) bool {
	head := 27 + int(p[26])
	found := false
	packets(p[27:head], func(_, _, from, to int) bool {
		found = isCommentHeader(p[head+from : head+to])
		return !found
	})
	return found
}

// replaceCommentHeader swaps the comment header packet for a new one and re-lays
// the one page it sits in: a fresh segment table for the packet, the segments
// and bodies either side of it kept exactly as they were, and the page's CRC
// recomputed. The page count, the sequence numbers and every other page are
// left alone.
func replaceCommentHeader(stream []byte, packet []byte) ([]byte, error) {
	for page, err := range pages(stream) {
		if err != nil {
			return nil, err
		}
		// A page that opens with a continued packet does not start one there,
		// so its first span is the tail of something else.
		continued := page.bytes[5]&0x01 != 0
		head := 27 + len(page.table)
		var out []byte
		var failure error
		packets(page.table, func(first, last, from, to int) bool {
			if first == 0 && continued {
				return true
			}
			if !isCommentHeader(page.bytes[head+from : head+to]) {
				return true
			}
			if last < 0 {
				failure = errors.New("looptag: the comment header runs across a page boundary, which this tool does not re-lay")
				return false
			}
			segments := append([]byte(nil), page.table[:first]...)
			segments = append(segments, lacing(len(packet))...)
			segments = append(segments, page.table[last+1:]...)
			if len(segments) > 255 {
				failure = fmt.Errorf("looptag: the new comment header needs %d segments and a page holds 255", len(segments))
				return false
			}
			relaid := append([]byte(nil), page.bytes[:27]...)
			relaid[26] = byte(len(segments))
			relaid = append(relaid, segments...)
			relaid = append(relaid, page.bytes[head:head+from]...)
			relaid = append(relaid, packet...)
			relaid = append(relaid, page.bytes[head+to:]...)
			clear(relaid[22:26])
			binary.LittleEndian.PutUint32(relaid[22:26], crc(relaid))

			out = append([]byte(nil), stream[:page.at]...)
			out = append(out, relaid...)
			out = append(out, stream[page.at+len(page.bytes):]...)
			return false
		})
		if failure != nil {
			return nil, failure
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, errors.New("looptag: the stream carries no Vorbis comment header")
}

// commentPacket builds a Vorbis comment header: the type byte and signature,
// the vendor string, the comments, and the framing bit.
func commentPacket(vendor string, comments []string) []byte {
	packet := append([]byte{3}, "vorbis"...)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(vendor)))
	packet = append(packet, vendor...)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comments)))
	for _, comment := range comments {
		packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comment)))
		packet = append(packet, comment...)
	}
	return append(packet, 1)
}

// lacing is a packet length as Ogg segment sizes: as many 255s as it takes and
// then the remainder, which is what ends the packet.
func lacing(length int) []byte {
	var segments []byte
	for length >= 255 {
		segments = append(segments, 255)
		length -= 255
	}
	return append(segments, byte(length))
}

// crc is the checksum an Ogg page carries, computed with its own CRC field
// zeroed: the 32-bit polynomial 0x04c11db7, unreflected, with no initial or
// final inversion.
func crc(page []byte) uint32 {
	var sum uint32
	for _, b := range page {
		sum ^= uint32(b) << 24
		for range 8 {
			if sum&0x80000000 != 0 {
				sum = sum<<1 ^ 0x04c11db7
			} else {
				sum <<= 1
			}
		}
	}
	return sum
}
