# The halo prototype, looked at

Throwaway, for [dvoyni/cog#191](https://github.com/dvoyni/cog/issues/191).
Screenshots in [`tmp/shots/`](../../../tmp/shots), captured by `tmp/shot.ps1`.

The question the ticket asks: **does the expanded quad work, and does it read
right** — a soft outward fade rather than a crisp outline, the same visual
language as the baked halo beside it, indistinguishable across the glyph, the
sprite and the fill, at more than one scale and over a busy background.

## It works, and no cog contract moved

One `SetLayerMaterial` over one layer haloes a `Text` number, a `Sprite` icon, a
`FillRect` flag, a `StrokeRect` and a `Line` together — because every one of them
is a sprite instance in the same instanced atlas draw. The material replaces both
entry points, includes `spritebindings.wgsl` alone, declares its own inter-stage
struct beside the published `VertexOut`, and expands the unit quad in `vs_main`.
Nothing in cog was touched.

**The two branches are invisible at the seam.** `08-flat-dark.png` is the
clearest look at it: the analytic distance-to-rect on the flag, the box and the
rule reads as exactly the same band as the texture-space kernel on the digits,
the anchor and the unit. Nothing about the picture says two mechanisms produced
it. That was the acceptance criterion most at risk and it is met.

**Frame rejection holds at every reach tried,** including reaches wide enough to
walk a long way past a sprite's 2px pad. No neighbour art, no extruded smear.

## What the pictures say that the plan did not

**The reach has to scale with the mark.** This is the one finding that changes
the design rather than confirming it. A reach in world units is a constant, but a
baked halo is in the texture and therefore scales with the art, so the two can
agree at exactly one size. At 1.0x, reach 6 matches the painted band
(`crop-units-1x.png`). At 1.9x the same 6 reads as a tight rim, not a fade
(`crop-digits-19x-reach6.png`); 12 reads right (`crop-digits-19x-reach12.png`) —
1.9x the reach for 1.9x the mark, which is the ratio exactly.

That is a parameter-frequency question, not a shader one. A per-batch reach
cannot vary per sprite, so either the caller scales it themselves per scope, or
the reach is a per-sprite parameter, or it is expressed as a fraction of the
sprite rather than in world units. The map had this as fog ("whether the halo's
softness is resolution-aware"); it is now sharp and it is bigger than DPI.

**One mark's halo paints over its neighbour's ink, visibly.** The corners of the
`StrokeRect` are where it shows: the vertical bar's band cuts a notch into the
horizontal bar's ink, because each fragment composites its own halo under its own
mark and a bar drawn later brings its band over the one drawn before. Adjacent
digits do not show it — their bands merge cleanly with no seam — but they are
far enough apart to be lucky. This is [#192](https://github.com/dvoyni/cog/issues/192)'s
question, and the prototype now has a reproducible case of it rather than a
hypothesis.

**Ring banding at low tap counts, but not where it was expected.** 4 rings and 12
onto 12 rings is nearly indistinguishable at a small reach
(`z-big18-r4` vs `z-big18-r12` in the earlier set); the banding shows at a *wide*
reach, where the rings are far apart in screen pixels. Taps therefore want to
scale with the reach in pixels, not be a constant.

## The numbers, measured rather than guessed

The starting knobs come from the art itself. Across the outer band of
`militiaman.png`, 84% of the halo pixels are exactly **#ae9f8d** — flat colour,
soft alpha — and alpha holds near 0.82 for the first fifth of the band, then
falls away almost exactly linearly, out to about six world units at a 96-unit
icon. Hence: reach 6, plateau 0.18, exponent 1.0, alpha 0.85. A plain
`pow(1-t, k)` from the ink outward does not match painted art; the plateau is
what makes it.

## One thing that is not about the halo

**naga's SPIR-V backend cannot lower `ir.ExprRelational`** — `any()` and `all()`
over a vector of bools. The shader compiles as WGSL and fails at pipeline
creation with `unsupported expression kind: ir.ExprRelational`. Every comparison
in `shader.go` is spelled out component-wise because of it. Same family as the
module-scope vector bug this repo already overrides naga for (dvoyni/cog#181,
gogpu/naga#92).
