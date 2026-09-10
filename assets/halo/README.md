# halo/ — prototype art, this branch only

Not part of the vendored Khronos asset set, not covered by
`assets/ATTRIBUTION.md`, and not managed by `cmd/prepare-assets`. It is here so
`cmd/canvas/halo` can be judged against real art rather than a stand-in, which
is the whole of what a VFX prototype is for.

Everything here is copied unchanged from [dvoyni/feuds-26](https://github.com/dvoyni/feuds-26)
`res/`, except the two `-ink` files:

| file | from | why |
|---|---|---|
| `Gabriela-Regular.ttf`, `OFL.txt` | `res/fonts/` | the face feuds sets board text in, so the glyph case is the real glyph case |
| `paper-bg.jpg` | `res/images/` | the ground every mark is read against |
| `trees_1.png`, `trees_11.png` | `res/images/trees/` | the busiest thing a mark ever overlaps |
| `anchor.png` | `res/images/` | the sprite feuds' own score cluster draws |
| `militiaman.png`, `infantryman.png` | `res/images/units/` | the **reference**: a hand-painted halo, the thing the effect has to match |
| `militiaman-ink.png`, `infantryman-ink.png` | derived, `tmp/strip-halo.py` | the same silhouette with the painted halo unmixed away, so the effect is handed what the painter started from |

The `-ink` pair is the **input** to the effect, never the thing being judged.
The originals sit beside them untouched, and the comparison is between the two.

Delete this directory with the branch.
