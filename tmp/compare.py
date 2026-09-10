"""THROWAWAY. The pixel readback gfx does not have, done on screenshots.

Issue 179 asks whether oct16 holds up under a reflected environment, a normal
map, and motion. The first two are stills, and a seam is a poor instrument for
them: whether a seam shows depends on whether the environment happens to put
structure across the cut. VN_SOLO fills the screen with one candidate instead,
so two captures at one pose differ only in the encoding and can be differenced
directly.

Run the captures listed in the issue's resolution comment, then:

    python tmp/compare.py

The null pair is the point. Two captures of the SAME encoding must come back
pixel-identical; if they do not, the renderer is not deterministic at this pose
and no other number here means anything.
"""

import sys
from PIL import Image

# The disc, for the VN_AZ=3.927 / VN_EL default pose at 1280x720 logical.
# Taken from a capture whose ball covers the backdrop everywhere, so the extent
# is the sphere rather than the lit part of it.
CX, CY, R = 962.0, 600.0, 365.0
MARGIN = 8


def disc_pixels(a, b):
    ia, ib = Image.open(a).convert("RGB"), Image.open(b).convert("RGB")
    if ia.size != ib.size:
        sys.exit(f"{a} is {ia.size} and {b} is {ib.size}")
    pa, pb = ia.load(), ib.load()
    for y in range(int(CY - R), int(CY + R)):
        dy2 = (y - CY) ** 2
        for x in range(int(CX - R), int(CX + R)):
            if (x - CX) ** 2 + dy2 < (R - MARGIN) ** 2:
                yield x, y, pa[x, y], pb[x, y]


def compare(a, b, label):
    diffs = [max(abs(ca[i] - cb[i]) for i in range(3)) for _, _, ca, cb in disc_pixels(a, b)]
    diffs.sort()
    n = len(diffs)
    over = lambda t: 100 * sum(1 for d in diffs if d > t) / n
    print(f"{label:46s} >2: {over(2):5.2f}%  >32: {over(32):5.2f}%  "
          f">128: {over(128):5.2f}%  mean {sum(diffs) / n:6.2f}  max {diffs[-1]:3d}")


def diffmap(a, b, out, gain=3):
    """Where the difference lands, which is the whole question.

    A coherent, edge-shaped map is a displacement: the reflection moved. An
    incoherent speckle is dither. The two read completely differently and the
    scalar percentages above cannot tell them apart.
    """
    dst = Image.new("RGB", (int(2 * R), int(2 * R)), (0, 0, 0))
    pd = dst.load()
    for x, y, ca, cb in disc_pixels(a, b):
        v = min(255, max(abs(ca[i] - cb[i]) for i in range(3)) * gain)
        pd[x - int(CX - R), y - int(CY - R)] = (v, v // 2, 0) if v else (10, 10, 14)
    dst.save(out)
    print("wrote", out)


if __name__ == "__main__":
    compare("tmp/14.png", "tmp/16.png", "NULL  oct32 against itself")
    compare("tmp/13.png", "tmp/14.png", "exact -> oct32 normal        (64B -> 40B)")
    compare("tmp/14.png", "tmp/15.png", "oct32 -> oct16 NORMAL        (40B -> 38B)")
    compare("tmp/19.png", "tmp/20.png", "oct30 -> oct16 TANGENT       (40B -> 38B)")
    compare("tmp/17.png", "tmp/18.png", "  the same, over an oct16 normal (38B -> 36B)")
    diffmap("tmp/14.png", "tmp/15.png", "tmp/diff-normal-oct32-vs-oct16.png")
    diffmap("tmp/19.png", "tmp/20.png", "tmp/diff-tangent-clean-normal.png")
    diffmap("tmp/17.png", "tmp/18.png", "tmp/diff-tangent-oct30-vs-oct16.png")
