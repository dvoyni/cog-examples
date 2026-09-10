// Command morphstats measures what scene's morph delta store actually contains
// across the vendored glTF assets, for the delta-precision decision.
//
// It is throwaway measurement code, a sibling of cmd/meshbytes and
// cmd/attrstats. meshbytes counted delta bytes from the shape alone - targets x
// vertices x stride x 16 - and never read a delta value. This reads them: how
// many are exactly zero, how large the non-zero ones get, and how much error
// each candidate storage format would introduce, per slot and accumulated over
// every target at once.
//
// Decoding mirrors scene/gltfmorph.go's readMorphTargets: the mask is the union
// across a primitive's targets, intersected with the base primitive's authored
// attributes and widened to a prefix, so a NORMAL delta on a primitive with no
// authored NORMAL is dropped here exactly as scene drops it. Accessors go
// through modeler.ReadAccessor, which applies sparse substitution, so a sparse
// target arrives dense - which is what scene stores.
//
// The (mesh, primitive) pair is the unit. A primitive reached twice is measured
// once: a second copy of the same numbers would only skew the statistics.
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

func main() {
	dir := flag.String("assets", "assets", "directory of vendored glTF assets")
	flag.Parse()

	files, err := filepath.Glob(filepath.Join(*dir, "*", "*.glb"))
	if err != nil {
		fail(err)
	}
	sort.Strings(files)

	var all []prim
	for _, file := range files {
		if strings.Contains(filepath.ToSlash(file), "/broken/") {
			continue
		}
		measured, err := measure(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
			continue
		}
		all = append(all, measured...)
	}
	slices.SortStableFunc(all, func(a, b prim) int {
		return cmp.Or(cmp.Compare(a.file, b.file),
			cmp.Compare(a.meshIdx, b.meshIdx), cmp.Compare(a.primAt, b.primAt))
	})
	report(all)
}

// ---- slots -----------------------------------------------------------------

const (
	slotPosition = iota
	slotNormal
	slotTangent
	slotCount
)

var slotName = [slotCount]string{"POSITION", "NORMAL", "TANGENT"}
var slotAttr = [slotCount]string{gltf.POSITION, gltf.NORMAL, gltf.TANGENT}

// ---- the measured record ---------------------------------------------------

// slotStats is one primitive's deltas for one slot, over every target.
type slotStats struct {
	present bool
	records int // targets * vertexCount
	zero    int // records whose three components are all exactly zero
	// maxAbs is the largest absolute component over every target, which is the
	// symmetric range a per-primitive fixed-point encoding would use.
	maxAbs float64
	// maxAbsPerTarget is the same, per target: the range a per-target encoding
	// would use.
	maxAbsPerTarget []float64
	// maxLen is the largest delta magnitude.
	maxLen float64
	// sumAbs and countAbs give the mean absolute component over non-zero
	// records, which says how much of the range the typical delta uses.
	sumAbs   float64
	countAbs int

	// err[c] accumulates the round-trip error of candidate c over every
	// component of every record.
	err [candidateCount]errStats
	// accum[c] is the worst case with every target at weight 1 at once: for
	// each vertex, the sum over targets of that target's per-component error,
	// maximised over vertices and components.
	accum [candidateCount]float64
}

type errStats struct {
	maxAbs float64
	sumAbs float64
	count  int
}

func (e *errStats) add(v float64) {
	a := math.Abs(v)
	e.maxAbs = math.Max(e.maxAbs, a)
	e.sumAbs += a
	e.count++
}

func (e errStats) mean() float64 {
	if e.count == 0 {
		return 0
	}
	return e.sumAbs / float64(e.count)
}

// prim is one measured (mesh, primitive) pair that carries targets.
type prim struct {
	file    string
	meshIdx int
	primAt  int
	verts   int
	targets int
	stride  int // vec4 slots per vertex per target, scene's morphStride
	// diag is the primitive's position AABB diagonal, the scale a position
	// delta error has to be judged against.
	diag float64
	slot [slotCount]slotStats
	// srcBytes is what the target accessors occupy in the file, and srcSparse
	// counts those stored as glTF sparse accessors: the file's own statement
	// that most of a target is zero, which scene densifies away on load.
	srcBytes  int
	srcSparse int
	srcTypes  map[string]bool
	shape     sparseShape
}

