package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// jpegQuality is what a resampled JPEG is written back at. The sources this
// touches are already JPEG, so the second generation rides on top of the
// first - but the halving that precedes it smooths the first generation's
// artefacts rather than compounding them.
const jpegQuality = 95

// reencode brings one image under an output's texture cap.
//
// An image already within the cap comes back byte for byte: untouched, still
// exactly the file the artist authored. That is deliberate and it is most of
// the set - a test card's whole value is its exact texels, and CompareBaseColor
// and TextureSettingsTest would be answering a different question if this tool
// resampled them. Only images that are genuinely oversized are rebuilt, which
// in practice is the two PBR showpieces.
//
// A rebuilt image keeps its media type. PNG stays PNG, so nothing lossy is ever
// introduced where it was not already; JPEG stays JPEG, because re-encoding a
// photographic 2048x2048 JPEG as PNG makes it five times larger, not smaller.
//
// Resampling happens in the image's stored space rather than in linear light.
// For a single 2:1 reduction the difference is far below the visible-change bar
// this is held to, and doing it "properly" would mean deciding per image which
// of its channels are colour and which are data - normal and ORM maps are data,
// and linearising those would be the actual error.
func reencode(data []byte, maxSize int) (packedImage, error) {
	source, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return packedImage{}, fmt.Errorf("decode: %w", err)
	}
	mime, ok := imageMime[format]
	if !ok {
		return packedImage{}, fmt.Errorf("%s is not a media type glTF carries", format)
	}

	size := source.Bounds().Size()
	if maxSize <= 0 || (size.X <= maxSize && size.Y <= maxSize) {
		return packedImage{Data: data, Mime: mime}, nil
	}

	var out bytes.Buffer
	scaled := downscale(source, maxSize)
	switch format {
	case "png":
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, scaled)
	case "jpeg":
		err = jpeg.Encode(&out, scaled, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		return packedImage{}, fmt.Errorf("encode %s: %w", format, err)
	}
	return packedImage{Data: out.Bytes(), Mime: mime}, nil
}

// imageMime maps the format names image.Decode reports onto the two media types
// glTF allows an image to declare.
var imageMime = map[string]string{
	"png":  "image/png",
	"jpeg": "image/jpeg",
}

// downscale reduces source so its longest edge is maxSize, keeping its aspect
// ratio. CatmullRom rather than a box filter: this is one large reduction, not
// a mip chain, and the sharper result is what "no visible change" asks for.
func downscale(source image.Image, maxSize int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width >= height {
		height = max(height*maxSize/width, 1)
		width = maxSize
	} else {
		width = max(width*maxSize/height, 1)
		height = maxSize
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, xdraw.Src, nil)
	return scaled
}
