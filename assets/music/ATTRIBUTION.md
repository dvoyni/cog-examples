# Music attribution

`pleasant-moments.ogg` is
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, **public domain**, vendored byte for byte with nothing
re-encoded: 2 854 297 bytes, stereo 44.1 kHz, 7 769 600 frames, 176.181 seconds.

## Why it is here rather than in the generated set

`assets/ATTRIBUTION.md` beside this directory is **generated** by
`cmd/prepare-assets` from a manifest that describes glTF models fetched from
`KhronosGroup/glTF-Sample-Assets` — it pins each asset to an upstream commit and
verifies the recorded licence against upstream's `metadata.json` on every run.
None of that applies to a public-domain audio file from a different project, and
`TestAttributionCoversTheVendoredSet` asserts that the generated file matches the
manifest exactly, so an audio entry appended by hand would read as stale on the
next run.

So the music sits in its own directory with its own credits. It is still carried
into the browser bundle, because `cmd/web/build.sh` tars the whole `assets/`
directory rather than the manifest's outputs.

## Why this file, at this length

It is the clip whose decode cost the audio research measured, and its size is
the argument the streamed tier exists for:

| | |
|---|---|
| encoded, as vendored | 2 854 297 bytes (2.7 MiB) |
| decoded to float32 stereo | 62 156 800 bytes (59.3 MiB) |
| ratio | **21.8×** |

`otosound`'s and `jssound`'s `DecodedClipLimit` defaults to 512 KiB, which is
1.49 s of stereo 44.1 kHz, so this track is three orders of magnitude over the
line and **streams** — a decoder and a read-ahead ring per Voice, decoding a few
thousand frames at a time. Holding it resident instead would cost 59 MiB for one
piece of music, which is the whole reason neither residency tier is defensible
alone.

The 1.1 second cut of this same recording is vendored separately inside the
engine, at `extensions/nosound/internal/testdata/pianoroll.ogg` and beside the
`cmd/sound/orbit` demo, for tests and demos that want a short resident Clip. The
first 13 072 bytes of the two files are byte-identical; they diverge where the
cut's final page was marked end-of-stream with a recomputed CRC.

The public-domain recording was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, four of whose
five clips are CC BY-SA, so that nothing here carries a share-alike obligation.
