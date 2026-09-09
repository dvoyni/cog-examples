// Command meshbytes measures what scene stores per mesh across the vendored
// glTF assets, for cog#168.
//
// It is throwaway measurement code, not a tool anyone maintains. It mirrors
// scene's glTF conversion rather than calling it, because the conversion is
// unexported and this module cannot reach it; every rule it mirrors is marked
// with the file and function in scene it was copied from, so a reader can check
// the mirror rather than trusting it.
//
// The measurement is taken *after* conversion, not on the accessors. That
// matters more than it sounds: scene unwelds a primitive that authored no
// NORMAL, expands strips and fans into lists, synthesises an index buffer for
// non-indexed geometry, and fills JOINTS_0/WEIGHTS_0 on any mesh node an
// animation moves. Counting accessors would miss all four.
package main

import (
	"cmp"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Byte widths scene stores today, from scene/mesh.go's Vertex.
const (
	todayPosition = 12 // Float32x3
	todayNormal   = 12 // Float32x3
	todayTangent  = 16 // Float32x4
	todayUV       = 8  // Float32x2
	todayColor    = 4  // Unorm8x4
	todayJoints   = 8  // Uint16x4
	todayWeights  = 16 // Float32x4
	todayStride   = 84
)

// The candidate narrow set. This is arithmetic on a hypothesis, not a decision:
// cog#171 is where the real per-attribute choice is made. Position is kept at
// full float here because it is the one attribute with an unbounded range;
// narrowPositionBytes is the variant that narrows it too, which costs a
// per-mesh scale and bias somewhere outside the vertex.
const (
	narrowPosition = 12 // Float32x3, unchanged
	narrowNormal   = 4  // Unorm1010102, or oct32 as Unorm16x2
	narrowTangent  = 4  // Unorm1010102 with handedness in the spare 2 bits
	narrowUV       = 4  // Float16x2
	narrowColor    = 4  // Unorm8x4, unchanged
	narrowJoints   = 4  // Uint8x4, capping a skin at 256 joints
	narrowWeights  = 4  // Unorm8x4

	narrowPositionQuantized = 8 // Snorm16x4 with a per-mesh scale and bias
)

// skinKind is how a primitive's JOINTS_0/WEIGHTS_0 came to be filled.
//
// The three cases are not the same data even though they occupy the same 24
// bytes. From scene/gltfload.go's walkNode and scene/gltfanim.go's
// bindGeometryJoints: a node with a glTF skin gets the file's own per-vertex
// influences; a mesh node that any animation moves, or that hangs under one,
// gets a degenerate single-joint binding written over every vertex; everything
// else keeps the zero value scene allocated and never reads.
type skinKind int

const (
	skinNone  skinKind = iota // zeros, never read
	skinPlain                 // one constant joint at weight 1, written per vertex
	skinReal                  // the file's own per-vertex influences
)

func (k skinKind) String() string {
	switch k {
	case skinPlain:
		return "plain"
	case skinReal:
		return "skin"
	}
	return "none"
}

// geoKey interns one converted primitive exactly as scene/gltfload.go's
// geometryKey does: the same primitive under two skin bindings, or with and
// without generated tangents, is two conversions and two copies in memory.
type geoKey struct {
	mesh, primitive int
	tangents        bool
	skin, joint     int
}

// attr is one vertex attribute as the file carries it.
type attr struct {
	present    bool
	component  string
	normalized bool
}

// geo is one converted primitive measured.
type geo struct {
	file            string
	mesh            string
	meshIdx, primAt int
	mode            string

	srcVertices int // the POSITION accessor's count
	vertices    int // after unwelding
	indices     int // after topology expansion and unwelding
	srcIndices  int
	indexType   string
	unwelded    bool
	skipped     string

	attrs      map[string]attr
	genNormal  bool // scene generated flat normals
	genTangent bool // scene generated a tangent frame
	skin       skinKind

	targets     int
	morphStride int

	uvMin, uvMax [2]float32
	hasUV        bool
	maxJoint     int

	instances int // flattened primitives pointing at this one conversion
}

// attribute names in scene's location order.
var attrOrder = []string{
	gltf.POSITION, gltf.NORMAL, gltf.TANGENT,
	gltf.TEXCOORD_0, gltf.TEXCOORD_1, gltf.COLOR_0,
	gltf.JOINTS_0, gltf.WEIGHTS_0,
}

var shortName = map[string]string{
	gltf.POSITION: "POS", gltf.NORMAL: "NRM", gltf.TANGENT: "TAN",
	gltf.TEXCOORD_0: "UV0", gltf.TEXCOORD_1: "UV1", gltf.COLOR_0: "COL",
	gltf.JOINTS_0: "JNT", gltf.WEIGHTS_0: "WGT",
}

// onDisk is each .glb's size on disk, for contrast with what scene holds after
// loading it. It is the whole file - textures, animations and JSON included -
// so it is a ceiling on the geometry the file shipped, never an understatement.
var onDisk = map[string]int{}

func main() {
	dir := flag.String("assets", "assets", "directory of vendored glTF assets")
	flag.Parse()

	files, err := filepath.Glob(filepath.Join(*dir, "*", "*.glb"))
	if err != nil {
		fail(err)
	}
	sort.Strings(files)

	var all []geo
	for _, file := range files {
		// broken/ holds deliberately malformed fixtures; they are load-failure
		// tests, not content.
		if strings.Contains(filepath.ToSlash(file), "/broken/") {
			continue
		}
		if info, err := os.Stat(file); err == nil {
			onDisk[strings.TrimSuffix(filepath.Base(file), ".glb")] = int(info.Size())
		}
		measured, err := measure(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			continue
		}
		all = append(all, measured...)
	}
	report(all)
}

func measure(path string) ([]geo, error) {
	doc, err := gltf.Open(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".glb")

	animated := animatedNodes(doc)
	interned := map[geoKey]int{}
	var out []geo

	// One walk per scene, with the visited set cleared between them, exactly as
	// scene/gltfload.go's flattenScene does. Every scene in the file is
	// flattened, so an unreferenced mesh costs nothing and a mesh referenced
	// from two scenes is converted once.
	for _, scene := range doc.Scenes {
		if scene == nil {
			continue
		}
		visited := map[int]bool{}
		chain := 0
		for _, root := range scene.Nodes {
			walkNode(doc, root, visited, &chain, animated, func(mesh int, key func(int, *gltf.Primitive) geoKey) {
				if mesh < 0 || mesh >= len(doc.Meshes) || doc.Meshes[mesh] == nil {
					return
				}
				for at, primitive := range doc.Meshes[mesh].Primitives {
					if primitive == nil {
						continue
					}
					k := key(at, primitive)
					if index, seen := interned[k]; seen {
						out[index].instances++
						continue
					}
					measured := convert(doc, name, mesh, at, primitive, k)
					interned[k] = len(out)
					out = append(out, measured)
				}
			})
		}
	}
	return out, nil
}

// animatedNodes is scene/gltfanim.go's function of the same name.
func animatedNodes(doc *gltf.Document) map[int]bool {
	animated := map[int]bool{}
	for _, animation := range doc.Animations {
		if animation == nil {
			continue
		}
		for _, channel := range animation.Channels {
			if channel == nil || channel.Target.Node == nil {
				continue
			}
			switch channel.Target.Path {
			case gltf.TRSTranslation, gltf.TRSRotation, gltf.TRSScale:
				animated[*channel.Target.Node] = true
			}
		}
	}
	return animated
}

// walkNode mirrors scene/gltfload.go's walkNode, minus everything that does not
// change which conversions happen: the transforms, the names and the lights.
func walkNode(
	doc *gltf.Document, index int, visited map[int]bool, chain *int,
	animated map[int]bool, emit func(int, func(int, *gltf.Primitive) geoKey),
) {
	if index < 0 || index >= len(doc.Nodes) || doc.Nodes[index] == nil || visited[index] {
		return
	}
	visited[index] = true
	node := doc.Nodes[index]

	skin, joint := -1, -1
	switch {
	case node.Skin != nil:
		skin = *node.Skin
	case node.Mesh != nil && (animated[index] || *chain > 0):
		// claimPlain gives this node its own joint; the node index stands in
		// for the joint number, which is all the key needs it for.
		joint = index
	}
	if node.Mesh != nil {
		emit(*node.Mesh, func(at int, primitive *gltf.Primitive) geoKey {
			return geoKey{
				mesh: *node.Mesh, primitive: at,
				tangents: needsTangents(doc, primitive),
				skin:     skin, joint: joint,
			}
		})
	}
	if animated[index] {
		*chain++
	}
	for _, child := range node.Children {
		walkNode(doc, child, visited, chain, animated, emit)
	}
	if animated[index] {
		*chain--
	}
}

// needsTangents reports whether the primitive's material has a normal map, which
// is what makes scene generate a tangent frame for a file that shipped none
// (scene/gltfload.go:551).
func needsTangents(doc *gltf.Document, primitive *gltf.Primitive) bool {
	if primitive.Material == nil || *primitive.Material < 0 || *primitive.Material >= len(doc.Materials) {
		return false
	}
	material := doc.Materials[*primitive.Material]
	return material != nil && material.NormalTexture != nil && material.NormalTexture.Index != nil
}

// convert mirrors scene/gltfmesh.go's convertPrimitive, in its order, counting
// bytes instead of filling them.
func convert(doc *gltf.Document, file string, mesh, at int, primitive *gltf.Primitive, key geoKey) geo {
	g := geo{
		file: file, mesh: doc.Meshes[mesh].Name, meshIdx: mesh, primAt: at,
		mode: modeName(primitive.Mode), attrs: map[string]attr{}, instances: 1,
		maxJoint: -1,
	}
	for _, name := range attrOrder {
		accessor, ok := attributeAccessor(doc, primitive.Attributes, name)
		if !ok {
			continue
		}
		g.attrs[name] = attr{
			present:    true,
			component:  componentName(accessor.ComponentType),
			normalized: accessor.Normalized,
		}
	}
	position, ok := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if !ok {
		g.skipped = "no POSITION"
		return g
	}
	g.srcVertices, g.vertices = position.Count, position.Count

	// POINTS has no gfx topology and the primitive is dropped whole
	// (scene/gltfmesh.go's errPointTopology).
	if primitive.Mode == gltf.PrimitivePoints {
		g.skipped = "POINTS has no gfx topology"
		return g
	}

	targets, stride := morphShape(primitive)
	g.targets, g.morphStride = targets, stride

	indices := indexCount(doc, primitive)
	g.srcIndices = indices
	if accessor, ok := accessorAt(doc, primitive.Indices); ok {
		g.indexType = componentName(accessor.ComponentType)
	} else {
		g.indexType = "none"
	}

	// convertTopology: strips, fans and loops expand into lists, and a
	// non-indexed primitive gains the identity sequence rather than duplicating
	// vertices.
	source := indices
	if source == 0 {
		source = g.srcVertices
	}
	triangleList := false
	switch primitive.Mode {
	case gltf.PrimitiveTriangles:
		triangleList = true
		g.indices = indices
	case gltf.PrimitiveLines:
		g.indices = indices
	case gltf.PrimitiveLineStrip:
		g.indices = max(source-1, 0) * 2
	case gltf.PrimitiveLineLoop:
		g.indices = source * 2
	case gltf.PrimitiveTriangleStrip:
		triangleList = true
		g.indices = max(source-2, 0) * 3
	case gltf.PrimitiveTriangleFan:
		triangleList = true
		g.indices = max(source-2, 0) * 3
	}

	_, hasNormal := primitive.Attributes[gltf.NORMAL]
	_, hasTangent := primitive.Attributes[gltf.TANGENT]

	if triangleList && !hasNormal {
		// unweld: every index gets its own vertex, and the index buffer becomes
		// the identity over them. This is what flat normals cost.
		unwelded := g.indices
		if unwelded == 0 {
			unwelded = g.srcVertices
		}
		g.vertices, g.indices, g.unwelded = unwelded, unwelded, true
		g.genNormal = true
	}
	if triangleList && !hasTangent && key.tangents {
		g.genTangent = true
	}

	g.skin = skinNone
	switch {
	case key.joint >= 0:
		g.skin = skinPlain
	case key.skin >= 0:
		// bindGeometryJoints only marks the geometry skinned when some vertex
		// carried a non-zero weight; a skinned node whose mesh has no
		// WEIGHTS_0 draws unskinned.
		if g.attrs[gltf.WEIGHTS_0].present {
			g.skin = skinReal
		}
	}

	g.uvMin, g.uvMax, g.hasUV = uvRange(doc, primitive)
	g.maxJoint = maxJoint(doc, primitive)
	return g
}

// morphShape is scene/gltfmorph.go's readMorphTargets, reduced to the two
// numbers that decide the block's size: how many targets, and the vec4 slots
// one vertex spends in one target.
func morphShape(primitive *gltf.Primitive) (targets, stride int) {
	if len(primitive.Targets) == 0 {
		return 0, 0
	}
	_, hasNormal := primitive.Attributes[gltf.NORMAL]
	_, hasTangent := primitive.Attributes[gltf.TANGENT]
	authored := 0b001
	if hasNormal {
		authored |= 0b010
		if hasTangent {
			authored |= 0b100
		}
	}
	mask := 0
	for _, target := range primitive.Targets {
		if _, named := target[gltf.POSITION]; named {
			mask |= 0b001
		}
		if _, named := target[gltf.NORMAL]; named {
			mask |= 0b010
		}
		if _, named := target[gltf.TANGENT]; named {
			mask |= 0b100
		}
	}
	mask &= authored
	if mask == 0 {
		return 0, 0
	}
	// prefix: widen to the contiguous run ending at the highest set bit, so the
	// stride is recoverable from itself alone.
	for bit := 2; bit >= 0; bit-- {
		if mask&(1<<bit) != 0 {
			mask = 1<<(bit+1) - 1
			break
		}
	}
	slots := 0
	for bit := range 3 {
		if mask&(1<<bit) != 0 {
			slots++
		}
	}
	return len(primitive.Targets), slots
}

func indexCount(doc *gltf.Document, primitive *gltf.Primitive) int {
	accessor, ok := accessorAt(doc, primitive.Indices)
	if !ok {
		return 0
	}
	return accessor.Count
}

func uvRange(doc *gltf.Document, primitive *gltf.Primitive) (lo, hi [2]float32, ok bool) {
	accessor, has := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_0)
	if !has {
		return lo, hi, false
	}
	coords, err := modeler.ReadTextureCoord(doc, accessor, nil)
	if err != nil || len(coords) == 0 {
		return lo, hi, false
	}
	lo = [2]float32{math.MaxFloat32, math.MaxFloat32}
	hi = [2]float32{-math.MaxFloat32, -math.MaxFloat32}
	for _, uv := range coords {
		for axis := range 2 {
			lo[axis] = min(lo[axis], uv[axis])
			hi[axis] = max(hi[axis], uv[axis])
		}
	}
	return lo, hi, true
}

