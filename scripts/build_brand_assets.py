"""Build desktop AppIcon + in-app animated brand mark from source frames."""

from __future__ import annotations

import struct
from io import BytesIO
from pathlib import Path

from PIL import Image

ASSETS = Path(r"C:\Users\papat\.cursor\projects\e-Soft-Git-nc-ai\assets")
SRC = {
    "appicon": ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_AppIcon-6a5df677-81dd-4336-a437-7a9a4982383d.png",
    1: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_1-3971cbe1-d95a-45c3-9441-d04342638991.png",
    2: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_2-95886c82-28b8-41f6-a0a9-4bdd082ec581.png",
    3: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_3-9780ea86-1add-41d2-b016-e59d8eb88e69.png",
    4: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_4-e187759a-abfe-4ce5-a1cf-66773e1d0be3.png",
    5: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_5-97fd1ac0-07f8-499d-8ee3-2e2e150fdcdc.png",
    6: ASSETS
    / "c__Users_papat_AppData_Roaming_Cursor_User_workspaceStorage_empty-window_images_6-866213d8-6a27-4870-81db-ddabca6d992b.png",
}

ROOT = Path(__file__).resolve().parents[1]
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
