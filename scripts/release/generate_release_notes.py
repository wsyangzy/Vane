#!/usr/bin/env python3
"""Generate categorized in-app release notes while preserving older versions."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path
from typing import Callable, Iterable

PROJECT_ROOT = Path(__file__).resolve().parents[2]

RELEASE_SUBJECT_PATTERN = re.compile(
    r"^chore\(release\):\s*发布\s+v(?P<version>\S+?)(?:\s+版本)?$",
    re.IGNORECASE,
)
CONVENTIONAL_SUBJECT_PATTERN = re.compile(
    r"^(?P<type>[a-z]+)(?:\((?P<scope>[^)]+)\))?!?:\s*(?P<description>.+)$",
    re.IGNORECASE,
)
VISIBLE_CHANGE_TYPES = {"feat", "fix", "perf", "refactor"}
DEFAULT_RELEASE_NOTE = "未提供发布说明"

SECTION_RULES = (
    ("渠道与故障转移", ("claude", "codex", "provider", "failover", "endpoint", "management")),
    ("凭证与成本", ("credentials", "credential", "billing", "pricing", "usage", "cost")),
    ("隐私防护", ("privacy", "security")),
    ("架构与界面", ("architecture", "core", "tracking", "store", "cache", "database", "desktop")),
    ("版本体验", ("release", "build", "frontend", "ui")),
)
FALLBACK_SECTION = "稳定性与兼容性"


def run_git(*args: str) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=PROJECT_ROOT,
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


def parse_change_subject(subject: str) -> tuple[str, str, str] | None:
    match = CONVENTIONAL_SUBJECT_PATTERN.match(subject.strip())
    if not match:
        return None

    change_type = match.group("type").lower()
    if change_type not in VISIBLE_CHANGE_TYPES:
        return None
    return (
        change_type,
        (match.group("scope") or "").strip().lower(),
        match.group("description").strip(),
    )


def section_for_change(scope: str) -> str:
    for title, prefixes in SECTION_RULES:
        if any(scope == prefix or scope.startswith(f"{prefix}-") for prefix in prefixes):
            return title
    return FALLBACK_SECTION


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


def build_sections(commit_subjects: Iterable[str], release_note: str = "") -> list[dict[str, object]]:
    section_order = [title for title, _ in SECTION_RULES] + [FALLBACK_SECTION]
    grouped: dict[str, list[str]] = {title: [] for title in section_order}

    for subject in commit_subjects:
        parsed = parse_change_subject(subject)
        if parsed is None:
            continue
        _, scope, description = parsed
        grouped[section_for_change(scope)].append(description)

    sections = [
        {"title": title, "items": unique_non_empty(grouped[title])}
        for title in section_order
        if grouped[title]
    ]
    if sections:
        return sections

    summary = release_note.strip()
    if summary and summary != DEFAULT_RELEASE_NOTE:
        return [{"title": "重点更新", "items": [summary]}]
    return [{"title": FALLBACK_SECTION, "items": ["包含稳定性与体验优化"]}]


def normalize_release_record(record: object) -> dict[str, object] | None:
    if not isinstance(record, dict):
        return None
    version = normalize_version(str(record.get("version", "")))
    if not version:
        return None

    normalized: dict[str, object] = {"version": version}
    if isinstance(record.get("sections"), list):
        normalized["sections"] = record["sections"]
    elif isinstance(record.get("highlights"), list):
        normalized["highlights"] = record["highlights"]
    else:
        return None
    return normalized


def get_archived_releases(catalog: object) -> list[dict[str, object]]:
    if not isinstance(catalog, dict):
        return []
    raw_releases = catalog.get("releases")
    if not isinstance(raw_releases, list):
        raw_releases = [catalog] if catalog.get("version") else []

    releases: list[dict[str, object]] = []
    seen: set[str] = set()
    for raw_release in raw_releases:
        release = normalize_release_record(raw_release)
        if release is None or release["version"] in seen:
            continue
        seen.add(str(release["version"]))
        releases.append(release)
    return releases


def merge_release_catalog(
    existing_catalog: object,
    new_release: dict[str, object],
) -> dict[str, object]:
    normalized_new_release = normalize_release_record(new_release)
    if normalized_new_release is None:
        raise ValueError("new release notes must include a version and content")

    new_version = str(normalized_new_release["version"])
    archived = [
        release
        for release in get_archived_releases(existing_catalog)
        if release["version"] != new_version
    ]
    return {
        "currentVersion": new_version,
        "releases": [normalized_new_release, *archived],
    }


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
    release_record: dict[str, object] = {
        "version": normalize_version(version),
        "sections": build_sections(commit_output.splitlines(), release_note),
    }

    existing_catalog: object = {}
    if output_path.exists():
        existing_catalog = json.loads(output_path.read_text(encoding="utf-8"))
    catalog = merge_release_catalog(existing_catalog, release_record)

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(catalog, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return catalog


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="new release version")
    parser.add_argument("--previous-version", required=True, help="current release version")
    parser.add_argument("--release-note", default="", help="fallback release summary")
    parser.add_argument("--output", required=True, type=Path, help="release catalog JSON path")
    args = parser.parse_args()

    catalog = generate_release_notes(
        version=args.version,
        previous_version=args.previous_version,
        release_note=args.release_note,
        output_path=args.output,
    )
    current_release = catalog["releases"][0]
    change_count = sum(len(section["items"]) for section in current_release["sections"])
    print(
        f"已归档 v{catalog['currentVersion']} 新版功能提示，"
        f"共 {len(current_release['sections'])} 类 {change_count} 项；"
        f"目录保留 {len(catalog['releases'])} 个版本"
    )


if __name__ == "__main__":
    main()
