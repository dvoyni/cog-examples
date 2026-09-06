package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The GLB container: a 12-byte header, then a run of 8-byte-headed chunks.
const (
	glbHeaderSize = 12
	glbChunkSize  = 8
	glbMagic      = 0x46546C67 // "glTF"
	glbChunkJSON  = 0x4E4F534A // "JSON"
	glbChunkBIN   = 0x004E4942 // "BIN\0"
)

// truncateGLB cuts src's binary chunk in half without touching a single byte
// before it. The 12-byte header, the whole JSON chunk and the binary chunk's
// own header all survive verbatim, so both length fields still declare the
// bytes the file no longer has - which is what a truncated download looks like,
// and what makes this a failure a loader can only discover after it has already
// accepted the file and started reading it.
//
// That distinction is the point of the asset. A path that does not exist fails
// synchronously, before anything is loaded; this one parses far enough to look
// fine and then fails terminally, and scene's contract is that a model which
// fails is skipped rather than substituted.
func truncateGLB(src []byte) ([]byte, error) {
	offset, length, err := glbBinChunk(src)
	if err != nil {
		return nil, err
	}
	if length < 2 {
		return nil, fmt.Errorf("truncateGLB: binary chunk is %d bytes, too small to cut", length)
	}
	return src[:offset+length/2], nil
}

// glbBinChunk returns where the binary chunk's payload starts and how long it
// declares itself to be.
func glbBinChunk(src []byte) (offset, length int, err error) {
	if len(src) < glbHeaderSize {
		return 0, 0, errors.New("truncateGLB: shorter than a GLB header")
	}
	if binary.LittleEndian.Uint32(src) != glbMagic {
		return 0, 0, errors.New("truncateGLB: not a GLB file")
	}
	if declared := int(binary.LittleEndian.Uint32(src[8:])); declared != len(src) {
		return 0, 0, fmt.Errorf("truncateGLB: header declares %d bytes, file holds %d", declared, len(src))
	}

	position := glbHeaderSize
	sawJSON := false
	for position+glbChunkSize <= len(src) {
		chunkLength := int(binary.LittleEndian.Uint32(src[position:]))
		chunkType := binary.LittleEndian.Uint32(src[position+4:])
		payload := position + glbChunkSize
		if payload+chunkLength > len(src) {
			return 0, 0, fmt.Errorf("truncateGLB: chunk at %d runs past the end of the file", position)
		}
		switch chunkType {
		case glbChunkJSON:
			sawJSON = true
		case glbChunkBIN:
			if !sawJSON {
				return 0, 0, errors.New("truncateGLB: binary chunk precedes the JSON chunk")
			}
			return payload, chunkLength, nil
		}
		position = payload + chunkLength
	}
	return 0, 0, errors.New("truncateGLB: no binary chunk")
}
