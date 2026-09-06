package main

import "fmt"

// upstream is where every asset in the set comes from.
const (
	upstreamRepo = "KhronosGroup/glTF-Sample-Assets"
	upstreamURL  = "https://github.com/KhronosGroup/glTF-Sample-Assets"
	upstreamRaw  = "https://raw.githubusercontent.com/KhronosGroup/glTF-Sample-Assets"
)

// asset is one vendored model.
//
// Commit is the whole point of this table. The Khronos repository has no
// releases and no tags, so a commit SHA is the only pin that means anything -
// "the version on main" names different bytes every month, and an asset that
// silently changes under a demo whose test asserts a node count is a bug that
// looks like a regression in scene. Each SHA is the last commit that touched
// that model's directory, so it moves only when the asset itself does.
//
// Legal is recorded here rather than copied out of upstream at fetch time, and
// verifyLegal checks the record against upstream's metadata.json on every run.
// The difference matters: a table that reads its own licence terms from the
// thing it is licensing cannot notice a relicensing. This one refuses to vendor
// an asset whose terms have moved.
type asset struct {
	Name    string // the upstream model directory, and the vendored one
	Title   string // upstream's human-readable label
	Summary string // what the asset is for, one line, from upstream
	Commit  string
	Why     string // why this asset is in the set at all
	Outputs []output
	Legal   []legalEntry
}

// output is one .glb this tool writes, built from one upstream variant
// directory. Every output is packed into a single file: whatever mixture of
// .gltf, .bin and loose images the variant is stored as upstream, what lands in
// assets/ is one .glb with no siblings.
type output struct {
	Variant string // upstream variant directory, e.g. "glTF" or "glTF-Quantized"
	File    string // the vendored file name, under assets/<Name>/
	// MaxTexture caps an image's longest edge in pixels. Zero - the usual case -
	// vendors every image byte for byte. See reencode for why that is the
	// default and what setting it costs.
	MaxTexture int
	Note       string // what this output is here to exercise
}

// legalEntry is one row of an upstream metadata.json "legal" array: either a
// licence covering some part of the asset, or a legal mark. A mark carries no
// SPDX identifier because it is not a licence - it reserves the right to
// withdraw a *mark*, and the licence it sits beside is unaffected.
type legalEntry struct {
	SPDX   string // empty for a legal mark
	What   string
	Artist string
	Owner  string
	Year   string
	Text   string
	URL    string
}

// IsMark reports whether the entry is a trademark reservation rather than a
// licence.
func (l legalEntry) IsMark() bool { return l.SPDX == "" }

// allowedSPDX is the whole permitted set, and it is checked on every run.
//
// CC0-1.0 and CC-BY-4.0 only: no NonCommercial, no NoDerivatives, no custom
// LicenseRef agreement. The README calls these examples "collected here for
// later publication alongside the engine", which makes them a distribution, and
// a committed reference screenshot is itself a derivative - so a
// permissive-for-vendored, anything-for-fetched split would only surface at
// publication, with the spec long frozen.
var allowedSPDX = map[string]bool{
	"CC0-1.0":   true,
	"CC-BY-4.0": true,
}

// licenseURL is the canonical text for each licence in allowedSPDX.
var licenseURL = map[string]string{
	"CC0-1.0":   "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
	"CC-BY-4.0": "https://creativecommons.org/licenses/by/4.0/legalcode",
}

