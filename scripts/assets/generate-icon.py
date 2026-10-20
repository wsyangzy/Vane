#!/usr/bin/env python3
"""Build all Vane icons from the original transparent brand artwork."""
from pathlib import Path
from PIL import Image

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "assets/vane.png"


def render(artwork, size, coverage=0.84):
    image = artwork.copy()
    image.thumbnail((round(size * coverage), round(size * coverage)), Image.Resampling.LANCZOS)
    canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    canvas.alpha_composite(image, ((size - image.width) // 2, (size - image.height) // 2))
    return canvas


def main():
    with Image.open(SOURCE) as source:
        artwork = source.convert("RGBA")
    # The supplied raster contains almost invisible stray alpha outside the mark.
    alpha = artwork.getchannel("A").point(lambda value: 0 if value <= 8 else value)
    artwork.putalpha(alpha)
    artwork = artwork.crop(alpha.getbbox())
    master = render(artwork, 1024)
    outputs = {
        "assets/vane-app.png": master,
        "assets/vane-tray.png": render(artwork, 48, 0.94),
        "build/linux/appicon.png": master,
        "frontend/public/vane.png": render(artwork, 256),
    }
    for relative, image in outputs.items():
        target = ROOT / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        image.save(target)
        print(f"Generated {relative}")
    sizes = [(size, size) for size in (16, 24, 32, 48, 64, 128, 256)]
    for relative in ("build/windows/icon.ico", "frontend/public/favicon.ico"):
        master.save(ROOT / relative, format="ICO", sizes=sizes)
        print(f"Generated {relative}")
    master.save(ROOT / "build/darwin/appicon.icns", format="ICNS")
    print("Generated build/darwin/appicon.icns")


if __name__ == "__main__":
    main()
