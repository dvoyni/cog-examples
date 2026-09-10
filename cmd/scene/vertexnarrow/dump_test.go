package main

// THROWAWAY. Writes the two generated textures out so they can be looked at
// directly, instead of being inferred from what the sphere reflects.
//
//	go test ./cmd/scene/vertexnarrow/ -run DumpTextures

import (
	"image"
	"image/png"
	"os"
	"testing"
)

func writePNG(t *testing.T, name string, width, height int, pixels []byte) {
	t.Helper()
	img := &image.NRGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%dx%d)", name, width, height)
}

func TestDumpTextures(t *testing.T) {
	writePNG(t, "../../../tmp/dump-env.png", envWidth, envHeight, environmentTexture())
	writePNG(t, "../../../tmp/dump-grain.png", normalMapWidth, normalMapHeight, normalMapTexture(mapGrain, mapStrength))
}