// sparseShape describes where a target's non-zero records sit, which is what
// decides whether a sparse scheme can address them cheaply.
//
// The unit is the whole record, not the slot: a vertex is live for a target if
// any of that target's slots moves it, because the record is what the shader
// addresses. slotDisagree counts live records where one slot moves and another
// does not - the waste record granularity costs against per-slot granularity.
type sparseShape struct {
	recordBytes  int   // under the chosen per-slot widths: 8 pos + 4 nrm + 4 tan
	live         []int // per target, records with any non-zero slot
	span         []int // per target, last live index - first + 1
	runs         []int // per target, maximal runs of consecutive live indices
	slotDisagree int
}

func (s sparseShape) sum(of []int) int {
	total := 0
	for _, v := range of {
		total += v
	}
	return total
}

// costs, in bytes, of the schemes that could address it.
func (s sparseShape) dense(verts int) int { return len(s.live) * verts * s.recordBytes }
func (s sparseShape) byRange() int        { return s.sum(s.span)*s.recordBytes + len(s.span)*8 }
func (s sparseShape) byRuns() int         { return s.sum(s.live)*s.recordBytes + s.sum(s.runs)*8 }
func (s sparseShape) byIndex() int        { return s.sum(s.live) * (s.recordBytes + 4) }

// bytesToday is what scene stores for this primitive's deltas now.
func (p prim) bytesToday() int { return p.targets * p.verts * p.stride * 16 }

// ---- candidate formats -----------------------------------------------------

const (
	candHalf        = iota // 3 x f16, in a vec2<u32>: 8 bytes per slot record
	candSnorm16Prim        // 3 x snorm16 against a per-primitive range: 8 bytes
	candSnorm16Tgt         // 3 x snorm16 against a per-target range: 8 bytes
	candSnorm8Prim         // 3 x snorm8 against a per-primitive range: 4 bytes
	candSnorm8Tgt          // 3 x snorm8 against a per-target range: 4 bytes
	candidateCount
)

var candName = [candidateCount]string{
	"f16x3 (8B)", "snorm16x3 prim range (8B)", "snorm16x3 target range (8B)",
	"snorm8x3 prim range (4B)", "snorm8x3 target range (4B)",
}

var candBytes = [candidateCount]int{8, 8, 8, 4, 4}

// roundHalf returns v rounded to the nearest IEEE binary16 value, ties to even.
// Subnormals are handled: below 2^-14 the quantum is a flat 2^-24, which is the
// whole reason a half float suits a quantity clustered near zero.
func roundHalf(v float64) float64 {
	if v == 0 || math.IsNaN(v) {
		return v
	}
	sign := 1.0
	a := v
	if a < 0 {
		sign, a = -1, -a
	}
	if a >= 65520 { // rounds to infinity
		return sign * math.Inf(1)
	}
	var q float64
	if a < math.Ldexp(1, -14) {
		q = math.Ldexp(1, -24)
	} else {
		q = math.Ldexp(1, math.Ilogb(a)-10)
	}
	return sign * math.RoundToEven(a/q) * q
}

// roundSnorm returns v rounded to the nearest of 2n+1 evenly spaced values over
// [-r, r], which is what a snorm integer of that width against range r stores.
func roundSnorm(v, r float64, n int) float64 {
	if r == 0 {
		return 0
	}
	q := r / float64(n)
	k := math.RoundToEven(v / q)
	k = math.Max(-float64(n), math.Min(float64(n), k))
	return k * q
}

// roundTo applies candidate c to one component, given the ranges in force.
func roundTo(c int, v, primRange, targetRange float64) float64 {
	switch c {
	case candHalf:
		return roundHalf(v)
	case candSnorm16Prim:
		return roundSnorm(v, primRange, 32767)
	case candSnorm16Tgt:
		return roundSnorm(v, targetRange, 32767)
	case candSnorm8Prim:
		return roundSnorm(v, primRange, 127)
	case candSnorm8Tgt:
		return roundSnorm(v, targetRange, 127)
	}
	return v
}

