# Morph delta measurement

4 morphed primitives across 3 assets.

## What the store holds

`zero` is records whose three components are all exactly zero: a target that
does not move that vertex. scene stores them anyway, one full record each.

| asset | mesh/prim | verts | targets | slots | bytes today | zero records | of total | in file | file stores |
|---|---|---|---|---|---|---|---|---|---|
| AnimatedMorphCube | 0/0 | 24 | 2 | POS+NOR+TAN | 2.2 KiB | 122 | 84.7% | 1.7 KiB | f32 |
| AnimatedMorphCube-Quantized | 0/0 | 24 | 2 | POS+NOR | 1.5 KiB | 74 | 77.1% | 432 B | i16, i8n |
| MorphStressTest | 0/0 | 24 | 8 | POS+NOR | 6.0 KiB | 384 | 100.0% | 4.5 KiB | f32 |
| MorphStressTest | 0/1 | 1504 | 8 | POS+NOR | 376.0 KiB | 22392 | 93.1% | 282.0 KiB | f32 |
| **all** | | | | | **385.8 KiB** | **22972** | **93.0%** | **288.6 KiB** | |

## Where the live records sit

A record is live for a target if any of that target's slots moves the vertex:
the record is what the shader addresses, so it is the unit a sparse scheme
would keep or drop. `span` is the index distance from the first live record to
the last, `runs` the number of maximal consecutive stretches. `disagree` counts
live records where one slot moves and another does not - what record
granularity wastes against per-slot granularity.

| asset | mesh/prim | verts | targets | record B | live/target | span/target | runs/target | disagree |
|---|---|---|---|---|---|---|---|---|
| AnimatedMorphCube | 0/0 | 24 | 2 | 16 | 8..12 (mean 10) | 15..18 (mean 16) | 4..5 (mean 4) | 20 |
| AnimatedMorphCube-Quantized | 0/0 | 24 | 2 | 12 | 8..12 (mean 10) | 16..20 (mean 18) | 4..5 (mean 4) | 18 |
| MorphStressTest | 0/0 | 24 | 8 | 12 | 0 | 0 | 0 | 0 |
| MorphStressTest | 0/1 | 1504 | 8 | 12 | 115 | 187 | 16 | 168 |

| scheme | total | of today |
|---|---|---|
| vec4<f32>, dense (today) | 385.8 KiB | 100% |
| dense, 8/4/4 per slot (narrowing alone) | 144.6 KiB | 37.5% |
| live span per target, at today's 16 B slots (sparsity alone) | 49.6 KiB | 12.9% |
| live span per target, 8/4/4 slots (both) | 18.6 KiB | 4.8% |
| runs of live records, 8 B per run | 12.5 KiB | 3.2% |
| live records + a 4 B vertex index each | 15.1 KiB | 3.9% |

## The magnitudes, per slot

`max |c|` is the largest absolute component, the symmetric range a per-primitive
fixed-point encoding would carry. `mean |c|` is over non-zero records only.
`diag` is the primitive's position AABB diagonal; `max/diag` says how far a
position delta reaches relative to the thing it is deforming.

| asset | mesh/prim | slot | max \|c\| | mean \|c\| | max len | diag | max/diag | target ranges lo..hi |
|---|---|---|---|---|---|---|---|---|
| AnimatedMorphCube | 0/0 | POSITION | 0.019891 | 0.0064173 | 0.019891 | 0.034641 | 0.574 | 0.01893 .. 0.01989 |
| AnimatedMorphCube | 0/0 | NORMAL | 0.70517 | 0.33204 | 0.76284 | 0.034641 | - | 0 .. 0.7052 |
| AnimatedMorphCube | 0/0 | TANGENT | 0 | 0 | 0 | 0.034641 | - | 0 .. 0 |
| AnimatedMorphCube-Quantized | 0/0 | POSITION | 5451 | 1758.6 | 5451 | 9493.4 | 0.574 | 5188 .. 5451 |
| AnimatedMorphCube-Quantized | 0/0 | NORMAL | 0.00558 | 0.0026247 | 0.0060332 | 9493.4 | - | 0 .. 0.00558 |
| MorphStressTest | 0/0 | POSITION | 0 | 0 | 0 | 4.1243 | 0.000 | 0 .. 0 |
| MorphStressTest | 0/0 | NORMAL | 0 | 0 | 0 | 4.1243 | - | 0 .. 0 |
| MorphStressTest | 0/1 | POSITION | 1 | 0.35 | 1.0012 | 3.8161 | 0.262 | 1 .. 1 |
| MorphStressTest | 0/1 | NORMAL | 0.017679 | 0.0015903 | 0.017786 | 3.8161 | - | 0.01768 .. 0.01768 |

## Round-trip error, per candidate

`max` and `mean` are per component over every record of every target.
`accum` is the worst case with every target at weight 1 at once: the largest,
over vertices and components, of the summed absolute error. For POSITION the
bracketed figure is that accumulated error as a fraction of the primitive's
AABB diagonal - a displacement error of 1e-4 means nothing until it is compared
with the size of what is being displaced.

### AnimatedMorphCube 0/0 POSITION (range 0.019891, 2 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 6.625e-06 | 5.783e-07 | 1.025e-05 (2.96e-04 diag) |
| snorm16x3 prim range (8B) | 2.092e-07 | 1.739e-08 | 2.092e-07 (6.04e-06 diag) |
| snorm16x3 target range (8B) | 1.863e-09 | 3.881e-11 | 1.863e-09 (5.38e-08 diag) |
| snorm8x3 prim range (4B) | 1.858e-05 | 1.548e-06 | 1.858e-05 (5.36e-04 diag) |
| snorm8x3 target range (4B) | 1.863e-09 | 3.881e-11 | 1.863e-09 (5.38e-08 diag) |

