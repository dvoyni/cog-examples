package main

import (
	"bytes"
	"testing"

	"github.com/qmuntal/gltf"
)

// twoBufferDoc is a document in the shape a loose glTF variant arrives in: more
// than one external buffer, views spread across them, and an image that is
// still a file of its own.
func twoBufferDoc() *gltf.Document {
	doc := gltf.NewDocument()
	doc.Buffers = []*gltf.Buffer{
		{ByteLength: 6, Data: []byte{1, 2, 3, 4, 5, 6}},
		{ByteLength: 4, Data: []byte{7, 8, 9, 10}},
	}
	doc.BufferViews = []*gltf.BufferView{
		{Buffer: 0, ByteOffset: 2, ByteLength: 4},
		{Buffer: 1, ByteOffset: 0, ByteLength: 4},
	}
	doc.Images = []*gltf.Image{{URI: "tex.png"}}
	return doc
}

func TestPackGLBCollapsesEveryBufferIntoOne(t *testing.T) {
	doc := twoBufferDoc()
	if err := packGLB(doc, []packedImage{{Data: []byte{11, 12, 13}, Mime: "image/png"}}); err != nil {
		t.Fatalf("packGLB: %v", err)
	}

	if len(doc.Buffers) != 1 {
		t.Fatalf("buffers = %d, want 1", len(doc.Buffers))
	}
	if doc.Buffers[0].URI != "" {
		t.Errorf("buffer URI = %q, want empty so the encoder writes a BIN chunk", doc.Buffers[0].URI)
	}
	if got := doc.Buffers[0].ByteLength; got != len(doc.Buffers[0].Data) {
		t.Errorf("ByteLength = %d, data = %d", got, len(doc.Buffers[0].Data))
	}

	// Buffer 0 is six bytes, so buffer 1 starts at the next 4-byte boundary.
	if got, want := doc.BufferViews[0].ByteOffset, 2; got != want {
		t.Errorf("view 0 offset = %d, want %d", got, want)
	}
	if got, want := doc.BufferViews[1].ByteOffset, 8; got != want {
		t.Errorf("view 1 offset = %d, want %d", got, want)
	}
	for i, view := range doc.BufferViews {
		if view.Buffer != 0 {
			t.Errorf("view %d points at buffer %d, want 0", i, view.Buffer)
		}
		if view.ByteOffset%4 != 0 && i > 0 {
			t.Errorf("view %d offset %d is not 4-byte aligned", i, view.ByteOffset)
		}
	}

	bin := doc.Buffers[0].Data
	if got := bin[doc.BufferViews[1].ByteOffset : doc.BufferViews[1].ByteOffset+4]; !bytes.Equal(got, []byte{7, 8, 9, 10}) {
		t.Errorf("second buffer's bytes moved to %v, want 7 8 9 10", got)
	}
}

func TestPackGLBMovesAnExternalImageIntoTheChunk(t *testing.T) {
	doc := twoBufferDoc()
	if err := packGLB(doc, []packedImage{{Data: []byte{11, 12, 13}, Mime: "image/png"}}); err != nil {
		t.Fatalf("packGLB: %v", err)
	}

	image := doc.Images[0]
	if image.URI != "" {
		t.Errorf("image URI = %q, want empty", image.URI)
	}
	if image.MimeType != "image/png" {
		t.Errorf("image MimeType = %q, want image/png", image.MimeType)
	}
	if image.BufferView == nil {
		t.Fatal("image has no buffer view")
	}
	view := doc.BufferViews[*image.BufferView]
	if view.ByteOffset%4 != 0 {
		t.Errorf("image view offset %d is not 4-byte aligned", view.ByteOffset)
	}
	bin := doc.Buffers[0].Data
	if got := bin[view.ByteOffset : view.ByteOffset+view.ByteLength]; !bytes.Equal(got, []byte{11, 12, 13}) {
		t.Errorf("image bytes = %v, want 11 12 13", got)
	}
}

// TestPackGLBRoundTrips is the claim that matters: what the packer produces is
// a .glb a decoder reads back without the sibling files.
func TestPackGLBRoundTrips(t *testing.T) {
	doc := twoBufferDoc()
	if err := packGLB(doc, []packedImage{{Data: []byte{11, 12, 13}, Mime: "image/png"}}); err != nil {
		t.Fatalf("packGLB: %v", err)
	}

	var out bytes.Buffer
	encoder := gltf.NewEncoder(&out)
	encoder.AsBinary = true
	if err := encoder.Encode(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var back gltf.Document
	// No fs.FS: a decoder that has to open a sibling file fails here, which is
	// exactly the failure this test is looking for.
	if err := gltf.NewDecoder(bytes.NewReader(out.Bytes())).Decode(&back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.Buffers) != 1 {
		t.Fatalf("decoded buffers = %d, want 1", len(back.Buffers))
	}
	if back.Images[0].BufferView == nil || back.Images[0].URI != "" {
		t.Errorf("decoded image is %+v, want a buffer view and no URI", back.Images[0])
	}
	view := back.BufferViews[*back.Images[0].BufferView]
	if got := back.Buffers[0].Data[view.ByteOffset : view.ByteOffset+view.ByteLength]; !bytes.Equal(got, []byte{11, 12, 13}) {
		t.Errorf("decoded image bytes = %v, want 11 12 13", got)
	}
}

func TestPackGLBRejectsAnImageWithNoMediaType(t *testing.T) {
	doc := twoBufferDoc()
	if err := packGLB(doc, []packedImage{{Data: []byte{11}}}); err == nil {
		t.Fatal("packGLB accepted an image with no media type")
	}
}

func TestPackGLBRejectsAShortBuffer(t *testing.T) {
	doc := twoBufferDoc()
	doc.Buffers[0].Data = doc.Buffers[0].Data[:2]
	if err := packGLB(doc, []packedImage{{Data: []byte{11}, Mime: "image/png"}}); err == nil {
		t.Fatal("packGLB accepted a buffer holding less than it declares")
	}
}
