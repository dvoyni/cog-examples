# Demo clip attribution

`pianoroll-loop.ogg` is the first 229 568 sample frames (5.2056 seconds,
stereo 44.1 kHz) of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. It is Scott Joplin's
*Pleasant Moments Ragtime Waltz*, played by the composer on Connorized piano
roll #10319 (1916). The recording's own Vorbis comments are kept.

It is the same recording as the 1.1 second `pianoroll.ogg` the other sound
demos embed, cut longer. The loop wants an intro of about a second and a loop
of a few, and 1.1 seconds holds neither.

## How it was made

Nothing was re-encoded. It was made once, with this module's own tool:

```
go run ./cmd/looptag -in assets/music/pleasant-moments.ogg \
    -out cmd/sound/loopregion/pianoroll-loop.ogg \
    -loopstart 43960 -looplength 178383 -cut 222343
```

That does two things, in this order:

1. **Cut.** It keeps the recording's pages up to the first one whose granule
   position reaches frame 222 343 (the loop end) and that ends on a packet
   boundary: page 19, at frame 229 568. That page is marked end-of-stream and
   its CRC recomputed. No other byte changes: the first 19 pages are the
   published file's, byte for byte.
2. **Tag.** It rebuilds the Vorbis comment header with the recording's seven
   comments unchanged and in order, then `LOOPSTART=43960` and
   `LOOPLENGTH=178383`. Only that one page is re-laid, with its CRC recomputed.
   The tool then decodes both the cut and the tagged file to the last sample,
   and writes the file only if they come to the same number of frames and 19
   of the 20 pages are byte-identical.

What it printed:

```
looptag: wrote cmd/sound/loopregion/pianoroll-loop.ogg, 77259 bytes
  frames      229568 decoded from the tagged file, 229568 from its source (5.2056 s, 44100 Hz, 2 channels)
  pages       19 of 20 byte-identical to the source; the other is the comment header's
  region      [43960, 222343) = [0.9968 s, 5.0418 s): an intro of 0.9968 s, a loop of 4.0450 s
```

`clip_test.go` rebuilds the clip from the recording with the same parameters
and requires the same bytes. It skips when `assets/music/pleasant-moments.ogg`
is not in the checkout.

## Why the loop sits where it does

The loop start and end were found by measuring the recording, not by ear. The
search covered the first ten seconds: a loop starting between 0.9 s and 1.4 s,
between 2.4 s and 4.2 s long. For each candidate pair, the log-magnitude spectrum
around the loop end (0.3 s before to 0.6 s after) was compared with the spectrum
around the loop start. One loop length won clearly. The best 25 pairs all lay
within a few milliseconds of 0.998 s to 5.042 s, and the best distinct candidates
all had loops between 4.04 s and 4.06 s. The winner's distance was 29% below the
median candidate's. Then the exact samples were chosen, within 5 ms of that
winner: the pair where the 8 frames either side of the loop end best matched the
8 frames either side of the loop start, in both channels. That way the waveform
meets itself at the seam instead of jumping.

A piano roll is a mechanical performance and not a loop, so no seam in it is
perfect. This one was chosen by measurement, and whether it passes by ear is
for a listener to say. If it does not, re-run the tool with a better pair and
update the demo's constants; `clip_test.go` fails until the two agree.

## Licence

Public domain, like the recording. It was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, four of whose
five clips are CC BY-SA, so that nothing here carries a share-alike obligation.
