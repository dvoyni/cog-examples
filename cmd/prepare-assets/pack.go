package main

import (
	"fmt"

	"github.com/qmuntal/gltf"
)

// packedImage is the bytes and media type prepare-assets has decided one
// gltf.Image should carry once it stops being a file of its own.
type packedImage struct {
	Data []byte
	Mime string
}

// packGLB collapses doc into the single-buffer shape a .glb needs. Every
// buffer's bytes and every externally referenced image land in one binary
// chunk, every buffer view is re-pointed at it, and the document that comes
// back holds no URI at all - so the vendored asset is one file with no
// siblings, whichever variant it was fetched from.
//
// images is indexed like doc.Images and supplies bytes only for the entries
// that carry a URI; an image already backed by a buffer view is left alone
// beyond the re-pointing every view gets.
//
// Every chunk is placed at a 4-byte boundary. glTF only requires a view to be
// aligned to the component size of the accessors reading it, and 4 is the
// largest of those, so one rule covers every case without inspecting accessors.
func packGLB(doc *gltf.Document, images []packedImage) error {
	if len(images) != len(doc.Images) {
		return fmt.Errorf("packGLB: %d images supplied for %d image entries", len(images), len(doc.Images))
	}

	var bin []byte
	offsets := make([]int, len(doc.Buffers))
	for i, buffer := range doc.Buffers {
		if len(buffer.Data) < buffer.ByteLength {
			return fmt.Errorf("packGLB: buffer %d holds %d bytes of a declared %d", i, len(buffer.Data), buffer.ByteLength)
		}
		bin = align4(bin)
		offsets[i] = len(bin)
		bin = append(bin, buffer.Data[:buffer.ByteLength]...)
	}

	for i, view := range doc.BufferViews {
		if view.Buffer < 0 || view.Buffer >= len(offsets) {
			return fmt.Errorf("packGLB: buffer view %d points at buffer %d of %d", i, view.Buffer, len(offsets))
		}
		view.ByteOffset += offsets[view.Buffer]
		view.Buffer = 0
	}

	for i, image := range doc.Images {
		if image.URI == "" {
			continue
		}
		if images[i].Mime == "" {
			return fmt.Errorf("packGLB: image %d (%q) has no media type", i, image.URI)
		}
		bin = align4(bin)
		doc.BufferViews = append(doc.BufferViews, &gltf.BufferView{
			Buffer:     0,
			ByteOffset: len(bin),
			ByteLength: len(images[i].Data),
		})
		bin = append(bin, images[i].Data...)
		image.BufferView = gltf.Index(len(doc.BufferViews) - 1)
		image.MimeType = images[i].Mime
		image.URI = ""
	}

	doc.Buffers = []*gltf.Buffer{{ByteLength: len(bin), Data: bin}}
	return nil
}

// align4 pads bin out to the next 4-byte boundary.
func align4(bin []byte) []byte {
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	return bin
}