// ---- measurement -----------------------------------------------------------

func measure(file string) ([]prim, error) {
	doc, err := gltf.Open(file)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	var out []prim
	for meshIdx, mesh := range doc.Meshes {
		if mesh == nil {
			continue
		}
		for primAt, primitive := range mesh.Primitives {
			if primitive == nil || len(primitive.Targets) == 0 {
				continue
			}
			measured, err := measurePrim(doc, primitive)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s mesh %d prim %d: %v\n", name, meshIdx, primAt, err)
				continue
			}
			if measured.targets == 0 {
				continue
			}
			measured.file, measured.meshIdx, measured.primAt = name, meshIdx, primAt
			out = append(out, measured)
		}
	}
	return out, nil
}

// maskOf mirrors readMorphTargets: the union across targets, intersected with
// what the base primitive authored, widened to a prefix.
func maskOf(primitive *gltf.Primitive) [slotCount]bool {
	_, hasNormal := primitive.Attributes[gltf.NORMAL]
	_, hasTangent := primitive.Attributes[gltf.TANGENT]
	authored := [slotCount]bool{true, hasNormal, hasNormal && hasTangent}

	var named [slotCount]bool
	for _, target := range primitive.Targets {
		for s := range slotCount {
			if _, ok := target[slotAttr[s]]; ok {
				named[s] = true
			}
		}
	}
	var mask [slotCount]bool
	prefix := true
	for s := range slotCount {
		prefix = prefix && named[s] && authored[s]
		mask[s] = prefix
	}
	return mask
}

func measurePrim(doc *gltf.Document, primitive *gltf.Primitive) (prim, error) {
	position, ok := accessorOf(doc, primitive.Attributes, gltf.POSITION)
	if !ok {
		return prim{}, nil
	}
	verts := int(position.Count)
	if verts == 0 {
		return prim{}, nil
	}
	mask := maskOf(primitive)
	stride := 0
	for s := range slotCount {
		if mask[s] {
			stride++
		}
	}
	if stride == 0 {
		return prim{}, nil
	}

	p := prim{verts: verts, targets: len(primitive.Targets), stride: stride}
	p.diag = diagonalOf(doc, position)
	p.srcTypes = map[string]bool{}
	for _, target := range primitive.Targets {
		for s := range slotCount {
			if !mask[s] {
				continue
			}
			accessor, ok := accessorOf(doc, target, slotAttr[s])
			if !ok {
				continue
			}
			p.srcTypes[componentName(accessor)] = true
			if accessor.Sparse != nil {
				p.srcSparse++
				p.srcBytes += int(accessor.Sparse.Count) * (4 + 3*componentWidth(accessor))
				continue
			}
			p.srcBytes += int(accessor.Count) * 3 * componentWidth(accessor)
		}
	}

	// deltas[s][target] is one target's components for slot s, three per
	// vertex, zero where the target does not name the slot.
	var deltas [slotCount][][]float64
	for s := range slotCount {
		if !mask[s] {
			continue
		}
		deltas[s] = make([][]float64, p.targets)
		for t, target := range primitive.Targets {
			values := make([]float64, verts*3)
			accessor, ok := accessorOf(doc, target, slotAttr[s])
			if ok {
				if err := readVec3(doc, accessor, verts, values); err != nil {
					return prim{}, err
				}
			}
			deltas[s][t] = values
		}
	}

	for s := range slotCount {
		if !mask[s] {
			continue
		}
		p.slot[s] = statsFor(deltas[s], verts)
	}
	p.shape = shapeOf(deltas, mask, verts, p.targets)
	return p, nil
}