### AnimatedMorphCube 0/0 NORMAL (range 0.70517, 2 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 9.096e-05 | 4.05e-06 | 9.096e-05 |
| snorm16x3 prim range (8B) | 7.99e-07 | 2.219e-08 | 7.99e-07 |
| snorm16x3 target range (8B) | 7.99e-07 | 2.219e-08 | 7.99e-07 |
| snorm8x3 prim range (4B) | 0.00223 | 6.195e-05 | 0.00223 |
| snorm8x3 target range (4B) | 0.00223 | 6.195e-05 | 0.00223 |

### AnimatedMorphCube 0/0 TANGENT (range 0, 2 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 0 | 0 | 0 |
| snorm16x3 prim range (8B) | 0 | 0 | 0 |
| snorm16x3 target range (8B) | 0 | 0 | 0 |
| snorm8x3 prim range (4B) | 0 | 0 | 0 |
| snorm8x3 target range (4B) | 0 | 0 | 0 |

### AnimatedMorphCube-Quantized 0/0 POSITION (range 5451, 2 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 1 | 0.04167 | 1 (1.05e-04 diag) |
| snorm16x3 prim range (8B) | 0.009461 | 0.0007884 | 0.009461 (9.97e-07 diag) |
| snorm16x3 target range (8B) | 0 | 0 | 0 (0.00e+00 diag) |
| snorm8x3 prim range (4B) | 5.472 | 0.456 | 5.472 (5.76e-04 diag) |
| snorm8x3 target range (4B) | 0 | 0 | 0 (0.00e+00 diag) |

### AnimatedMorphCube-Quantized 0/0 NORMAL (range 0.00558, 2 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 8.909e-07 | 3.963e-08 | 8.909e-07 |
| snorm16x3 prim range (8B) | 2.081e-08 | 5.782e-10 | 2.081e-08 |
| snorm16x3 target range (8B) | 2.081e-08 | 5.782e-10 | 2.081e-08 |
| snorm8x3 prim range (4B) | 9.276e-06 | 2.577e-07 | 9.276e-06 |
| snorm8x3 target range (4B) | 9.276e-06 | 2.577e-07 | 9.276e-06 |

### MorphStressTest 0/0 POSITION (range 0, 8 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 0 | 0 | 0 (0.00e+00 diag) |
| snorm16x3 prim range (8B) | 0 | 0 | 0 (0.00e+00 diag) |
| snorm16x3 target range (8B) | 0 | 0 | 0 (0.00e+00 diag) |
| snorm8x3 prim range (4B) | 0 | 0 | 0 (0.00e+00 diag) |
| snorm8x3 target range (4B) | 0 | 0 | 0 (0.00e+00 diag) |

### MorphStressTest 0/0 NORMAL (range 0, 8 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 0 | 0 | 0 |
| snorm16x3 prim range (8B) | 0 | 0 | 0 |
| snorm16x3 target range (8B) | 0 | 0 | 0 |
| snorm8x3 prim range (4B) | 0 | 0 | 0 |
| snorm8x3 target range (4B) | 0 | 0 | 0 |

### MorphStressTest 0/1 POSITION (range 1, 8 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 1.222e-05 | 2.54e-07 | 1.222e-05 (3.20e-06 diag) |
| snorm16x3 prim range (8B) | 1.069e-05 | 2.222e-07 | 1.069e-05 (2.80e-06 diag) |
| snorm16x3 target range (8B) | 1.069e-05 | 2.222e-07 | 1.069e-05 (2.80e-06 diag) |
| snorm8x3 prim range (4B) | 0.002756 | 5.741e-05 | 0.002756 (7.22e-04 diag) |
| snorm8x3 target range (4B) | 0.002756 | 5.741e-05 | 0.002756 (7.22e-04 diag) |

### MorphStressTest 0/1 NORMAL (range 0.017679, 8 targets)

| candidate | max err | mean err | accum |
|---|---|---|---|
| f16x3 (8B) | 6.147e-06 | 2.936e-08 | 6.147e-06 |
| snorm16x3 prim range (8B) | 2.697e-07 | 1.027e-08 | 2.697e-07 |
| snorm16x3 target range (8B) | 2.695e-07 | 1.036e-08 | 2.695e-07 |
| snorm8x3 prim range (4B) | 6.536e-05 | 1.04e-06 | 6.536e-05 |
| snorm8x3 target range (4B) | 6.536e-05 | 1.04e-06 | 6.536e-05 |

## What each candidate would cost

Every candidate keeps one indexed load per slot per target, exactly as today:
8 bytes is a `vec2<u32>` (aligned 8), 4 bytes a `u32` (aligned 4). The 12/24/36
tight packing morph.wgsl rejects is a different thing and is not on this table.

| storage | bytes per slot record | total | of today |
|---|---|---|---|
| vec4<f32> (today) | 16 | 385.8 KiB | 100% |
| f16x3 (8B) | 8 | 192.9 KiB | 50% |
| snorm16x3 prim range (8B) | 8 | 192.9 KiB | 50% |
| snorm8x3 prim range (4B) | 4 | 96.4 KiB | 25% |

And what dropping the all-zero records would save on its own, before any
narrowing - the ceiling on a sparsity scheme, not a proposal:

- non-zero records at 16 bytes: 26.8 KiB, 7% of today

