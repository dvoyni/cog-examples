package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// gradient is a picture with enough going on that a resampler has something to
// do and an encoder cannot trivially collapse it.
func gradient(width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return out.Bytes()
}

func decodeSize(t *testing.T, data []byte) (image.Point, string) {
	t.Helper()
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return image.Point{X: config.Width, Y: config.Height}, format
}

// TestReencodeLeavesAnImageWithinTheCapAlone is the property most of the set
// depends on: a test card is vendored exactly as authored.
func TestReencodeLeavesAnImageWithinTheCapAlone(t *testing.T) {
	source := encodePNG(t, gradient(64, 64))
	got, err := reencode(source, 1024)
	if err != nil {
		t.Fatalf("reencode: %v", err)
	}
	if !bytes.Equal(got.Data, source) {
		t.Error("an image within the cap should come back byte for byte")
	}
	if got.Mime != "image/png" {
		t.Errorf("mime = %q, want image/png", got.Mime)
	}
}

func TestReencodeLeavesEverythingAloneWithNoCap(t *testing.T) {
	source := encodePNG(t, gradient(2048, 8))
	got, err := reencode(source, 0)
	if err != nil {
		t.Fatalf("reencode: %v", err)
	}
	if !bytes.Equal(got.Data, source) {
		t.Error("a zero cap should copy every image through untouched")
	}
}

func TestReencodeCapsTheLongestEdgeAndKeepsTheAspect(t *testing.T) {
	got, err := reencode(encodePNG(t, gradient(512, 256)), 128)
	if err != nil {
		t.Fatalf("reencode: %v", err)
	}
	size, format := decodeSize(t, got.Data)
	if size != (image.Point{X: 128, Y: 64}) {
		t.Errorf("size = %v, want 128x64", size)
	}
	if format != "png" {
		t.Errorf("format = %q, want png - a resampled PNG must not turn lossy", format)
	}
}

func TestReencodeKeepsAJPEGAJPEG(t *testing.T) {
	source := encodeJPEG(t, gradient(512, 512))
	got, err := reencode(source, 128)
	if err != nil {
		t.Fatalf("reencode: %v", err)
	}
	size, format := decodeSize(t, got.Data)
	if format != "jpeg" || got.Mime != "image/jpeg" {
		t.Errorf("format = %q mime = %q, want jpeg - re-encoding a photo as PNG grows it", format, got.Mime)
	}
	if size != (image.Point{X: 128, Y: 128}) {
		t.Errorf("size = %v, want 128x128", size)
	}
	if len(got.Data) >= len(source) {
		t.Errorf("resampled to %d bytes from %d - the cap is meant to shrink it", len(got.Data), len(source))
	}
}

func TestReencodeRejectsWhatItCannotDecode(t *testing.T) {
	if _, err := reencode([]byte("not an image"), 1024); err == nil {
		t.Fatal("reencode accepted bytes that are not an image")
	}
}
