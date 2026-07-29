import struct
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW_PATH = ROOT / ".github" / "workflows" / "release.yml"
PACKAGE_SCRIPT_PATH = ROOT / "scripts" / "package-windows.sh"
TRAY_ICON_PATH = ROOT / "build" / "trayicon.png"


class ReleasePipelineTests(unittest.TestCase):
    def test_workflow_uses_pinned_wails_v3_and_current_product_names(self):
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")

        self.assertIn("github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.96", workflow)
        self.assertIn("wails3 task build", workflow)
        self.assertIn("build/bin/AI Relay Forwarder.exe", workflow)
        self.assertIn("frontend/src/release-notes.json", workflow)
        self.assertIn("$catalog.releases", workflow)
        self.assertIn('throw "Release notes not found for $tag"', workflow)
        self.assertIn("AI-Relay-Forwarder-windows-amd64.zip", workflow)
        self.assertNotIn("github.com/wailsapp/wails/v2", workflow)
        self.assertNotIn("Claude Request Forwarder", workflow)

    def test_package_script_quotes_the_executable_and_uses_standard_library_zip(self):
        package_script = PACKAGE_SCRIPT_PATH.read_text(encoding="utf-8")

        self.assertIn('EXE_PATH="build/bin/${APP_NAME}.exe"', package_script)
        self.assertIn('cp "$EXE_PATH"', package_script)
        self.assertIn("from zipfile import ZIP_DEFLATED, ZipFile", package_script)
        self.assertNotIn("Claude Request Forwarder", package_script)

    def test_tray_icon_is_a_48_pixel_rgba_png(self):
        data = TRAY_ICON_PATH.read_bytes()

        self.assertEqual(data[:8], b"\x89PNG\r\n\x1a\n")
        width, height, bit_depth, color_type = struct.unpack(">IIBB", data[16:26])
        self.assertEqual((width, height), (48, 48))
        self.assertEqual(bit_depth, 8)
        self.assertEqual(color_type, 6)


if __name__ == "__main__":
    unittest.main()
