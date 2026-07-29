import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.generate_release_notes import (
    build_highlights,
    find_release_boundary,
    generate_release_notes,
    parse_change_subject,
)


class ReleaseNotesGeneratorTests(unittest.TestCase):
    def test_finds_previous_release_from_first_parent_history(self):
        calls = []

        def git_runner(*args):
            calls.append(args)
            return (
                "newer\x1ffeat: newer change\n"
                "release-84\x1fchore(release): 发布 v8.4\n"
                "older\x1fchore(release): 发布 v8.3"
            )

        self.assertEqual(find_release_boundary("8.4", git_runner), "release-84")
        self.assertEqual(calls, [("log", "--first-parent", "--format=%H%x1f%s", "HEAD")])

    def test_falls_back_to_version_tag_when_release_commit_is_absent(self):
        def git_runner(*args):
            if args[0] == "log":
                return "head\x1ffix: patch release"
            if "refs/tags/v8.4^{commit}" in args:
                return "tag-84"
            raise subprocess.CalledProcessError(1, ["git", *args])

        self.assertEqual(find_release_boundary("8.4", git_runner), "tag-84")

    def test_prefers_feature_commits_and_keeps_a_curated_summary(self):
        subjects = [
            "fix(proxy): 修复转移状态",
            "feat(privacy): 增加请求级脱敏",
            "docs: 更新说明",
            "feat(provider): 完善初次配置",
        ]

        self.assertEqual(
            build_highlights(subjects, "重点更新"),
            ["重点更新", "增加请求级脱敏", "完善初次配置"],
        )
        self.assertEqual(parse_change_subject("chore: update deps"), None)

    def test_uses_fix_descriptions_when_a_release_has_no_features(self):
        self.assertEqual(
            build_highlights(["fix(codex): 修复并发切换", "ci: Windows 发布"]),
            ["修复并发切换"],
        )

    def test_writes_release_json_for_the_requested_version(self):
        def git_runner(*args):
            if args[0] == "log" and "--first-parent" in args:
                return "release-84\x1fchore(release): 发布 v8.4"
            return "feat: 新增渠道成本限额\nfix: 修复启动问题"

        with tempfile.TemporaryDirectory() as temp_dir:
            output_path = Path(temp_dir) / "release-notes.json"
            data = generate_release_notes("8.5", "8.4", "", output_path, git_runner)

            self.assertEqual(data, {
                "version": "8.5",
                "highlights": ["新增渠道成本限额"],
            })
            self.assertIn('"version": "8.5"', output_path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