func statsFor(targets [][]float64, verts int) slotStats {
	st := slotStats{present: true, records: len(targets) * verts}
	st.maxAbsPerTarget = make([]float64, len(targets))

	for t, values := range targets {
		for i := range verts {
			x, y, z := values[i*3], values[i*3+1], values[i*3+2]
			if x == 0 && y == 0 && z == 0 {
				st.zero++
				continue
			}
			for _, v := range [3]float64{x, y, z} {
				a := math.Abs(v)
				st.maxAbs = math.Max(st.maxAbs, a)
				st.maxAbsPerTarget[t] = math.Max(st.maxAbsPerTarget[t], a)
				st.sumAbs += a
				st.countAbs++
			}
			st.maxLen = math.Max(st.maxLen, math.Sqrt(x*x+y*y+z*z))
		}
	}

	// The error pass needs the ranges, so it runs second. accum sums each
	// vertex's per-target error across every target, the worst case where every
	// weight is 1 at once.
	for c := range candidateCount {
		sums := make([]float64, verts*3)
		for t, values := range targets {
			for i := range verts * 3 {
				v := values[i]
				e := roundTo(c, v, st.maxAbs, st.maxAbsPerTarget[t]) - v
				st.err[c].add(e)
				sums[i] += math.Abs(e)
			}
		}
		for _, s := range sums {
			st.accum[c] = math.Max(st.accum[c], s)
		}
	}
	return st
}

// slotWidth is the stored width this measurement assumes per slot: position
// earns 16-bit fixed point, normal and tangent 8-bit, both against a
// per-primitive per-slot range.
var slotWidth = [slotCount]int{8, 4, 4}

func shapeOf(deltas [slotCount][][]float64, mask [slotCount]bool, verts, targets int) sparseShape {
	shape := sparseShape{
		live: make([]int, targets), span: make([]int, targets), runs: make([]int, targets),
	}
	for s := range slotCount {
		if mask[s] {
			shape.recordBytes += slotWidth[s]
		}
	}
	for t := range targets {
		first, last, wasLive := -1, -1, false
		for i := range verts {
			live, all := false, true
			for s := range slotCount {
				if !mask[s] {
					continue
				}
				values := deltas[s][t]
				moved := values[i*3] != 0 || values[i*3+1] != 0 || values[i*3+2] != 0
				live = live || moved
				all = all && moved
			}
			if !live {
				wasLive = false
				continue
			}
			if !all {
				shape.slotDisagree++
			}
			shape.live[t]++
			if first < 0 {
				first = i
			}
			last = i
			if !wasLive {
				shape.runs[t]++
			}
			wasLive = true
		}
		if first >= 0 {
			shape.span[t] = last - first + 1
		}
	}
	return shape
}

func accessorOf(doc *gltf.Document, attributes map[string]int, name string) (*gltf.Accessor, bool) {
	at, ok := attributes[name]
	if !ok || at < 0 || at >= len(doc.Accessors) || doc.Accessors[at] == nil {
		return nil, false
	}
	return doc.Accessors[at], true
}

// readVec3 fills out with the accessor's first three components per element,
// dequantised, mirroring scene's readAttribute.
func readVec3(doc *gltf.Document, accessor *gltf.Accessor, verts int, out []float64) error {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return err
	}
	scale := componentScale(accessor)
	put := func(i int, x, y, z float64) {
		if i >= verts {
			return
		}
		out[i*3], out[i*3+1], out[i*3+2] = x*scale, y*scale, z*scale
	}
	switch values := data.(type) {
	case [][3]float32:
		for i, v := range values {
			put(i, float64(v[0]), float64(v[1]), float64(v[2]))
		}
	case [][3]int8:
		for i, v := range values {
			put(i, clampSnorm(float64(v[0]), scale), clampSnorm(float64(v[1]), scale), clampSnorm(float64(v[2]), scale))
		}
	case [][3]int16:
		for i, v := range values {
			put(i, clampSnorm(float64(v[0]), scale), clampSnorm(float64(v[1]), scale), clampSnorm(float64(v[2]), scale))
		}
	case [][3]uint8:
		for i, v := range values {
			put(i, float64(v[0]), float64(v[1]), float64(v[2]))
		}
	case [][3]uint16:
		for i, v := range values {
			put(i, float64(v[0]), float64(v[1]), float64(v[2]))
		}
	case [][4]float32:
		for i, v := range values {
			put(i, float64(v[0]), float64(v[1]), float64(v[2]))
		}
	default:
		return fmt.Errorf("unhandled morph accessor type %T", data)
	}
	return nil
}