func maxJoint(doc *gltf.Document, primitive *gltf.Primitive) int {
	accessor, has := attributeAccessor(doc, primitive.Attributes, gltf.JOINTS_0)
	if !has {
		return -1
	}
	joints, err := modeler.ReadJoints(doc, accessor, nil)
	if err != nil {
		return -1
	}
	highest := -1
	for _, joint := range joints {
		for _, slot := range joint {
			highest = max(highest, int(slot))
		}
	}
	return highest
}

func accessorAt(doc *gltf.Document, index *int) (*gltf.Accessor, bool) {
	if index == nil || *index < 0 || *index >= len(doc.Accessors) || doc.Accessors[*index] == nil {
		return nil, false
	}
	return doc.Accessors[*index], true
}

func attributeAccessor(doc *gltf.Document, attributes gltf.PrimitiveAttributes, name string) (*gltf.Accessor, bool) {
	index, named := attributes[name]
	if !named {
		return nil, false
	}
	return accessorAt(doc, &index)
}

func componentName(c gltf.ComponentType) string {
	switch c {
	case gltf.ComponentByte:
		return "i8"
	case gltf.ComponentUbyte:
		return "u8"
	case gltf.ComponentShort:
		return "i16"
	case gltf.ComponentUshort:
		return "u16"
	case gltf.ComponentUint:
		return "u32"
	case gltf.ComponentFloat:
		return "f32"
	}
	return "?"
}

