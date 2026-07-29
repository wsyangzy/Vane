#!/usr/bin/env python3
"""Generate the in-app release announcement from commits since the last release."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path
from typing import Callable, Iterable

RELEASE_SUBJECT_PATTERN = re.compile(
    r"^chore\(release\):\s*发布\s+v(?P<version>\S+?)(?:\s+版本)?$",
    re.IGNORECASE,
)
CONVENTIONAL_SUBJECT_PATTERN = re.compile(
    r"^(?P<type>[a-z]+)(?:\([^)]*\))?!?:\s*(?P<description>.+)$",
    re.IGNORECASE,
)
VISIBLE_CHANGE_TYPES = {"feat", "fix", "perf", "refactor"}
DEFAULT_RELEASE_NOTE = "未提供发布说明"


def run_git(*args: str) -> str:
    result = subprocess.run(
        ["git", *args],
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    return result.stdout.strip()


def normalize_version(value: str) -> str:
    return value.strip().removeprefix("v")


def find_release_boundary(
    previous_version: str,
    git_runner: Callable[..., str] = run_git,
) -> str:
    target_version = normalize_version(previous_version)
    log_output = git_runner("log", "--first-parent", "--format=%H%x1f%s", "HEAD")

    for line in log_output.splitlines():
        commit_hash, separator, subject = line.partition("\x1f")
        if not separator:
            continue
        match = RELEASE_SUBJECT_PATTERN.match(subject.strip())
        if match and normalize_version(match.group("version")) == target_version:
            return commit_hash

    for tag_name in (f"v{target_version}", target_version):
        try:
            return git_runner("rev-parse", "--verify", f"refs/tags/{tag_name}^{{commit}}")
        except subprocess.CalledProcessError:
            continue

    raise RuntimeError(
        f"找不到 v{target_version} 的发布提交或 tag，无法确定新版功能提交范围"
    )


def parse_change_subject(subject: str) -> tuple[str, str] | None:
    match = CONVENTIONAL_SUBJECT_PATTERN.match(subject.strip())
    if not match:
        return None

    change_type = match.group("type").lower()
    if change_type not in VISIBLE_CHANGE_TYPES:
        return None
    return change_type, match.group("description").strip()


def unique_non_empty(values: Iterable[str]) -> list[str]:
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        normalized = value.strip()
        if not normalized or normalized in seen:
            continue
        seen.add(normalized)
        result.append(normalized)
    return result


def build_highlights(commit_subjects: Iterable[str], release_note: str = "") -> list[str]:
    parsed_changes = [
        parsed
        for subject in commit_subjects
        if (parsed := parse_change_subject(subject)) is not None
    ]
    feature_descriptions = [description for change_type, description in parsed_changes if change_type == "feat"]
    fallback_descriptions = [description for _, description in parsed_changes]

    candidates: list[str] = []
    if release_note.strip() and release_note.strip() != DEFAULT_RELEASE_NOTE:
        candidates.append(release_note.strip())
    candidates.extend(feature_descriptions or fallback_descriptions)

    highlights = unique_non_empty(candidates)
    return highlights or ["包含稳定性与体验优化"]


def generate_release_notes(
    version: str,
    previous_version: str,
    release_note: str,
    output_path: Path,
    git_runner: Callable[..., str] = run_git,
) -> dict[str, object]:
    boundary = find_release_boundary(previous_version, git_runner)
    commit_output = git_runner(
        "log",
        f"{boundary}..HEAD",
        "--no-merges",
        "--reverse",
        "--format=%s",
    )
    data: dict[str, object] = {
        "version": normalize_version(version),
        "highlights": build_highlights(commit_output.splitlines(), release_note),
    }

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return data


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="new release version")
    parser.add_argument("--previous-version", required=True, help="current release version")
    parser.add_argument("--release-note", default="", help="optional curated release summary")
    parser.add_argument("--output", required=True, type=Path, help="generated JSON path")
    args = parser.parse_args()

    data = generate_release_notes(
        version=args.version,
        previous_version=args.previous_version,
        release_note=args.release_note,
        output_path=args.output,
    )
    print(f"已生成 v{data['version']} 新版功能提示，共 {len(data['highlights'])} 项")


if __name__ == "__main__":
    main()