// manifest is the demo asset set.
//
// Three earlier candidates are deliberately absent and must not creep back.
// DamagedHelmet is disqualified: CC-BY-4.0 and CC-BY-NC-4.0 both apply to its
// files, the NC term riding in from the original its rebuild derives from -
// WaterBottle replaces it. FlightHelmet is dropped: no .glb variant, 48 MB in
// 18 files, and it needs KHR_materials_transmission, which is not in the v1
// extension list, so scene would silently render it wrong. CesiumMan is
// dropped: exactly one animation clip, so it cannot demonstrate a crossfade,
// while Fox has three over 24 joints.
var manifest = []asset{
	{
		Name:    "WaterBottle",
		Title:   "Water Bottle",
		Summary: "Basic metal/roughness water bottle.",
		Commit:  "723ffc6706725b618b8c14ceb82e3e6904b08a76",
		Why: "The PBR showpiece: the complete core metallic-roughness set in one " +
			"material, TANGENT present, and no extensions at all.",
		Outputs: []output{{
			Variant:    "glTF",
			File:       "WaterBottle.glb",
			MaxTexture: 1024,
			Note:       "four 2048x2048 PNGs upstream, which is most of the set's bytes on their own",
		}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Microsoft", Owner: "Public", Year: "2017",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "AlphaBlendModeTest",
		Title:   "Alpha Blend Mode Test",
		Summary: "Tests alpha modes and settings.",
		Commit:  "723ffc6706725b618b8c14ceb82e3e6904b08a76",
		Why:     "alphaMode OPAQUE, MASK and BLEND side by side, which is what the back-to-front blend bucket is judged against.",
		Outputs: []output{{
			Variant:    "glTF",
			File:       "AlphaBlendModeTest.glb",
			MaxTexture: 1024,
			Note:       "three 2048x2048 JPEGs upstream",
		}},
		Legal: []legalEntry{{
			SPDX: "CC-BY-4.0", What: "Everything", Artist: "Ed Mackey", Owner: "Analytical Graphics, Inc.", Year: "2018",
			Text: "Creative Commons Attribution 4.0 International",
			URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
		}},
	},
	{
		Name:    "BoxVertexColors",
		Title:   "Box Vertex Colors",
		Summary: "A simple unit cube that uses vertex colors, stored in the `COLORS_0` attribute.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why:     "COLOR_0, and the source of assets/broken/truncated.glb - which is why it has to be CC0.",
		Outputs: []output{{Variant: "glTF", File: "BoxVertexColors.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Marco Hutter (https://github.com/javagl/)", Owner: "Public", Year: "2023",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "CompareBaseColor",
		Title:   "Compare Base Color",
		Summary: "This model compares base color methods.",
		Commit:  "9429648735279342b4c32b8745f7904196607379",
		Why:     "baseColorFactor against baseColorTexture against both, which is the material contract's easiest thing to get subtly wrong.",
		Outputs: []output{{Variant: "glTF", File: "CompareBaseColor.glb"}},
		Legal: []legalEntry{
			{
				What: "glTF logo", Artist: "Non-copyrightable logo", Owner: "Khronos Group", Year: "2017",
				Text: "Khronos Trademark or Logo",
				URL:  upstreamURL + "/blob/main/LICENSES/LicenseRef-LegalMark-Khronos.txt",
			},
			{
				SPDX: "CC0-1.0", What: "Everything", Artist: "Eric Chadwick and DGG", Owner: "Public", Year: "2024",
				Text: "Creative Commons Zero v1.0 Universal",
				URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
			},
		},
	},
	{
		Name:    "EmissiveStrengthTest",
		Title:   "Emissive Strength Test",
		Summary: "Tests if the KHR_materials_emissive_strength extension is supported properly.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why:     "KHR_materials_emissive_strength, one of the v1 extensions.",
		Outputs: []output{{Variant: "glTF", File: "EmissiveStrengthTest.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC-BY-4.0", What: "Everything", Artist: "Ed Mackey", Owner: "AGI", Year: "2022",
			Text: "Creative Commons Attribution 4.0 International",
			URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
		}},
	},
	{
		Name:    "PointLightIntensityTest",
		Title:   "Point Light Intensity Test",
		Summary: "This model tests KHR_lights_punctual intensity vs lamp color.",
		Commit:  "723ffc6706725b618b8c14ceb82e3e6904b08a76",
		Why:     "KHR_lights_punctual parsed and exposed as data, and the intensity units that go with it.",
		Outputs: []output{{Variant: "glTF", File: "PointLightIntensityTest.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Ed Mackey", Owner: "Public", Year: "2025",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "Fox",
		Title:   "Fox",
		Summary: "Multiple animations cycles: Survey, Walk, Run.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why:     "Three clips over 24 joints, which is what a crossfade needs and what CesiumMan, with one clip, could not give.",
		Outputs: []output{{Variant: "glTF", File: "Fox.glb"}},
		Legal: []legalEntry{
			{
				SPDX: "CC0-1.0", What: "Model", Artist: "PixelMannen", Owner: "Public", Year: "2014",
				Text: "Creative Commons Zero v1.0 Universal",
				URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
			},
			{
				SPDX: "CC-BY-4.0", What: "Rigging & Animation", Artist: "tomkranis", Owner: "tomkranis", Year: "2014",
				Text: "Creative Commons Attribution 4.0 International",
				URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
			},
			{
				SPDX: "CC-BY-4.0", What: "Conversion to glTF", Artist: "@AsoboStudio and @scurest", Owner: "@AsoboStudio and @scurest", Year: "2017",
				Text: "Creative Commons Attribution 4.0 International",
				URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
			},
		},
	},
	{
		Name:    "AnimatedMorphCube",
		Title:   "Animated Morph Cube",
		Summary: "Demonstrates a simple cube with two simple morph targets and an animation that transitions between them both.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why:     "Morph targets with an animation on the weights channel, in the two forms the set needs.",
		Outputs: []output{
			{Variant: "glTF", File: "AnimatedMorphCube.glb", Note: "morph POSITION/NORMAL deltas and a weights animation"},
			{
				Variant: "glTF-Quantized",
				File:    "AnimatedMorphCube-Quantized.glb",
				Note:    "KHR_mesh_quantization, including quantized morph deltas; upstream ships it as .gltf only, so this tool packs it",
			},
		},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Microsoft", Owner: "Public", Year: "2017",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "MorphStressTest",
		Title:   "Morph Stress Test",
		Summary: "Tests up to 8 morph targets.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why:     "Eight morph targets against the attribute mask and the sparse-weights path.",
		Outputs: []output{{Variant: "glTF", File: "MorphStressTest.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC-BY-4.0", What: "Everything", Artist: "Ed Mackey", Owner: "Analytical Graphics, Inc.", Year: "2021",
			Text: "Creative Commons Attribution 4.0 International",
			URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
		}},
	},
	{
		Name:    "InterpolationTest",
		Title:   "Interpolation Test",
		Summary: "A sample with three different animation interpolations",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why: "No skins key at all, so it is the only way to animate a node that is not a joint. " +
			"Its nine clips are exactly Step/Linear/CubicSpline over translation/rotation/scale, " +
			"and it carries u8 indices - three contracts in one 8 KB file.",
		Outputs: []output{{Variant: "glTF", File: "InterpolationTest.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Khronos", Owner: "Public", Year: "2017",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "CesiumMilkTruck",
		Title:   "Cesium Milk Truck",
		Summary: "Textured. Multiple nodes/meshes. Animations.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why: "The re-rooting asset: Wheels and Wheels.001 sit at +/-1.43 on X beneath a Yup2Zup root, " +
			"so a Node draw has a real authored world transform to discard, at hierarchy depth 4.",
		Outputs: []output{{Variant: "glTF", File: "CesiumMilkTruck.glb"}},
		Legal: []legalEntry{
			{
				SPDX: "CC-BY-4.0", What: "Everything", Artist: "Cesium", Owner: "Cesium", Year: "2017",
				Text: "Creative Commons Attribution 4.0 International",
				URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
			},
			{
				What: "Cesium logo", Artist: "Non-copyrightable logo", Owner: "Cesium", Year: "2015",
				Text: "Cesium Trademark or Logo",
				URL:  upstreamURL + "/blob/main/LICENSES/LicenseRef-LegalMark-Cesium.txt",
			},
		},
	},
	{
		Name:    "MultipleScenes",
		Title:   "Multiple Scenes",
		Summary: "A simple glTF asset with two scenes. Each scene consists of one node with one mesh.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why: "The only file in the whole Khronos repository with more than one scenes entry, " +
			"and therefore the only possible exercise of the Scene selector. Upstream ships it as .gltf only.",
		Outputs: []output{{Variant: "glTF", File: "MultipleScenes.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Public", Owner: "Public", Year: "2017",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
	{
		Name:    "TextureSettingsTest",
		Title:   "Texture Settings Test",
		Summary: "Tests single/double-sided and various texturing modes.",
		Commit:  "81e8b567643b5166e6ff40024e4ff71ad4b18676",
		Why: "Three images backing nine textures through different samplers - the only honest exercise of a " +
			"path-keyed texture cache, since every model owns a private directory and no two files ever " +
			"resolve to the same path. It covers mirror, repeat and clamp on both axes.",
		Outputs: []output{{Variant: "glTF", File: "TextureSettingsTest.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC-BY-4.0", What: "Everything", Artist: "Ed Mackey", Owner: "Analytical Graphics, Inc.", Year: "2017",
			Text: "Creative Commons Attribution 4.0 International",
			URL:  "https://creativecommons.org/licenses/by/4.0/legalcode",
		}},
	},
	{
		Name:    "MeshPrimitiveModes",
		Title:   "Mesh Primitive Modes",
		Summary: "An example that shows rendering modes that are supported for mesh primitives in glTF.",
		Commit:  "723ffc6706725b618b8c14ceb82e3e6904b08a76",
		Why: "No .glb anywhere in the Khronos repository contains a fan, strip, loop, line or point primitive - " +
			"the histogram is TRIANGLES x14,192 against 21 of everything else, all inside .gltf-only assets. " +
			"Packing this one is the only way the StripIndexFormat path is reachable at all.",
		Outputs: []output{{Variant: "glTF", File: "MeshPrimitiveModes.glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Marco Hutter (https://github.com/javagl/)", Owner: "Public", Year: "2023",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  "https://creativecommons.org/publicdomain/zero/1.0/legalcode",
		}},
	},
}

// brokenSource names the asset assets/broken/truncated.glb is cut from, and the
// file it is written to. It is CC0 on purpose: a deliberately corrupted
// derivative is a poor thing to owe an attribution line for.
var brokenSource = struct{ Asset, Output, File string }{
	Asset:  "BoxVertexColors",
	Output: "BoxVertexColors.glb",
	File:   "truncated.glb",
}

// validate checks the manifest against its own rules before anything is
// fetched: the licence policy, and that every asset and output is named once.
func validate(assets []asset) error {
	names := map[string]bool{}
	files := map[string]bool{}
	for _, a := range assets {
		if names[a.Name] {
			return fmt.Errorf("%s: listed twice", a.Name)
		}
		names[a.Name] = true
		if len(a.Outputs) == 0 {
			return fmt.Errorf("%s: no outputs", a.Name)
		}
		if a.Commit == "" {
			return fmt.Errorf("%s: no source commit - a pin to main is not a pin", a.Name)
		}
		licensed := false
		for _, entry := range a.Legal {
			if entry.IsMark() {
				if entry.Text == "" {
					return fmt.Errorf("%s: a legal mark with no name", a.Name)
				}
				continue
			}
			if !allowedSPDX[entry.SPDX] {
				return fmt.Errorf("%s: %q is outside the CC0-1.0 / CC-BY-4.0 policy", a.Name, entry.SPDX)
			}
			licensed = true
		}
		if !licensed {
			return fmt.Errorf("%s: no licence, only marks", a.Name)
		}
		for _, out := range a.Outputs {
			key := a.Name + "/" + out.File
			if files[key] {
				return fmt.Errorf("%s: written twice", key)
			}
			files[key] = true
		}
	}
	if !names[brokenSource.Asset] {
		return fmt.Errorf("broken/%s is cut from %s, which is not in the set", brokenSource.File, brokenSource.Asset)
	}
	return nil
}

// find returns the asset named name.
func find(assets []asset, name string) (asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return asset{}, false
}