func modeName(mode gltf.PrimitiveMode) string {
	switch mode {
	case gltf.PrimitivePoints:
		return "POINTS"
	case gltf.PrimitiveLines:
		return "LINES"
	case gltf.PrimitiveLineLoop:
		return "LINE_LOOP"
	case gltf.PrimitiveLineStrip:
		return "LINE_STRIP"
	case gltf.PrimitiveTriangles:
		return "TRIANGLES"
	case gltf.PrimitiveTriangleStrip:
		return "TRI_STRIP"
	case gltf.PrimitiveTriangleFan:
		return "TRI_FAN"
	}
	return "?"
}

// ---- byte arithmetic -------------------------------------------------------

// has reports whether the converted vertex carries meaningful data in one
// attribute slot: the file authored it, or scene generated it.
func (g geo) has(name string) bool {
	switch name {
	case gltf.POSITION:
		return true
	case gltf.NORMAL:
		return g.attrs[name].present || g.genNormal
	case gltf.TANGENT:
		return g.attrs[name].present || g.genTangent
	case gltf.JOINTS_0, gltf.WEIGHTS_0:
		// A plain binding writes one constant joint over every vertex, so the
		// data is real but it is per primitive, not per vertex. It is counted
		// as absent for the presence axis and called out separately.
		return g.skin == skinReal
	}
	return g.attrs[name].present
}

