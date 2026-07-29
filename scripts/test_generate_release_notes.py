import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.generate_release_notes import (
    build_sections,
    find_release_boundary,
    generate_release_notes,
    parse_change_subject,
    section_for_change,
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

    def test_parses_scope_and_maps_provider_prefixes(self):
        self.assertEqual(
            parse_change_subject("fix(codex-proxy): 修复并发切换"),
            ("fix", "codex-proxy", "修复并发切换"),
        )
        self.assertEqual(section_for_change("codex-proxy"), "渠道与故障转移")
        self.assertEqual(parse_change_subject("chore: update deps"), None)

    def test_groups_all_visible_changes_in_release_order(self):
        subjects = [
            "fix(codex): 修复转移状态",
            "feat(privacy): 增加请求级脱敏",
            "docs: 更新说明",
            "feat(credentials): 增加每日成本限额",
            "refactor(architecture): 收敛 Provider 目录",
            "feat(release): 增加版本提示",
        ]

        self.assertEqual(build_sections(subjects, "不应重复展示的摘要"), [
            {"title": "渠道与故障转移", "items": ["修复转移状态"]},
            {"title": "凭证与成本", "items": ["增加每日成本限额"]},
            {"title": "隐私防护", "items": ["增加请求级脱敏"]},
            {"title": "架构与界面", "items": ["收敛 Provider 目录"]},
            {"title": "版本体验", "items": ["增加版本提示"]},
        ])

    def test_uses_curated_summary_only_when_no_visible_commits_exist(self):
        self.assertEqual(
            build_sections(["docs: 更新说明"], "维护版本"),
            [{"title": "重点更新", "items": ["维护版本"]}],
        )

    def test_writes_categorized_release_json_for_the_requested_version(self):
        def git_runner(*args):
            if args[0] == "log" and "--first-parent" in args:
                return "release-84\x1fchore(release): 发布 v8.4"
            return "feat(credentials): 新增渠道成本限额\nfix(codex): 修复启动问题"

        with tempfile.TemporaryDirectory() as temp_dir:
            output_path = Path(temp_dir) / "release-notes.json"
            data = generate_release_notes("8.5", "8.4", "", output_path, git_runner)

            self.assertEqual(data, {
                "version": "8.5",
                "sections": [
                    {"title": "渠道与故障转移", "items": ["修复启动问题"]},
                    {"title": "凭证与成本", "items": ["新增渠道成本限额"]},
                ],
            })
            self.assertIn('"version": "8.5"', output_path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
