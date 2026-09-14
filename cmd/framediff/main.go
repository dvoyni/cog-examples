// Command framediff compares two PNG captures pixel by pixel and reports how
// far apart they are.
//
//	go run ./cmd/framediff before.png after.png [diff.png]
//
// It exists because cog has no pixel readback in any test: gfx and gogpu can
// draw a frame but nothing in the engine can assert about one, so every
// fidelity claim about a narrowed encoding is judged by eye. This narrows what
// the eye has to do. It does not replace the eye - a difference this reports is
// a number, not a verdict, and a frame can be pixel-identical and still wrong
// for a reason no capture at one pose can show.
//
// The method is the one that worked: whole-frame captures at a fixed pose,
// differenced channel by channel, with a null pair - two captures of the same
// build at the same pose - as the control. The control is what makes any other
// number mean something. If the null pair is not identical, nothing else here
// is signal.
//
// Output is one line of summary plus a per-channel histogram of absolute
// differences, and an optional amplified difference image.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: framediff before.png after.png [diff.png]")
		os.Exit(2)
	}
	before, after := load(os.Args[1]), load(os.Args[2])
	if before.Bounds() != after.Bounds() {
		fmt.Fprintf(os.Stderr, "different sizes: %v and %v\n", before.Bounds(), after.Bounds())
		os.Exit(1)
	}

	bounds := before.Bounds()
	var differing, total int
	var worst uint32
	var sum float64
	// Histogram of the largest per-pixel channel difference, so a frame that is
	// off by one everywhere reads differently from one that is off by eighty in
	// a corner - the first is encoding noise, the second is a bug.
	hist := map[uint32]int{}
	diff := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			br, bg, bb, _ := before.At(x, y).RGBA()
			ar, ag, ab, _ := after.At(x, y).RGBA()
			d := max3(absDiff(br, ar), absDiff(bg, ag), absDiff(bb, ab)) >> 8
			total++
			hist[d]++
			if d > 0 {
				differing++
				sum += float64(d)
			}
			if d > worst {
				worst = d
			}
			// Amplified ten times so a difference of one is visible at all.
			v := min(d*10, 255)
			diff.Set(x, y, color.RGBA{R: uint8(v), G: uint8(v), B: uint8(v), A: 255})
		}
	}

	fmt.Printf("pixels           %d\n", total)
	fmt.Printf("differing        %d (%.4f%%)\n", differing, 100*float64(differing)/float64(total))
	fmt.Printf("worst channel    %d / 255\n", worst)
	if differing > 0 {
		fmt.Printf("mean over those  %.3f / 255\n", sum/float64(differing))
	}
	fmt.Println("histogram of the largest channel difference per pixel:")
	for d := uint32(0); d <= 255; d++ {
		if n := hist[d]; n > 0 {
			fmt.Printf("  %3d  %10d  %7.4f%%\n", d, n, 100*float64(n)/float64(total))
		}
	}
	if worst == 0 {
		fmt.Println("VERDICT: pixel-identical")
	}

	if len(os.Args) > 3 {
		f, err := os.Create(os.Args[3])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		if err := png.Encode(f, diff); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (differences amplified 10x)\n", os.Args[3])
	}
}

func load(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return img
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func max3(a, b, c uint32) uint32 { return max(a, max(b, c)) }