// presenceStride is bytes per vertex carrying only what the mesh actually uses,
// at today's types.
func (g geo) presenceStride() int {
	stride := todayPosition
	if g.has(gltf.NORMAL) {
		stride += todayNormal
	}
	if g.has(gltf.TANGENT) {
		stride += todayTangent
	}
	if g.has(gltf.TEXCOORD_0) {
		stride += todayUV
	}
	if g.has(gltf.TEXCOORD_1) {
		stride += todayUV
	}
	if g.has(gltf.COLOR_0) {
		stride += todayColor
	}
	if g.has(gltf.JOINTS_0) {
		stride += todayJoints + todayWeights
	}
	return stride
}

// narrowStride is bytes per vertex with all eight attributes kept and the
// candidate types applied. It is the same number for every primitive; it varies
// only with whether position is narrowed too.
func narrowStride(quantizedPosition bool) int {
	position := narrowPosition
	if quantizedPosition {
		position = narrowPositionQuantized
	}
	return position + narrowNormal + narrowTangent + narrowUV + narrowUV +
		narrowColor + narrowJoints + narrowWeights
}

// bothStride is bytes per vertex with both axes applied.
func (g geo) bothStride(quantizedPosition bool) int {
	stride := narrowPosition
	if quantizedPosition {
		stride = narrowPositionQuantized
	}
	if g.has(gltf.NORMAL) {
		stride += narrowNormal
	}
	if g.has(gltf.TANGENT) {
		stride += narrowTangent
	}
	if g.has(gltf.TEXCOORD_0) {
		stride += narrowUV
	}
	if g.has(gltf.TEXCOORD_1) {
		stride += narrowUV
	}
	if g.has(gltf.COLOR_0) {
		stride += narrowColor
	}
	if g.has(gltf.JOINTS_0) {
		stride += narrowJoints + narrowWeights
	}
	// WebGPU requires the vertex stride to be a multiple of 4; every candidate
	// width above already is, so this only guards the arithmetic.
	if stride%4 != 0 {
		stride += 4 - stride%4
	}
	return stride
}

