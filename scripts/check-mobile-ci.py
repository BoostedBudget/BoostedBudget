#!/usr/bin/env python3
"""Fail closed when mobile CI triggers or dependency updates drift (#1654)."""
from __future__ import annotations

import fnmatch
import json
import pathlib
import sys

import yaml


REQUIRED_MOBILE_PATHS = {
    "apps/mobile/**",
    "packages/api-client/**",
    "packages/design-tokens/**",
    "api/openapi.yaml",
    "package.json",
    "bun.lock",
    "bunfig.toml",
    "turbo.json",
    ".github/workflows/test.yml",
    ".github/dependabot.yml",
    "scripts/check-mobile-ci.py",
    "scripts/__tests__/check-mobile-ci.test.sh",
}

POSITIVE_PATHS = (
    "apps/mobile/app/index.tsx",
    "packages/api-client/src/client.ts",
    "packages/design-tokens/src/tokens.ts",
    "api/openapi.yaml",
    "package.json",
    "bun.lock",
    "bunfig.toml",
    "turbo.json",
    ".github/workflows/test.yml",
    ".github/dependabot.yml",
    "scripts/check-mobile-ci.py",
    "scripts/__tests__/check-mobile-ci.test.sh",
)

NEGATIVE_PATHS = (
    "backend/internal/services/example.go",
    "backend/config/config.go",
    "backend/migrations/000999_example.up.sql",
    "apps/web/src/App.tsx",
    "docs/mobile.md",
    "packages/shared-types/src/index.ts",
    "config/subscription_tiers.yaml",
    "README.md",
)

REQUIRED_COMMANDS = (
    "bun install --frozen-lockfile",
    "bun run --filter @boostedbudget/design-tokens check",
    "bun run --cwd apps/mobile lint",
    "bun run --cwd apps/mobile typecheck",
    "bun run --cwd apps/mobile test:ci",
    "bunx expo-doctor",
    "bun run --cwd apps/mobile export:web",
    "bun run --cwd apps/mobile export:ios",
    "bun run --cwd apps/mobile export:android",
)

MOBILE_COMPATIBILITY_DEPS = {
    "@babel/core",
    "@babel/runtime",
    "@testing-library/react-native",
    "babel-preset-expo",
    "expo",
    "expo-constants",
    "expo-linking",
    "expo-router",
    "expo-status-bar",
    "jest-expo",
    "nativewind",
    "react",
    "react-dom",
    "react-native",
    "react-native-css",
    "react-native-reanimated",
    "react-native-safe-area-context",
    "react-native-web",
    "react-native-worklets",
}


def matches(path: str, pattern: str) -> bool:
    if pattern.endswith("/**"):
        return path == pattern[:-3] or path.startswith(pattern[:-2])
    return fnmatch.fnmatchcase(path, pattern)


def dependency_matches(name: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatchcase(name, pattern) for pattern in patterns)


def load_yaml(path: pathlib.Path):
    try:
        return yaml.safe_load(path.read_text()) or {}
    except (OSError, yaml.YAMLError) as exc:
        raise ValueError(f"cannot read valid YAML from {path}: {exc}") from exc


