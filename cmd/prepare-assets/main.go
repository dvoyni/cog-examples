// Command prepare-assets builds the vendored demo asset set under `assets/`.
//
//	go run ./cmd/prepare-assets
//
// The scene demos need real models, and real models come with real licensing.
// This tool is where both live: it carries the manifest - what each asset is,
// which upstream commit it is pinned to, what its licence obliges - fetches
// each one, packs it into a single `.glb`, writes the deliberately broken file
// the loading demo needs, and generates `assets/ATTRIBUTION.md` from the same
// manifest that drove the fetch.
//
// It exists rather than a one-off download for three reasons.
//
// # The pin
//
// The Khronos repository has no releases and no tags. "The version on main"
// names different bytes every month, so an asset can change under a demo whose
// test asserts a node count, and the failure looks like a regression in scene.
// Every asset here is pinned to the commit that last touched it.
//
// # The packing
//
// Three assets the set needs - MeshPrimitiveModes, the quantized
// AnimatedMorphCube, MultipleScenes - have no `.glb` variant upstream at all,
// and MeshPrimitiveModes is the only route to a fan, strip, loop, line or point
// primitive anywhere in the repository. Packing them is not a convenience; it
// is the only way those contracts are reachable. Everything else is packed the
// same way so the set is uniform: one file per asset, no siblings, no loose
// `.bin` and no loose images.
//
// # The size
//
// No sub-5 MiB PBR showpiece exists in the repository. WaterBottle is 9 MB,
// almost all of it four 2048x2048 PNGs, and AlphaBlendModeTest is another 3 MB
// of 2048x2048 JPEGs - together most of the set's bytes. Both are resampled to
// a 1024 px cap, which lands WaterBottle around 1.7 MiB with no visible change.
// Every other asset is vendored byte for byte: a test card's exact texels are
// the thing it is testing, and this tool will not touch them.
//
// # Re-running it
//
// Every fetch is cached under the commit that produced it, so a second run does
// no network. The output is deterministic: the same manifest produces the same
// bytes.
//
// Before anything is written, each asset's recorded licensing is checked
// against upstream's own metadata.json at the pinned commit, and the run stops
// if they have diverged. The licence policy is CC0-1.0 and CC-BY-4.0 only, and
// it is enforced here rather than remembered.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/qmuntal/gltf"
)

func main() {
	out := flag.String("out", "assets", "directory the vendored asset set is written to")
	cache := flag.String("cache", defaultCache(), "directory upstream downloads are cached in")
	only := flag.String("only", "", "prepare just this asset, by upstream model name")
	quiet := flag.Bool("quiet", false, "only report failures")
	flag.Parse()

	if err := run(*out, *cache, *only, !*quiet); err != nil {
		fmt.Fprintf(os.Stderr, "prepare-assets: %v\n", err)
		os.Exit(1)
	}
}

func run(out, cache, only string, loud bool) error {
	if err := validate(manifest); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	assets := manifest
	if only != "" {
		one, ok := find(assets, only)
		if !ok {
			return fmt.Errorf("%q is not in the manifest", only)
		}
		assets = []asset{one}
	}

	source := newSource(cache, loud)
	written := map[string]int{}

	for _, a := range assets {
		if loud {
			fmt.Printf("%s @ %s\n", a.Name, short(a.Commit))
		}
		metadata, err := source.file(a.Commit, path.Join("Models", a.Name, "metadata.json"))
		if err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		if err := verifyLegal(a, metadata); err != nil {
			return err
		}
		for _, output := range a.Outputs {
			size, err := prepare(source, a, output, out)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", a.Name, output.File, err)
			}
			written[path.Join(a.Name, output.File)] = size
		}
	}

	// The broken file is cut from an asset this run may not have prepared, so it
	// is read back off disk rather than kept from the loop above.
	if only == "" || only == brokenSource.Asset {
		size, err := prepareBroken(out)
		if err != nil {
			return err
		}
		written[path.Join("broken", brokenSource.File)] = size
	}

	if only == "" {
		attribution := filepath.Join(out, attributionFile)
		if err := os.WriteFile(attribution, []byte(renderAttribution(manifest)), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", attribution, err)
		}
		written[attributionFile] = len(renderAttribution(manifest))
	}

	if loud {
		report(written)
	}
	return nil
}

// prepare fetches one output's upstream variant, packs it and writes it.
func prepare(source *source, a asset, output output, out string) (int, error) {
	upstream := variantFS{
		source: source,
		commit: a.Commit,
		prefix: path.Join("Models", a.Name, output.Variant),
	}

	document, err := upstream.ReadFile(a.Name + ".gltf")
	if err != nil {
		return 0, err
	}
	var doc gltf.Document
	if err := gltf.NewDecoderFS(bytes.NewReader(document), upstream).Decode(&doc); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}

	images, err := gatherImages(&doc, upstream, output.MaxTexture)
	if err != nil {
		return 0, err
	}
	if err := packGLB(&doc, images); err != nil {
		return 0, err
	}

	var packed bytes.Buffer
	encoder := gltf.NewEncoder(&packed)
	encoder.AsBinary = true
	if err := encoder.Encode(&doc); err != nil {
		return 0, fmt.Errorf("encode: %w", err)
	}
	return write(filepath.Join(out, a.Name, output.File), packed.Bytes())
}

// gatherImages resolves every image the document references and applies the
// output's texture cap to it.
func gatherImages(doc *gltf.Document, upstream variantFS, maxTexture int) ([]packedImage, error) {
	images := make([]packedImage, len(doc.Images))
	for i, image := range doc.Images {
		switch {
		case image.URI == "":
			// Already a buffer view; packGLB re-points it and leaves it alone.
			continue
		case image.IsEmbeddedResource():
			data, err := image.MarshalData()
			if err != nil {
				return nil, fmt.Errorf("image %d: decode embedded resource: %w", i, err)
			}
			images[i], err = reencode(data, maxTexture)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", i, err)
			}
		default:
			name, err := url.PathUnescape(image.URI)
			if err != nil {
				return nil, fmt.Errorf("image %d: %q is not a usable URI: %w", i, image.URI, err)
			}
			data, err := upstream.ReadFile(name)
			if err != nil {
				return nil, err
			}
			images[i], err = reencode(data, maxTexture)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return images, nil
}

// prepareBroken cuts the truncated file out of an already-prepared asset, so
// what it corrupts is exactly the bytes a demo would otherwise have loaded.
func prepareBroken(out string) (int, error) {
	whole, err := os.ReadFile(filepath.Join(out, brokenSource.Asset, brokenSource.Output))
	if err != nil {
		return 0, fmt.Errorf("read the source of broken/%s: %w", brokenSource.File, err)
	}
	cut, err := truncateGLB(whole)
	if err != nil {
		return 0, err
	}
	return write(filepath.Join(out, "broken", brokenSource.File), cut)
}

func write(name string, data []byte) (int, error) {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return 0, fmt.Errorf("create %s: %w", filepath.Dir(name), err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", name, err)
	}
	return len(data), nil
}

func report(written map[string]int) {
	names := make([]string, 0, len(written))
	total := 0
	for name, size := range written {
		names = append(names, name)
		total += size
	}
	sort.Strings(names)
	fmt.Println()
	for _, name := range names {
		fmt.Printf("%10s  %s\n", humanBytes(written[name]), name)
	}
	fmt.Printf("%10s  %d files\n", humanBytes(total), len(names))
}

func humanBytes(size int) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func defaultCache() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "cog-examples-assets")
	}
	return filepath.Join(base, "cog-examples", "gltf-sample-assets")
}