func (g geo) todayVertexBytes() int { return g.vertices * todayStride }
func (g geo) todayIndexBytes() int  { return g.indices * 4 }
func (g geo) morphBytes() int       { return g.targets * g.vertices * g.morphStride * 16 }
func (g geo) uint16Legal() bool     { return g.vertices > 0 && g.vertices < 65536 }
func (g geo) uint16IndexBytes() int {
	if !g.uint16Legal() {
		return g.todayIndexBytes()
	}
	// Rounded up to 4, because a WebGPU index buffer's size wants the alignment
	// even when its elements are two bytes.
	bytes := g.indices * 2
	if bytes%4 != 0 {
		bytes += 4 - bytes%4
	}
	return bytes
}

// ---- reporting -------------------------------------------------------------

func report(all []geo) {
	live := make([]geo, 0, len(all))
	for _, g := range all {
		if g.skipped == "" {
			live = append(live, g)
		}
	}
	slices.SortStableFunc(live, func(a, b geo) int {
		return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.meshIdx, b.meshIdx), cmp.Compare(a.primAt, b.primAt))
	})

	perPrimitive(all)
	perFile(live)
	axes(live)
	contentSubtotal(live)
	duplicates(live)
	quantizedTwins(live)
	extras(all)
}

// contentWeighted is the subset of the vendored set that resembles a game's
// content rather than a glTF feature test: a skinned animated character and a
// textured vehicle with a wheel animation. The ticket asks for this explicitly,
// because the grand total is dominated by CompareBaseColor's three 9216-vertex
// comparison grids and averaging over those measures the wrong thing.
var contentWeighted = map[string]bool{"Fox": true, "CesiumMilkTruck": true}