// componentName and componentWidth describe how the file stores a delta.
func componentName(accessor *gltf.Accessor) string {
	name := map[gltf.ComponentType]string{
		gltf.ComponentByte: "i8", gltf.ComponentUbyte: "u8",
		gltf.ComponentShort: "i16", gltf.ComponentUshort: "u16",
		gltf.ComponentUint: "u32", gltf.ComponentFloat: "f32",
	}[accessor.ComponentType]
	if accessor.Normalized {
		name += "n"
	}
	if accessor.Sparse != nil {
		name += " sparse"
	}
	return name
}

func componentWidth(accessor *gltf.Accessor) int {
	switch accessor.ComponentType {
	case gltf.ComponentByte, gltf.ComponentUbyte:
		return 1
	case gltf.ComponentShort, gltf.ComponentUshort:
		return 2
	}
	return 4
}

// componentScale is scene/gltfattr.go's rule: a normalised integer accessor
// divides by its type's maximum, everything else is taken as stored.
func componentScale(accessor *gltf.Accessor) float64 {
	if !accessor.Normalized {
		return 1
	}
	switch accessor.ComponentType {
	case gltf.ComponentByte:
		return 1.0 / 127
	case gltf.ComponentUbyte:
		return 1.0 / 255
	case gltf.ComponentShort:
		return 1.0 / 32767
	case gltf.ComponentUshort:
		return 1.0 / 65535
	}
	return 1
}

// clampSnorm applies the -1 floor a normalised signed accessor carries.
func clampSnorm(raw, scale float64) float64 {
	v := raw * scale
	if scale != 1 && v < -1 {
		return -1
	}
	return v
}

// diagonalOf is the primitive's position AABB diagonal, from the accessor's
// declared min and max where it has them.
func diagonalOf(doc *gltf.Document, position *gltf.Accessor) float64 {
	if len(position.Min) < 3 || len(position.Max) < 3 {
		return 0
	}
	var sum float64
	for i := range 3 {
		d := float64(position.Max[i] - position.Min[i])
		sum += d * d
	}
	return math.Sqrt(sum)
}

// ---- report ----------------------------------------------------------------

func report(all []prim) {
	fmt.Println("# Morph delta measurement")
	fmt.Println()
	fmt.Printf("%d morphed primitives across %d assets.\n", len(all), countFiles(all))
	fmt.Println()

	reportShape(all)
	reportSparse(all)
	reportRanges(all)
	reportError(all)
	reportBytes(all)
}

func countFiles(all []prim) int {
	seen := map[string]bool{}
	for _, p := range all {
		seen[p.file] = true
	}
	return len(seen)
}

