import json
import os
import re
import shlex
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW_PATH = ROOT / ".github" / "workflows" / "release.yml"
PACKAGE_SCRIPT_PATH = ROOT / "scripts" / "build" / "package-windows.sh"
TRAY_ICON_PATH = ROOT / "assets" / "vane-tray.png"


def bash_executable():
    if os.name == "nt":
        git = shutil.which("git")
        if git:
            bundled = Path(git).resolve().parent.parent / "bin" / "bash.exe"
            if bundled.is_file():
                return str(bundled)
        return None
    return shutil.which("bash")


BASH = bash_executable()
PWSH = shutil.which("pwsh")


class ReleasePipelineTests(unittest.TestCase):
    def run_release_preparation(self, tag="v1.0.0", source_version="1.0.0",
                                catalog_version="1.0.0", releases=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "fixture"
            buildinfo = root / "internal/buildinfo/buildinfo.go"
            buildinfo.parent.mkdir(parents=True)
            buildinfo.write_text(f'package buildinfo\nvar Version = "{source_version}"\n', encoding="utf-8")
            catalog = root / "frontend/src/app/release-notes.json"
            catalog.parent.mkdir(parents=True)
            if releases is None:
                releases = [{"version": "1.0.0", "sections": [{"title": "修复", "items": ["版本验证"]}]}]
            catalog.write_text(json.dumps({"currentVersion": catalog_version, "releases": releases}), encoding="utf-8")
            notes = Path(temporary) / "notes.md"
            output = Path(temporary) / "github-output"
            result = subprocess.run([
                PWSH, "-NoLogo", "-NoProfile", "-File", str(ROOT / "scripts/release/prepare-release.ps1"),
                "-Tag", tag, "-RepositoryRoot", str(root), "-NotesPath", str(notes),
            ], cwd=temporary, env={**os.environ, "GITHUB_OUTPUT": str(output)},
                capture_output=True, text=True, encoding="utf-8")
            return (result, notes.read_bytes() if notes.exists() else None,
                    output.read_text(encoding="utf-8") if output.exists() else None)

    @unittest.skipUnless(PWSH, "PowerShell 7 is required for release validation checks")
    def test_release_preparation_accepts_matching_tag_and_writes_utf8_notes(self):
        result, notes, output = self.run_release_preparation()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output, "version=1.0.0\n")
        self.assertFalse(notes.startswith(b"\xef\xbb\xbf"))
        self.assertIn("## Vane v1.0.0", notes.decode("utf-8"))
        self.assertIn("版本验证", notes.decode("utf-8"))

    @unittest.skipUnless(PWSH, "PowerShell 7 is required for release validation checks")
    def test_release_preparation_rejects_branch_names_and_non_release_tags(self):
        for tag in ("main", "1.0.0", "v1.0", "v01.0.0", "v1.0.0-beta.1"):
            with self.subTest(tag=tag):
                result, notes, output = self.run_release_preparation(tag=tag)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Release tag must use vMajor.Minor.Patch", result.stderr)
                self.assertIsNone(notes)
                self.assertIsNone(output)

    @unittest.skipUnless(PWSH, "PowerShell 7 is required for release validation checks")
    def test_release_preparation_rejects_inconsistent_version_or_notes(self):
        for changes in (
            {"source_version": "1.0.1"},
            {"catalog_version": "1.0.1"},
            {"releases": []},
            {"releases": [{"version": "1.0.0", "sections": []}]},
            {"releases": [{"version": "1.0.0"}, {"version": "1.0.0"}]},
        ):
            with self.subTest(changes=changes):
                result, notes, output = self.run_release_preparation(**changes)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(notes)
                self.assertIsNone(output)

    def test_workflow_uses_pinned_wails_v3_and_current_product_names(self):
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")

        self.assertIn("github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.96", workflow)
        self.assertIn("wails3 task windows:build", workflow)
        self.assertIn("build/bin/Vane.exe", workflow)
        self.assertIn("./scripts/release/prepare-release.ps1", workflow)
        self.assertNotIn("workflow_dispatch", workflow)
        prepare = (ROOT / "scripts/release/prepare-release.ps1").read_text(encoding="utf-8")
        self.assertIn("frontend/src/app/release-notes.json", prepare)
        self.assertIn("$catalog.releases", prepare)
        self.assertIn("Vane-windows-amd64.zip", workflow)
        self.assertNotIn("github.com/wailsapp/wails/v2", workflow)
        self.assertNotIn("Claude Request Forwarder", workflow)

    def test_release_uses_github_cli_and_repository_token(self):
        workflow = WORKFLOW_PATH.read_text(encoding="utf-8")
        self.assertIn("GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}", workflow)
        self.assertIn("gh release create", workflow)
        self.assertIn("--verify-tag", workflow)
        self.assertIn("gh release upload", workflow)
        self.assertNotIn("Create Gitea release", workflow)
        self.assertNotIn("MultipartFormDataContent", workflow)

    def test_current_versions_agree_across_build_and_release_surfaces(self):
        buildinfo_source = (ROOT / "internal/buildinfo/buildinfo.go").read_text(encoding="utf-8")
        version = re.search(r'Version\s*=\s*"([^\"]+)"', buildinfo_source).group(1)
        self.assertRegex(version, r"^\d+\.\d+\.\d+$")
        windows_version = f"{version}.0"
        versioninfo = json.loads((ROOT / "build/windows/versioninfo.json").read_text(encoding="utf-8"))
        self.assertEqual(versioninfo["StringFileInfo"]["ProductVersion"], version)
        self.assertEqual(versioninfo["StringFileInfo"]["FileVersion"], windows_version)
        for field in ("FileVersion", "ProductVersion"):
            numeric = versioninfo["FixedFileInfo"][field]
            self.assertEqual([numeric[key] for key in ("Major", "Minor", "Patch", "Build")],
                             [int(part) for part in windows_version.split(".")])
        windows_info = json.loads((ROOT / "build/windows/info.json").read_text(encoding="utf-8"))
        self.assertEqual(windows_info["fixed"]["file_version"], windows_version)
        self.assertEqual(windows_info["info"]["0000"]["ProductVersion"], version)
        wails = json.loads((ROOT / "build/wails.json").read_text(encoding="utf-8"))
        self.assertEqual(wails["info"]["productVersion"], version)
        catalog = json.loads((ROOT / "frontend/src/app/release-notes.json").read_text(encoding="utf-8"))
        self.assertEqual(catalog["currentVersion"], version)
        self.assertEqual(catalog["releases"][0]["version"], version)
        package = json.loads((ROOT / "frontend/package.json").read_text(encoding="utf-8"))
        lock = json.loads((ROOT / "frontend/package-lock.json").read_text(encoding="utf-8"))
        self.assertEqual(package["version"], version)
        self.assertEqual(lock["version"], version)
        self.assertEqual(lock["packages"][""]["version"], version)
        self.assertIn(f'version: "{version}"', (ROOT / "build/config.yml").read_text(encoding="utf-8"))
        for platform in ("windows", "darwin", "linux"):
            task = (ROOT / f"build/{platform}/Taskfile.yml").read_text(encoding="utf-8")
            self.assertIn(f'{{{{default "{version}" .VERSION}}}}', task)
        manifest = (ROOT / "build/windows/app.manifest").read_text(encoding="utf-8")
        self.assertIn(f'version="{windows_version}"', manifest)
        self.assertIn(f'VERSION="{version}"', PACKAGE_SCRIPT_PATH.read_text(encoding="utf-8"))
        self.assertIn(f'version-{version}-blue', (ROOT / "README.md").read_text(encoding="utf-8"))

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

    @unittest.skipUnless(BASH, "Bash is required for shell script behavior checks")
    def test_native_build_packages_current_entry_from_an_unrelated_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "project"
            script = root / "scripts/build/build.sh"
            script.parent.mkdir(parents=True)
            shutil.copyfile(ROOT / "scripts/build/build.sh", script)
            for name in ("README.md", "LICENSE", "CLAUDE.md", "AGENTS.md"):
                (root / name).write_text(name, encoding="utf-8")
            fake_bin = root / "fake-bin"
            fake_bin.mkdir()
            stubs = {
                "go": "#!/bin/bash\ncase \"$2\" in GOOS) echo windows;; GOARCH) echo amd64;; esac\n",
                "wails3": "#!/bin/bash\nprintf '%s\\n' \"$@\" > build-arguments.txt\nmkdir -p build/bin\nprintf 'desktop fixture' > build/bin/Vane.exe\n",
                "python3": f"#!/bin/bash\nexec {shlex.quote(sys.executable.replace(chr(92), '/'))} \"$@\"\n",
            }
            for name, source in stubs.items():
                path = fake_bin / name
                path.write_text(source, encoding="utf-8", newline="\n")
                path.chmod(0o755)
            environment = dict(os.environ)
            environment["PATH"] = str(fake_bin) + os.pathsep + environment["PATH"]
            subprocess.run([BASH, str(script), "1.0.0"], cwd=temporary,
                           env=environment, check=True, capture_output=True)

            from zipfile import ZipFile

            archive_path = root / "build/dist/Vane-windows-amd64.zip"
            with ZipFile(archive_path) as archive:
                self.assertEqual(set(archive.namelist()), {
                    "Vane-windows-amd64/Vane.exe",
                    "Vane-windows-amd64/README.md",
                    "Vane-windows-amd64/LICENSE",
                })
            arguments = (root / "build-arguments.txt").read_text(encoding="utf-8")
            self.assertIn("VERSION=1.0.0", arguments)
            self.assertIn("COMMIT=unknown", arguments)
            self.assertTrue(Path(str(archive_path) + ".sha256").is_file())

    @unittest.skipUnless(BASH, "Bash is required for shell script behavior checks")
    def test_release_updates_relocated_files_and_preserves_local_rules_and_backups(self):
        files = [
            "README.md", "build/windows/versioninfo.json", "build/windows/info.json",
            "build/wails.json", "build/config.yml", "build/windows/Taskfile.yml",
            "build/darwin/Taskfile.yml", "build/linux/Taskfile.yml",
            "build/windows/app.manifest", "internal/buildinfo/buildinfo.go",
            "scripts/build/package-windows.sh", "frontend/package.json",
            "frontend/package-lock.json", "frontend/src/app/release-notes.json",
            "scripts/release/release.sh", "scripts/release/generate_release_notes.py",
        ]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "project"
            for relative in files:
                destination = root / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, destination)
            for name in ("CLAUDE.md", "AGENTS.md", "notes.bak"):
                (root / name).write_text("caller owned", encoding="utf-8")
            buildinfo_path = root / "internal/buildinfo/buildinfo.go"
            current = re.search(r'Version\s*=\s*"([^"]+)"',
                                buildinfo_path.read_text(encoding="utf-8")).group(1)
            major, minor, patch = (int(part) for part in current.split("."))
            target = f"{major}.{minor}.{patch + 1}"
            # The release boundary is a separate dependency, already covered by
            # the generator tests. Supply deterministic history for this fixture.
            generator = root / "scripts/release/generate_release_notes.py"
            source = generator.read_text(encoding="utf-8")
            start = source.index("def run_git(")
            end = source.index("def normalize_version(", start)
            source = source[:start] + (
                "def run_git(*args):\n"
                "    if '--first-parent' in args:\n"
                f"        return 'boundary\\x1fchore(release): 发布 v{current}'\n"
                "    return 'fix(build): 兼容新目录'\n\n\n"
            ) + source[end:]
            generator.write_text(source, encoding="utf-8")
            subprocess.run([BASH, str(root / "scripts/release/release.sh"), target, "目录迁移"],
                           cwd=temporary, check=True, capture_output=True,
                           env={**os.environ, "PYTHONIOENCODING": "utf-8"})

            buildinfo = (root / "internal/buildinfo/buildinfo.go").read_text(encoding="utf-8")
            self.assertRegex(buildinfo, rf'Version\s*=\s*"{re.escape(target)}"')
            info = json.loads((root / "build/windows/versioninfo.json").read_text(encoding="utf-8"))
            self.assertEqual(info["StringFileInfo"]["ProductVersion"], target)
            catalog = json.loads((root / "frontend/src/app/release-notes.json").read_text(encoding="utf-8"))
            self.assertEqual(catalog["currentVersion"], target)
            self.assertIn(current, [item["version"] for item in catalog["releases"]])
            for name in ("CLAUDE.md", "AGENTS.md", "notes.bak"):
                self.assertEqual((root / name).read_text(encoding="utf-8"), "caller owned")


if __name__ == "__main__":
    unittest.main()