func contentSubtotal(live []geo) {
	var content, feature totals
	for _, g := range live {
		if contentWeighted[g.file] {
			accumulate(&content, g)
			continue
		}
		accumulate(&feature, g)
	}
	fmt.Println("### Content-weighted subtotal")
	fmt.Println()
	fmt.Println("| set | today | presence | precision | both | both, quantized pos |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: |")
	row := func(name string, t totals) {
		fmt.Printf("| %s | %s | %s (%s) | %s (%s) | %s (%s) | %s (%s) |\n",
			name, bytes(t.today),
			bytes(t.presence), pct(t.presence, t.today),
			bytes(t.narrow), pct(t.narrow, t.today),
			bytes(t.both), pct(t.both, t.today),
			bytes(t.bothQ), pct(t.bothQ, t.today))
	}
	row("Fox + CesiumMilkTruck", content)
	row("everything else", feature)
	fmt.Println()
}

// duplicates reports the same glTF primitive converted more than once.
//
// This is not instancing overhead: it is one primitive's vertex buffer held in
// memory several times over, because scene/gltfload.go's geometryKey includes
// the skin binding and scene/gltfanim.go's bindGeometryJoints writes the joint
// index into every vertex. A mesh referenced by nine animated nodes is nine
// conversions of the same geometry, differing only in a constant.
func duplicates(live []geo) {
	type site struct {
		file       string
		mesh, prim int
	}
	counts := map[site][]geo{}
	var order []site
	for _, g := range live {
		key := site{g.file, g.meshIdx, g.primAt}
		if _, seen := counts[key]; !seen {
			order = append(order, key)
		}
		counts[key] = append(counts[key], g)
	}
	fmt.Println("### The same primitive converted more than once")
	fmt.Println()
	fmt.Println("| file | prim | conversions | why | bytes once | bytes stored | wasted |")
	fmt.Println("| --- | --- | ---: | --- | ---: | ---: | ---: |")
	wasted := 0
	for _, key := range order {
		group := counts[key]
		if len(group) < 2 {
			continue
		}
		one := group[0].todayVertexBytes() + group[0].todayIndexBytes()
		stored := one * len(group)
		wasted += stored - one
		fmt.Printf("| %s | %d.%d | %d | %s binding per node | %s | %s | %s |\n",
			key.file, key.mesh, key.prim, len(group), group[0].skin,
			bytes(one), bytes(stored), bytes(stored-one))
	}
	fmt.Printf("\nDuplicated conversions cost %s across the set.\n\n", bytes(wasted))
}

