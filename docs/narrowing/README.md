# Confirming the narrowed engine by eye

The captures and the method behind
[cog#223](https://github.com/dvoyni/cog/issues/223), which confirmed the mesh
narrowing of [cog#213](https://github.com/dvoyni/cog/issues/213) — `uint16`
indices, an octahedral normal and tangent, `unorm16` UVs against a per-mesh
range, `unorm8` weights, two named vertex layouts, and sparse morph deltas.

There is **no pixel readback anywhere in cog**, so nothing in the engine's own
test suite can assert about a frame. What made this checkable is that the engine
speaks MCP: `wgpu_time` stops the update loop on an exact tick, `gfx_capture`
writes the frame to a PNG, and `gfx_frame` dumps what the renderer was told.
An agent can therefore do what a person would do by eye, and difference it.

## The method

**Whole-frame captures at a fixed pose, differenced channel by channel, with a
null pair as the control.** The control is what makes every other number mean
something: two captures of the *same* build at the *same* tick must come back
pixel-identical, and here they always did — four demos, both builds, 0 differing
pixels out of 1.78–2.07 million each time.

The pose is fixed by tick rather than by wall clock. `capture.py` pauses the
demo as soon as its server answers and then single-steps it onto an exact
absolute tick, so the "before" and "after" builds are compared at tick 600 of
their own deterministic fixed-step clocks — the same pose by construction, not
by waiting the same amount.

The "before" build is the engine at `437f1b1`, the commit before the narrowing
began, built from a detached worktree against a matching worktree of this repo.
It is *not* the committed `reference.png` beside each demo: those were taken in
early September and also predate changes that have nothing to do with the
narrowing. Differencing against them attributes other people's work to this one
— which is exactly what happened on the first attempt here, and is why the
comparison is against a purpose-built baseline instead.

## Running it

Add `mcpserver.New()` to a demo's plugin list, then:

    go build -o demo.exe ./cmd/scene/loading
    python docs/narrowing/capture.py demo.exe . 600 /tmp/out
    go run ./cmd/framediff /tmp/before_a.png /tmp/out_a.png /tmp/diff.png

`framediff` prints a histogram of the largest per-channel difference per pixel
and writes the difference amplified ten times. A difference of 1 is a rounding
code; the histogram is there so that a frame off by one everywhere reads
differently from one off by eighty in a corner.

## What it found

**One real difference across the whole vendored set, and it is in the spec now.**

`MeshPrimitiveModes` ships seven primitives that carry `POSITION` and nothing
else. An unwritten normal used to be `(0, 0, 0)`, which killed every dot product
and drew those primitives **black**; it now decodes to **+Z** and they are lit
like any other surface, drawing **light grey**. That is the specified behaviour
of the octahedral canonical, but the spec framed +Z as a "no NaN" safety
property and never said it would change how normal-less geometry shades.
`primitivemodes-before-after.png` is the pair, and it is recorded in
`bundles/scene/docs/specs/mesh.md`.

Everything else is quantisation noise or the demos' own counters:

| demo | differing | of which off by exactly 1 | above that |
| --- | ---: | ---: | --- |
| loading | 42,306 (2.37%) | 41,000 | 363 px of the change above, 943 px of HUD |
| pbr | 62,435 (3.01%) | ~61,500 | 912 px: HUD, and sub-texel shifts on glyph edges |
| cameras | 7,595 (0.37%) | 6,473 | 1,122 px, **all** HUD text |
| animated | 32,004 (1.54%) | 28,012 | 3,992 px: HUD counters and `fps` |

The `pbr` mean over differing pixels is **1.8 / 255** — the mirrored sphere, the
normal-mapped panels and the tiled alpha-test wall are all sub-perceptual. The
`cameras` obelisk, whose shader had to change to include the oct decode, differs
**nowhere outside its HUD**.

`animated-hud-before-after.png` is the size win stated by the running engine
rather than by arithmetic: the same frame, same `step 001566`, same
`time 26.10s`, same blend weights, and morph deltas reading
**`cube 2 / quantized 2 / stress 382 KiB  total 386 KiB`** before against
**`cube 1 / quantized 0 / stress 18 KiB  total 19 KiB`** after.

## Files

- `loading-before.png`, `loading-after.png`, `loading-diff.png` — the full frame
  either side, and their difference amplified ten times.
- `primitivemodes-before-after.png` — the one finding, magnified three times.
- `animated-hud-before-after.png` — the morph totals off the live HUD.
- `capture.py`, `mcpc.py` — the driver and a small MCP client over the demo's
  `127.0.0.1:7654` endpoint.