func reportShape(all []prim) {
	fmt.Println("## What the store holds")
	fmt.Println()
	fmt.Println("`zero` is records whose three components are all exactly zero: a target that")
	fmt.Println("does not move that vertex. scene stores them anyway, one full record each.")
	fmt.Println()
	fmt.Println("| asset | mesh/prim | verts | targets | slots | bytes today | zero records | of total | in file | file stores |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|")
	for _, p := range all {
		zero, records := 0, 0
		for s := range slotCount {
			zero += p.slot[s].zero
			records += p.slot[s].records
		}
		var kinds []string
		for k := range p.srcTypes {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		fmt.Printf("| %s | %d/%d | %d | %d | %s | %s | %d | %.1f%% | %s | %s |\n",
			p.file, p.meshIdx, p.primAt, p.verts, p.targets, slotsOf(p),
			bytes(p.bytesToday()), zero, pct(zero, records),
			bytes(p.srcBytes), strings.Join(kinds, ", "))
	}
	var total, zero, records, src int
	for _, p := range all {
		total += p.bytesToday()
		src += p.srcBytes
		for s := range slotCount {
			zero += p.slot[s].zero
			records += p.slot[s].records
		}
	}
	fmt.Printf("| **all** | | | | | **%s** | **%d** | **%.1f%%** | **%s** | |\n",
		bytes(total), zero, pct(zero, records), bytes(src))
	fmt.Println()
}

// reportSparse says where a target's live records sit, and what each addressing
// scheme would cost against a record narrowed to 8/4/4 bytes per slot.
func reportSparse(all []prim) {
	fmt.Println("## Where the live records sit")
	fmt.Println()
	fmt.Println("A record is live for a target if any of that target's slots moves the vertex:")
	fmt.Println("the record is what the shader addresses, so it is the unit a sparse scheme")
	fmt.Println("would keep or drop. `span` is the index distance from the first live record to")
	fmt.Println("the last, `runs` the number of maximal consecutive stretches. `disagree` counts")
	fmt.Println("live records where one slot moves and another does not - what record")
	fmt.Println("granularity wastes against per-slot granularity.")
	fmt.Println()
	fmt.Println("| asset | mesh/prim | verts | targets | record B | live/target | span/target | runs/target | disagree |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	for _, p := range all {
		sh := p.shape
		fmt.Printf("| %s | %d/%d | %d | %d | %d | %s | %s | %s | %d |\n",
			p.file, p.meshIdx, p.primAt, p.verts, p.targets, sh.recordBytes,
			spread(sh.live), spread(sh.span), spread(sh.runs), sh.slotDisagree)
	}
	fmt.Println()
	fmt.Println("| scheme | total | of today |")
	fmt.Println("|---|---|---|")
	today := 0
	for _, p := range all {
		today += p.bytesToday()
	}
	schemes := []struct {
		name string
		of   func(p prim) int
	}{
		{"dense, 8/4/4 per slot (narrowing alone)", func(p prim) int { return p.shape.dense(p.verts) }},
		{"live span per target, at today's 16 B slots (sparsity alone)", func(p prim) int {
			return p.shape.sum(p.shape.span)*p.stride*16 + p.targets*8
		}},
		{"live span per target, 8/4/4 slots (both)", func(p prim) int { return p.shape.byRange() }},
		{"runs of live records, 8 B per run", func(p prim) int { return p.shape.byRuns() }},
		{"live records + a 4 B vertex index each", func(p prim) int { return p.shape.byIndex() }},
	}
	fmt.Printf("| vec4<f32>, dense (today) | %s | 100%% |\n", bytes(today))
	for _, scheme := range schemes {
		sum := 0
		for _, p := range all {
			sum += scheme.of(p)
		}
		fmt.Printf("| %s | %s | %.1f%% |\n", scheme.name, bytes(sum), 100*float64(sum)/float64(today))
	}
	fmt.Println()
}

// spread renders a per-target series as min..max, or a single value where every
// target agrees.
func spread(of []int) string {
	if len(of) == 0 {
		return "-"
	}
	lo, hi, sum := of[0], of[0], 0
	for _, v := range of {
		lo, hi, sum = min(lo, v), max(hi, v), sum+v
	}
	if lo == hi {
		return fmt.Sprintf("%d", lo)
	}
	return fmt.Sprintf("%d..%d (mean %d)", lo, hi, sum/len(of))
}

func slotsOf(p prim) string {
	var names []string
	for s := range slotCount {
		if p.slot[s].present {
			names = append(names, slotName[s][:3])
		}
	}
	return strings.Join(names, "+")
}

func reportRanges(all []prim) {
	fmt.Println("## The magnitudes, per slot")
	fmt.Println()
	fmt.Println("`max |c|` is the largest absolute component, the symmetric range a per-primitive")
	fmt.Println("fixed-point encoding would carry. `mean |c|` is over non-zero records only.")
	fmt.Println("`diag` is the primitive's position AABB diagonal; `max/diag` says how far a")
	fmt.Println("position delta reaches relative to the thing it is deforming.")
	fmt.Println()
	fmt.Println("| asset | mesh/prim | slot | max \\|c\\| | mean \\|c\\| | max len | diag | max/diag | target ranges lo..hi |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	for _, p := range all {
		for s := range slotCount {
			st := p.slot[s]
			if !st.present {
				continue
			}
			lo, hi := math.Inf(1), 0.0
			for _, r := range st.maxAbsPerTarget {
				lo, hi = math.Min(lo, r), math.Max(hi, r)
			}
			mean := 0.0
			if st.countAbs > 0 {
				mean = st.sumAbs / float64(st.countAbs)
			}
			rel := "-"
			if s == slotPosition && p.diag > 0 {
				rel = fmt.Sprintf("%.3f", st.maxAbs/p.diag)
			}
			fmt.Printf("| %s | %d/%d | %s | %.5g | %.5g | %.5g | %.5g | %s | %.4g .. %.4g |\n",
				p.file, p.meshIdx, p.primAt, slotName[s],
				st.maxAbs, mean, st.maxLen, p.diag, rel, lo, hi)
		}
	}
	fmt.Println()
}

func reportError(all []prim) {
	fmt.Println("## Round-trip error, per candidate")
	fmt.Println()
	fmt.Println("`max` and `mean` are per component over every record of every target.")
	fmt.Println("`accum` is the worst case with every target at weight 1 at once: the largest,")
	fmt.Println("over vertices and components, of the summed absolute error. For POSITION the")
	fmt.Println("bracketed figure is that accumulated error as a fraction of the primitive's")
	fmt.Println("AABB diagonal - a displacement error of 1e-4 means nothing until it is compared")
	fmt.Println("with the size of what is being displaced.")
	fmt.Println()
	for _, p := range all {
		for s := range slotCount {
			st := p.slot[s]
			if !st.present {
				continue
			}
			fmt.Printf("### %s %d/%d %s (range %.5g, %d targets)\n\n",
				p.file, p.meshIdx, p.primAt, slotName[s], st.maxAbs, p.targets)
			fmt.Println("| candidate | max err | mean err | accum |")
			fmt.Println("|---|---|---|---|")
			for c := range candidateCount {
				accum := fmt.Sprintf("%.4g", st.accum[c])
				if s == slotPosition && p.diag > 0 {
					accum += fmt.Sprintf(" (%.2e diag)", st.accum[c]/p.diag)
				}
				fmt.Printf("| %s | %.4g | %.4g | %s |\n",
					candName[c], st.err[c].maxAbs, st.err[c].mean(), accum)
			}
			fmt.Println()
		}
	}
}

func reportBytes(all []prim) {
	fmt.Println("## What each candidate would cost")
	fmt.Println()
	fmt.Println("Every candidate keeps one indexed load per slot per target, exactly as today:")
	fmt.Println("8 bytes is a `vec2<u32>` (aligned 8), 4 bytes a `u32` (aligned 4). The 12/24/36")
	fmt.Println("tight packing morph.wgsl rejects is a different thing and is not on this table.")
	fmt.Println()
	total := 0
	for _, p := range all {
		total += p.bytesToday()
	}
	fmt.Println("| storage | bytes per slot record | total | of today |")
	fmt.Println("|---|---|---|---|")
	fmt.Printf("| vec4<f32> (today) | 16 | %s | 100%% |\n", bytes(total))
	for c := range candidateCount {
		if c == candSnorm16Tgt || c == candSnorm8Tgt {
			continue // same width as their per-primitive twins
		}
		sum := 0
		for _, p := range all {
			sum += p.targets * p.verts * p.stride * candBytes[c]
		}
		fmt.Printf("| %s | %d | %s | %.0f%% |\n",
			candName[c], candBytes[c], bytes(sum), 100*float64(sum)/float64(total))
	}
	fmt.Println()
	fmt.Println("And what dropping the all-zero records would save on its own, before any")
	fmt.Println("narrowing - the ceiling on a sparsity scheme, not a proposal:")
	fmt.Println()
	live := 0
	for _, p := range all {
		for s := range slotCount {
			live += (p.slot[s].records - p.slot[s].zero) * 16
		}
	}
	fmt.Printf("- non-zero records at 16 bytes: %s, %.0f%% of today\n",
		bytes(live), 100*float64(live)/float64(total))
	fmt.Println()
}

func pct(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return 100 * float64(n) / float64(of)
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