func perPrimitive(all []geo) {
	fmt.Println("### Per primitive, as scene converts it")
	fmt.Println()
	fmt.Println("| file | prim | mode | attributes in file | src verts | verts | idx | idx type | skin | targets x stride |")
	fmt.Println("| --- | --- | --- | --- | ---: | ---: | ---: | --- | --- | --- |")
	for _, g := range all {
		if g.skipped != "" {
			fmt.Printf("| %s | %d.%d | %s | *skipped: %s* | %d | - | - | - | - | - |\n",
				g.file, g.meshIdx, g.primAt, g.mode, g.skipped, g.srcVertices)
			continue
		}
		var carried []string
		for _, name := range attrOrder {
			a := g.attrs[name]
			if !a.present {
				continue
			}
			label := shortName[name] + ":" + a.component
			if a.normalized {
				label += "n"
			}
			carried = append(carried, label)
		}
		generated := ""
		if g.genNormal {
			generated += " +NRM*"
		}
		if g.genTangent {
			generated += " +TAN*"
		}
		verts := fmt.Sprint(g.vertices)
		if g.unwelded {
			verts += " ↑"
		}
		morph := "-"
		if g.targets > 0 {
			morph = fmt.Sprintf("%d x %d", g.targets, g.morphStride)
		}
		fmt.Printf("| %s | %d.%d | %s | %s%s | %d | %s | %d | %s | %s | %s |\n",
			g.file, g.meshIdx, g.primAt, g.mode, strings.Join(carried, " "), generated,
			g.srcVertices, verts, g.indices, g.indexType, g.skin, morph)
	}
	fmt.Println()
}

type totals struct {
	vertices, indices                         int
	today, presence, narrow, both             int
	narrowQ, bothQ                            int
	indexToday, index16                       int
	morph                                     int
	uint16Ok, primitives, plainSkin, realSkin int
}

func accumulate(t *totals, g geo) {
	t.primitives++
	t.vertices += g.vertices
	t.indices += g.indices
	t.today += g.todayVertexBytes()
	t.presence += g.vertices * g.presenceStride()
	t.narrow += g.vertices * narrowStride(false)
	t.narrowQ += g.vertices * narrowStride(true)
	t.both += g.vertices * g.bothStride(false)
	t.bothQ += g.vertices * g.bothStride(true)
	t.indexToday += g.todayIndexBytes()
	t.index16 += g.uint16IndexBytes()
	t.morph += g.morphBytes()
	if g.uint16Legal() {
		t.uint16Ok++
	}
	switch g.skin {
	case skinPlain:
		t.plainSkin++
	case skinReal:
		t.realSkin++
	}
}

func perFile(live []geo) {
	fmt.Println("### Per file, bytes today")
	fmt.Println()
	fmt.Println("| file | prims | verts | vertex bytes | idx | index bytes | morph bytes | total | whole .glb on disk |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, file := range filesOf(live) {
		var t totals
		for _, g := range live {
			if g.file == file {
				accumulate(&t, g)
			}
		}
		fmt.Printf("| %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			file, t.primitives, comma(t.vertices), bytes(t.today), comma(t.indices),
			bytes(t.indexToday), bytes(t.morph), bytes(t.today+t.indexToday+t.morph),
			bytes(onDisk[file]))
	}
	var grand totals
	for _, g := range live {
		accumulate(&grand, g)
	}
	disk := 0
	for _, size := range onDisk {
		disk += size
	}
	fmt.Printf("| **all** | **%d** | **%s** | **%s** | **%s** | **%s** | **%s** | **%s** | **%s** |\n",
		grand.primitives, comma(grand.vertices), bytes(grand.today), comma(grand.indices),
		bytes(grand.indexToday), bytes(grand.morph), bytes(grand.today+grand.indexToday+grand.morph),
		bytes(disk))
	fmt.Println()
}

