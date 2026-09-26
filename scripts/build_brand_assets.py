"""Build desktop AppIcon + in-app animated brand mark from source frames.

Исходные кадры лежат внутри проекта: `assets/brand-src/` (в .gitignore, чтобы
локальные картинки не уезжали в публичное зеркало). Другую папку можно указать
через переменную окружения NC_BRAND_SRC. Никаких абсолютных путей и личных
каталогов в скрипте нет — только пути от корня репозитория.
"""

from __future__ import annotations

import os
import struct
from io import BytesIO
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
SRC_DIR = Path(os.environ.get("NC_BRAND_SRC") or ROOT / "assets" / "brand-src")
SRC = {
    "appicon": SRC_DIR / "appicon.png",
    1: SRC_DIR / "frame-1.png",
    2: SRC_DIR / "frame-2.png",
    3: SRC_DIR / "frame-3.png",
    4: SRC_DIR / "frame-4.png",
    5: SRC_DIR / "frame-5.png",
    6: SRC_DIR / "frame-6.png",
}

BRAND = ROOT / "frontend" / "src" / "assets" / "brand"
DOCK = ROOT / "internal" / "dockicon" / "frames"
BUILD = ROOT / "build"
WIN = BUILD / "windows"

# Explorer list/details views use 16/32; shell overlays use 48; desktop uses 256.
ICO_SIZES = (16, 24, 32, 48, 64, 128, 256)


def center_square(im: Image.Image) -> Image.Image:
    w, h = im.size
    side = min(w, h)
    left = (w - side) // 2
    top = (h - side) // 2
    return im.crop((left, top, left + side, top + side))


def save_png(im: Image.Image, path: Path, size: int | None = None) -> None:
    out = im.convert("RGBA")
    if size:
        out = out.resize((size, size), Image.Resampling.LANCZOS)
    path.parent.mkdir(parents=True, exist_ok=True)
    out.save(path, format="PNG", optimize=True)


def save_multi_size_ico(im: Image.Image, path: Path, sizes: tuple[int, ...] = ICO_SIZES) -> None:
    """Write a real multi-resolution ICO (Pillow's sizes= often keeps only one bitmap)."""
    path.parent.mkdir(parents=True, exist_ok=True)
    png_blobs: list[tuple[int, bytes]] = []
    base = im.convert("RGBA")
    for s in sizes:
        buf = BytesIO()
        base.resize((s, s), Image.Resampling.LANCZOS).save(buf, format="PNG", optimize=True)
        png_blobs.append((s, buf.getvalue()))

    # ICONDIR + ICONDIRENTRY*n + PNG payloads (Vista+ PNG-in-ICO).
    header = struct.pack("<HHH", 0, 1, len(png_blobs))
    entries = bytearray()
    offset = 6 + 16 * len(png_blobs)
    payloads = bytearray()
    for s, blob in png_blobs:
        w = 0 if s >= 256 else s
        h = 0 if s >= 256 else s
        entries += struct.pack("<BBBBHHII", w, h, 0, 0, 1, 32, len(blob), offset)
        payloads += blob
        offset += len(blob)

    path.write_bytes(header + entries + payloads)


def main() -> None:
    missing = [p for p in SRC.values() if not p.is_file()]
    if missing:
        raise SystemExit(
            "no source frames: "
            + ", ".join(str(p) for p in missing)
            + "\nput appicon.png and frame-1..6.png into assets/brand-src/ "
            "or set NC_BRAND_SRC to another folder"
        )

    BRAND.mkdir(parents=True, exist_ok=True)

    app = center_square(Image.open(SRC["appicon"]))
    save_png(app, BUILD / "appicon.png", 1024)
    save_png(app, BRAND / "app-icon.png", 512)
    save_multi_size_ico(app, WIN / "icon.ico")

    frames: list[Image.Image] = []
    for i in range(1, 7):
        sq = center_square(Image.open(SRC[i]))
        save_png(sq, BRAND / f"frame-{i}.png", 96)
        save_png(sq, ROOT / "frontend" / "public" / "brand" / f"frame-{i}.png", 96)
        save_png(sq, DOCK / f"frame{i}.png", 256)
        frames.append(sq.resize((256, 256), Image.Resampling.LANCZOS).convert("RGBA"))

    save_png(app, DOCK / "default.png", 256)

    frames[0].save(
        BRAND / "mark-anim.webp",
        format="WEBP",
        save_all=True,
        append_images=frames[1:],
        duration=140,
        loop=0,
        quality=85,
        method=6,
    )
    frames[0].save(
        BRAND / "mark-anim.gif",
        format="GIF",
        save_all=True,
        append_images=frames[1:],
        duration=140,
        loop=0,
        disposal=2,
        optimize=True,
    )

    # Verify ICO entries
    raw = (WIN / "icon.ico").read_bytes()
    _reserved, itype, count = struct.unpack_from("<HHH", raw, 0)
    print(f"ico entries={count} type={itype} size={len(raw)/1024:.1f} KB")

    print("done")
    for p in sorted(BRAND.iterdir()):
        print(f"  {p.name:20} {p.stat().st_size / 1024:7.1f} KB")
    print(f"  appicon.png          {(BUILD / 'appicon.png').stat().st_size / 1024:7.1f} KB")
    print(f"  icon.ico             {(WIN / 'icon.ico').stat().st_size / 1024:7.1f} KB")


if __name__ == "__main__":
    main()
