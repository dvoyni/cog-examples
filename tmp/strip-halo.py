"""Unmix the baked halo out of feuds' unit art, leaving only its ink.

The halo prototype needs the silhouette the painter started from, so the
generated halo is judged against the painted one over the SAME shape. The
painted originals are untouched and stay beside it as the reference.

84% of the halo pixels in militiaman.png are exactly #ae9f8d, and the ink is
around #3c2824, so a pixel's inkness is where it sits between the two.

    python tmp/strip-halo.py
"""

from PIL import Image
import numpy as np

HALO, INK = 174.0, 60.0

for name in ("militiaman", "infantryman"):
    im = Image.open(f"assets/halo/{name}.png").convert("RGBA")
    a = np.array(im).astype(float)
    rgb, al = a[..., :3], a[..., 3] / 255
    lum = rgb.mean(axis=2)
    ink = np.clip((HALO - lum) / (HALO - INK), 0, 1) * al
    out = np.zeros_like(a)
    out[..., 0], out[..., 1], out[..., 2] = 50, 40, 36
    out[..., 3] = ink * 255
    Image.fromarray(out.astype(np.uint8)).save(f"assets/halo/{name}-ink.png")
    print(name, "ink coverage", (ink > 0.5).sum())