func axes(live []geo) {
	fmt.Println("### Per file, vertex bytes under each axis")
	fmt.Println()
	fmt.Println("| file | today | presence | precision | both | both, quantized pos | idx u32 | idx u16 |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, file := range filesOf(live) {
		var t totals
		for _, g := range live {
			if g.file == file {
				accumulate(&t, g)
			}
		}
		fmt.Printf("| %s | %s | %s (%s) | %s (%s) | %s (%s) | %s (%s) | %s | %s (%s) |\n",
			file, bytes(t.today),
			bytes(t.presence), pct(t.presence, t.today),
			bytes(t.narrow), pct(t.narrow, t.today),
			bytes(t.both), pct(t.both, t.today),
			bytes(t.bothQ), pct(t.bothQ, t.today),
			bytes(t.indexToday), bytes(t.index16), pct(t.index16, t.indexToday))
	}
	var grand totals
	for _, g := range live {
		accumulate(&grand, g)
	}
	fmt.Printf("| **all** | **%s** | **%s (%s)** | **%s (%s)** | **%s (%s)** | **%s (%s)** | **%s** | **%s (%s)** |\n",
		bytes(grand.today),
		bytes(grand.presence), pct(grand.presence, grand.today),
		bytes(grand.narrow), pct(grand.narrow, grand.today),
		bytes(grand.both), pct(grand.both, grand.today),
		bytes(grand.bothQ), pct(grand.bothQ, grand.today),
		bytes(grand.indexToday), bytes(grand.index16), pct(grand.index16, grand.indexToday))
	fmt.Printf("\nuint16 indices legal for %d of %d primitives (every primitive under 65536 vertices).\n",
		grand.uint16Ok, grand.primitives)
	fmt.Printf("Skin bindings: %d real, %d degenerate single-joint, %d none, of %d primitives.\n\n",
		grand.realSkin, grand.plainSkin, grand.primitives-grand.realSkin-grand.plainSkin, grand.primitives)
}

func quantizedTwins(live []geo) {
	fmt.Println("### AnimatedMorphCube against AnimatedMorphCube-Quantized")
	fmt.Println()
	fmt.Println("| file | attributes in file | verts | vertex bytes | idx type | index bytes | morph bytes | total |")
	fmt.Println("| --- | --- | ---: | ---: | --- | ---: | ---: | ---: |")
	for _, g := range live {
		if !strings.HasPrefix(g.file, "AnimatedMorphCube") {
			continue
		}
		var carried []string
		for _, name := range attrOrder {
			if a := g.attrs[name]; a.present {
				label := shortName[name] + ":" + a.component
				if a.normalized {
					label += "n"
				}
				carried = append(carried, label)
			}
		}
		fmt.Printf("| %s | %s | %d | %s | %s | %s | %s | %s |\n",
			g.file, strings.Join(carried, " "), g.vertices, bytes(g.todayVertexBytes()),
			g.indexType, bytes(g.todayIndexBytes()), bytes(g.morphBytes()),
			bytes(g.todayVertexBytes()+g.todayIndexBytes()+g.morphBytes()))
	}
	fmt.Println()
}

func extras(all []geo) {
	fmt.Println("### UV range and joint range")
	fmt.Println()
	fmt.Println("| file | prim | UV0 min | UV0 max | outside 0..1 | max joint index |")
	fmt.Println("| --- | --- | --- | --- | --- | ---: |")
	for _, g := range all {
		if !g.hasUV {
			continue
		}
		outside := "no"
		if g.uvMin[0] < 0 || g.uvMin[1] < 0 || g.uvMax[0] > 1 || g.uvMax[1] > 1 {
			outside = "**yes**"
		}
		joint := "-"
		if g.maxJoint >= 0 {
			joint = fmt.Sprint(g.maxJoint)
		}
		fmt.Printf("| %s | %d.%d | (%.4f, %.4f) | (%.4f, %.4f) | %s | %s |\n",
			g.file, g.meshIdx, g.primAt, g.uvMin[0], g.uvMin[1], g.uvMax[0], g.uvMax[1], outside, joint)
	}
	fmt.Println()
}

func filesOf(live []geo) []string {
	var files []string
	seen := map[string]bool{}
	for _, g := range live {
		if !seen[g.file] {
			seen[g.file] = true
			files = append(files, g.file)
		}
	}
	return files
}

func bytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func pct(part, whole int) string {
	if whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, digit := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digit)
	}
	return string(out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
