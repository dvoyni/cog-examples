package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/qmuntal/gltf"
)

// wholeGLB is a packed, encoded .glb with a binary chunk in it - the shape
// truncateGLB is meant to be handed.
func wholeGLB(t *testing.T) []byte {
	t.Helper()
	doc := twoBufferDoc()
	if err := packGLB(doc, []packedImage{{Data: bytes.Repeat([]byte{9}, 64), Mime: "image/png"}}); err != nil {
		t.Fatalf("packGLB: %v", err)
	}
	var out bytes.Buffer
	encoder := gltf.NewEncoder(&out)
	encoder.AsBinary = true
	if err := encoder.Encode(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out.Bytes()
}

func TestTruncateGLBKeepsTheHeaderAndTheJSONChunk(t *testing.T) {
	whole := wholeGLB(t)
	cut, err := truncateGLB(whole)
	if err != nil {
		t.Fatalf("truncateGLB: %v", err)
	}

	if len(cut) >= len(whole) {
		t.Fatalf("cut is %d bytes of an original %d", len(cut), len(whole))
	}
	offset, length, err := glbBinChunk(whole)
	if err != nil {
		t.Fatalf("glbBinChunk: %v", err)
	}
	if !bytes.Equal(cut[:offset], whole[:offset]) {
		t.Error("everything before the binary payload should be byte-identical")
	}
	if got, want := len(cut), offset+length/2; got != want {
		t.Errorf("cut length = %d, want %d - half the binary payload", got, want)
	}
	if declared := binary.LittleEndian.Uint32(cut[8:]); int(declared) != len(whole) {
		t.Errorf("header still has to declare the original %d bytes, declares %d", len(whole), declared)
	}
}

// TestTruncateGLBFailsAfterTheHeaderParses is the contract loading demos lean
// on: this file is not garbage, it is a file that looks right until the loader
// is already committed to it.
func TestTruncateGLBFailsAfterTheHeaderParses(t *testing.T) {
	cut, err := truncateGLB(wholeGLB(t))
	if err != nil {
		t.Fatalf("truncateGLB: %v", err)
	}
	if _, _, err := glbBinChunk(cut); err == nil {
		t.Error("the cut file should no longer hold the chunk it declares")
	}
	var doc gltf.Document
	if err := gltf.NewDecoder(bytes.NewReader(cut)).Decode(&doc); err == nil {
		t.Fatal("a decoder read the truncated file without complaint")
	}
}

func TestTruncateGLBRejectsWhatIsNotAGLB(t *testing.T) {
	for name, src := range map[string][]byte{
		"empty":     nil,
		"short":     []byte("glTF"),
		"bad magic": bytes.Repeat([]byte{0}, 32),
	} {
		if _, err := truncateGLB(src); err == nil {
			t.Errorf("%s: truncateGLB accepted it", name)
		}
	}
}