def check(root: pathlib.Path) -> list[str]:
    findings: list[str] = []
    workflow_path = root / ".github/workflows/test.yml"
    dependabot_path = root / ".github/dependabot.yml"
    mobile_package_path = root / "apps/mobile/package.json"

    try:
        workflow = load_yaml(workflow_path)
        dependabot = load_yaml(dependabot_path)
        mobile_package = json.loads(mobile_package_path.read_text())
    except (ValueError, OSError) as exc:
        return [str(exc)]

    jobs = workflow.get("jobs") or {}
    detect = jobs.get("detect-changes") or {}
    outputs = detect.get("outputs") or {}
    if outputs.get("mobile") != "${{ steps.filter.outputs.mobile }}":
        findings.append(
            "detect-changes.outputs.mobile must read steps.filter.outputs.mobile exactly"
        )

    filter_steps = [
        step
        for step in detect.get("steps") or []
        if isinstance(step, dict) and step.get("id") == "filter"
    ]
    if len(filter_steps) != 1:
        findings.append("detect-changes must contain exactly one step with id 'filter'")
        mobile_paths: list[str] = []
    else:
        raw_filters = (filter_steps[0].get("with") or {}).get("filters", "")
        try:
            parsed_filters = yaml.safe_load(raw_filters) or {}
        except yaml.YAMLError as exc:
            findings.append(f"paths-filter configuration is invalid YAML: {exc}")
            parsed_filters = {}
        mobile_paths = parsed_filters.get("mobile") or []
        if not isinstance(mobile_paths, list) or not all(
            isinstance(path, str) for path in mobile_paths
        ):
            findings.append("paths-filter.mobile must be a list of literal path patterns")
            mobile_paths = []

    missing_paths = sorted(REQUIRED_MOBILE_PATHS - set(mobile_paths))
    if missing_paths:
        findings.append(f"mobile filter is missing required paths: {', '.join(missing_paths)}")

    for path in POSITIVE_PATHS:
        if not any(matches(path, pattern) for pattern in mobile_paths):
            findings.append(f"mobile trigger matrix false negative: {path}")
    for path in NEGATIVE_PATHS:
        matched = [pattern for pattern in mobile_paths if matches(path, pattern)]
        if matched:
            findings.append(
                f"mobile trigger matrix false positive: {path} matched {', '.join(matched)}"
            )

    job = jobs.get("mobile-test")
    if not isinstance(job, dict):
        findings.append("workflow must define a mobile-test job")
    else:
        if job.get("needs") != "detect-changes":
            findings.append("mobile-test.needs must be detect-changes")
        if job.get("if") != "needs.detect-changes.outputs.mobile == 'true'":
            findings.append("mobile-test.if must gate on the declared mobile output")
        if job.get("runs-on") != "ubicloud-standard-2":
            findings.append("mobile-test must run on ubicloud-standard-2")
        text = "\n".join(
            str(step.get("run", ""))
            for step in job.get("steps") or []
            if isinstance(step, dict)
        )
        uses = "\n".join(
            str(step.get("uses", ""))
            for step in job.get("steps") or []
            if isinstance(step, dict)
        )
        if "actions/checkout@" not in uses:
            findings.append("mobile-test must check out the repository")
        node_steps = [
            step
            for step in job.get("steps") or []
            if isinstance(step, dict) and str(step.get("uses", "")).startswith("actions/setup-node@")
        ]
        node_version = str(((node_steps[0].get("with") or {}).get("node-version", ""))) if node_steps else ""
        if not node_version.startswith("22.13"):
            findings.append("mobile-test must pin Node 22.13.x or newer within the 22.13 line")
        bun_steps = [
            step
            for step in job.get("steps") or []
            if isinstance(step, dict) and str(step.get("uses", "")).startswith("oven-sh/setup-bun@")
        ]
        bun_version = str(((bun_steps[0].get("with") or {}).get("bun-version", ""))) if bun_steps else ""
        if bun_version != "1.3.14":
            findings.append("mobile-test must pin Bun 1.3.14")
        for command in REQUIRED_COMMANDS:
            if command not in text:
                findings.append(f"mobile-test is missing command: {command}")
        if "--coverageThreshold" not in text or '"global":{"lines":82' not in text.replace(" ", ""):
            findings.append("mobile-test must enforce a global 82% Jest line threshold")

    e2e = jobs.get("e2e-test") or {}
    e2e_text = "\n".join(
        str(step.get("run", ""))
        for step in e2e.get("steps") or []
        if isinstance(step, dict)
    )
    for command in (
        "bash scripts/__tests__/check-mobile-ci.test.sh",
        "python3 scripts/check-mobile-ci.py",
    ):
        if command not in e2e_text:
            findings.append(f"unconditional e2e-test job must run: {command}")
    if e2e.get("needs") is not None:
        findings.append("e2e-test hosts mobile CI guards and must remain ungated")
    if e2e.get("if") != "always() && !cancelled()":
        findings.append("e2e-test hosts mobile CI guards and must remain enabled on every PR")

    bun_updates = [
        update
        for update in dependabot.get("updates") or []
        if update.get("package-ecosystem") == "bun"
    ]
    if len(bun_updates) != 1 or bun_updates[0].get("directory") != "/":
        findings.append("Dependabot must have exactly one root-directory Bun updater")
    else:
        bun_update = bun_updates[0]
        groups = bun_update.get("groups") or {}
        if next(iter(groups), None) != "expo-react-native-compatibility":
            findings.append(
                "Expo compatibility group must be first so generic groups cannot claim its dependencies"
            )
        group = groups.get("expo-react-native-compatibility") or {}
        patterns = group.get("patterns") or []
        if group.get("update-types") != ["minor", "patch"]:
            findings.append("Expo compatibility group must update minor and patch versions only")
        missing_deps = sorted(
            dep for dep in MOBILE_COMPATIBILITY_DEPS if not dependency_matches(dep, patterns)
        )
        if missing_deps:
            findings.append(
                "Expo compatibility group misses mobile dependencies: " + ", ".join(missing_deps)
            )
        ignores = bun_update.get("ignore") or []
        ignored_majors = {
            str(item.get("dependency-name"))
            for item in ignores
            if "version-update:semver-major" in (item.get("update-types") or [])
        }
        for dependency in ("expo", "react-native", "nativewind"):
            if not dependency_matches(dependency, list(ignored_majors)):
                findings.append(f"Dependabot must hold {dependency} major updates for manual SDK upgrades")
        for name, candidate in groups.items():
            if name == "expo-react-native-compatibility":
                continue
            candidate_patterns = candidate.get("patterns") or []
            excluded = candidate.get("exclude-patterns") or []
            overlaps = sorted(
                dep
                for dep in MOBILE_COMPATIBILITY_DEPS
                if dependency_matches(dep, candidate_patterns)
                and not dependency_matches(dep, excluded)
            )
            if overlaps:
                findings.append(
                    f"Dependabot group {name} competes for mobile compatibility dependencies: "
                    + ", ".join(overlaps)
                )

    actual_mobile_deps = set(mobile_package.get("dependencies") or {}) | set(
        mobile_package.get("devDependencies") or {}
    )
    absent_expected = sorted(MOBILE_COMPATIBILITY_DEPS - actual_mobile_deps)
    if absent_expected:
        findings.append(
            "mobile compatibility policy lists dependencies absent from apps/mobile/package.json: "
            + ", ".join(absent_expected)
        )
    for lock_name in ("bun.lock", "bun.lockb", "package-lock.json", "yarn.lock", "pnpm-lock.yaml"):
        if (root / "apps/mobile" / lock_name).exists():
            findings.append(f"single-lockfile policy forbids apps/mobile/{lock_name}")

    return findings


def main() -> int:
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    findings = check(root)
    if findings:
        print("FAIL: mobile CI/dependency-policy findings:", file=sys.stderr)
        for finding in findings:
            print(f"  - {finding}", file=sys.stderr)
        return 1
    print("check-mobile-ci: OK (trigger matrix, build gates, runners, and Dependabot policy)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
