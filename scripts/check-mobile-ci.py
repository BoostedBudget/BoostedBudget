#!/usr/bin/env python3
"""Fail closed when mobile CI triggers or dependency updates drift (#1654)."""
from __future__ import annotations

import fnmatch
import json
import pathlib
import re
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

REQUIRED_STEPS = (
    ("bun install --frozen-lockfile", "."),
    ("bun run --filter @boostedbudget/design-tokens check", "."),
    ("bun run --filter @boostedbudget/design-tokens test", "."),
    ("bun run --filter @boostedbudget/api-client test", "."),
    ("bun run --cwd apps/mobile lint", "."),
    ("bun run --cwd apps/mobile typecheck", "."),
    (
        "bun run --cwd apps/mobile test:ci "
        "--coverageThreshold='{\"global\":{\"lines\":82}}'",
        ".",
    ),
    ("bun run doctor", "apps/mobile"),
    ("bun run --cwd apps/mobile export:web", "."),
    ("bun run --cwd apps/mobile export:ios", "."),
    ("bun run --cwd apps/mobile export:android", "."),
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
    "expo-system-ui",
    "expo-doctor",
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

PINNED_ACTION = re.compile(r"^[^@\s]+@[0-9a-f]{40}$")
PLAYWRIGHT_VERSION_EXPRESSION = (
    "require(require.resolve('@playwright/test/package.json', { paths: ['./apps/web'] })).version"
)
PLAYWRIGHT_VERSION_ASSIGNMENT = f'version="$(node -p "{PLAYWRIGHT_VERSION_EXPRESSION}")"'
EXACT_SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
STANDARD_SEMVER_SPEC = re.compile(
    r"^[~^]?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$"
)

REACT_LOCATIONS = (
    ("root dependencies", "root", "dependencies"),
    ("web dependencies", "web", "dependencies"),
    ("mobile dependencies", "mobile", "dependencies"),
    ("API-client devDependencies", "api-client", "devDependencies"),
)

MOBILE_VERSION_POLICIES = (
    ("apps/mobile expo", "expo", "~", 57, None),
    ("apps/mobile expo-router", "expo-router", "~", 57, None),
    ("apps/mobile react-native", "react-native", "", 0, 86),
    ("apps/mobile react-native-web", "react-native-web", "~", 0, 21),
)

TANSTACK_QUERY_LOCATIONS = (
    ("apps/mobile dependencies", "mobile", "dependencies"),
    ("apps/web dependencies", "web", "dependencies"),
    ("API-client peerDependencies", "api-client", "peerDependencies"),
    ("API-client devDependencies", "api-client", "devDependencies"),
)

LOCKFILE_NAMES = (
    "bun.lock",
    "bun.lockb",
    "package-lock.json",
    "npm-shrinkwrap.json",
    "yarn.lock",
    "pnpm-lock.yaml",
)
WORKSPACE_PATTERN = re.compile(r"^(apps|packages)/(\*|[A-Za-z0-9][A-Za-z0-9._-]*)$")
DEPENDENCY_SECTIONS = ("dependencies", "devDependencies")
MANUAL_UPDATE_POLICIES = {
    "react": {"version-update:semver-minor", "version-update:semver-major"},
    "react-dom": {"version-update:semver-minor", "version-update:semver-major"},
    "react-native": {"version-update:semver-minor", "version-update:semver-major"},
    "react-native-web": {"version-update:semver-minor", "version-update:semver-major"},
    "nativewind": {
        "version-update:semver-patch",
        "version-update:semver-minor",
        "version-update:semver-major",
    },
    "react-native-css": {
        "version-update:semver-patch",
        "version-update:semver-minor",
        "version-update:semver-major",
    },
}


def normalized_command(step: dict) -> str:
    """Return executable text in a canonical one-line form.

    Required CI gates are deliberately single commands. Keeping wrappers, comments, and shell
    conditionals distinct makes an inert mention fail instead of satisfying a substring search.
    """
    return " ".join(str(step.get("run", "")).split())


def working_directory(step: dict) -> str:
    value = str(step.get("working-directory", ".")).rstrip("/")
    return value or "."


def enabled_exact_steps(steps: list[dict], command: str, cwd: str) -> list[dict]:
    return [
        step
        for step in steps
        if "if" not in step
        and normalized_command(step) == command
        and working_directory(step) == cwd
    ]


def matches(path: str, pattern: str) -> bool:
    if pattern.endswith("/**"):
        return path == pattern[:-3] or path.startswith(pattern[:-2])
    return fnmatch.fnmatchcase(path, pattern)


def dependency_matches(name: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatchcase(name, pattern) for pattern in patterns)


def valid_semver_spec(value: object) -> bool:
    if not isinstance(value, str):
        return False
    match = STANDARD_SEMVER_SPEC.fullmatch(value)
    if match is None:
        return False
    prerelease = match.group(1)
    if prerelease is None:
        return True
    return all(
        not (identifier.isdigit() and len(identifier) > 1 and identifier.startswith("0"))
        for identifier in prerelease.split(".")
    )


def load_yaml(path: pathlib.Path):
    try:
        return yaml.safe_load(path.read_text()) or {}
    except (OSError, yaml.YAMLError) as exc:
        raise ValueError(f"cannot read valid YAML from {path}: {exc}") from exc


def load_json(path: pathlib.Path) -> dict:
    try:
        value = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read valid JSON from {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"JSON document must be an object: {path}")
    return value


def dependency_value(package: dict, section: str, dependency: str) -> str:
    dependencies = package.get(section) or {}
    if not isinstance(dependencies, dict):
        return ""
    value = dependencies.get(dependency, "")
    return value if isinstance(value, str) else ""


def numeric_version(value: str) -> tuple[int, int, int] | None:
    match = EXACT_SEMVER.fullmatch(value)
    if not match:
        return None
    return tuple(int(part) for part in match.groups())


def require_version_shape(
    findings: list[str],
    label: str,
    value: str,
    prefix: str,
    expected_major: int,
    expected_minor: int | None = None,
) -> None:
    if not value.startswith(prefix):
        findings.append(f"{label} must use {prefix!r} with a numeric compatible version")
        return
    version = numeric_version(value[len(prefix) :])
    if version is None or version[0] != expected_major:
        findings.append(f"{label} must stay within major {expected_major}")
        return
    if expected_minor is not None and version[1] != expected_minor:
        findings.append(f"{label} must stay within {expected_major}.{expected_minor}.x")


def override_dependency_names(overrides: dict) -> set[str]:
    names: set[str] = set()
    for key, value in overrides.items():
        if isinstance(key, str):
            names.add(key.partition("@")[0])
        if isinstance(value, dict):
            names.update(override_dependency_names(value))
    return names


def declared_workspace_directories(
    root: pathlib.Path, root_package: dict
) -> tuple[list[pathlib.Path], list[str]]:
    workspaces = root_package.get("workspaces")
    if not isinstance(workspaces, list) or not workspaces:
        return [], ["root package.json workspaces must be a non-empty list"]
    if not all(isinstance(pattern, str) and pattern for pattern in workspaces):
        return [], ["root package.json workspaces must contain non-empty string patterns"]

    root = root.resolve()
    workspace_directories: set[pathlib.Path] = set()
    findings: list[str] = []
    for pattern in workspaces:
        match = WORKSPACE_PATTERN.fullmatch(pattern)
        if match is None:
            findings.append(f"root package.json has unsafe workspace pattern: {pattern}")
            continue

        workspace_parent = root / match.group(1)
        if workspace_parent.is_symlink() or not workspace_parent.is_dir():
            findings.append(f"root package.json workspace parent is unsafe or missing: {pattern}")
            continue

        workspace_name = match.group(2)
        candidates = (
            [candidate for candidate in workspace_parent.iterdir() if candidate.is_dir()]
            if workspace_name == "*"
            else [workspace_parent / workspace_name]
        )
        if not any(candidate.is_dir() for candidate in candidates):
            findings.append(f"root package.json workspace pattern matches no directories: {pattern}")
            continue
        for candidate in candidates:
            if candidate.is_symlink():
                findings.append(f"root package.json workspace cannot be a symlink: {candidate}")
                continue
            resolved = candidate.resolve()
            try:
                relative = resolved.relative_to(root)
            except ValueError:
                findings.append(f"root package.json workspace escapes repository: {pattern}")
                continue
            if "node_modules" in relative.parts:
                findings.append(f"root package.json workspace resolves into node_modules: {relative}")
                continue
            if not (resolved / "package.json").is_file():
                findings.append(f"declared workspace is missing package.json: {relative}")
                continue
            workspace_directories.add(resolved)

    return sorted(workspace_directories), findings


def check_single_lockfile_policy(
    root: pathlib.Path, workspace_directories: list[pathlib.Path]
) -> list[str]:
    findings: list[str] = []
    root_lockfile = root / "bun.lock"
    if root_lockfile.is_symlink() or not root_lockfile.is_file():
        findings.append("single-lockfile policy requires root bun.lock as a regular file")

    for lock_name in LOCKFILE_NAMES[1:]:
        lock_path = root / lock_name
        if lock_path.is_symlink() or lock_path.exists():
            findings.append(f"single-lockfile policy forbids root {lock_name}")

    for workspace in workspace_directories:
        relative_workspace = workspace.relative_to(root)
        for lock_name in LOCKFILE_NAMES:
            lock_path = workspace / lock_name
            if lock_path.is_symlink() or lock_path.exists():
                findings.append(
                    f"single-lockfile policy forbids {relative_workspace / lock_name}"
                )
    return findings


def check_dependency_policy(
    packages: dict[str, dict],
    workspace_packages: list[tuple[pathlib.Path, dict]],
    compatibility_patterns: list[str],
) -> list[str]:
    findings: list[str] = []

    react_versions: list[tuple[str, str, str]] = []
    for label, package_name, section in REACT_LOCATIONS:
        package = packages[package_name]
        for dependency in ("react", "react-dom"):
            value = dependency_value(package, section, dependency)
            version = numeric_version(value)
            if version is None or version[:2] != (19, 2):
                findings.append(
                    f"{label} {dependency} must be an exact numeric 19.2.x version"
                )
            react_versions.append((label, dependency, value))

    distinct_react_versions = {value for _, _, value in react_versions}
    if len(distinct_react_versions) != 1:
        rendered = ", ".join(
            f"{label} {dependency}={value or '<empty>'}"
            for label, dependency, value in react_versions
        )
        findings.append(f"React and ReactDOM must use one exact version everywhere: {rendered}")

    mobile = packages["mobile"]
    for section in DEPENDENCY_SECTIONS:
        dependencies = mobile.get(section) or {}
        if not isinstance(dependencies, dict):
            findings.append(f"apps/mobile {section} must be an object")
            continue
        compatibility_dependencies = {
            dependency
            for dependency in dependencies
            if dependency_matches(dependency, compatibility_patterns)
        }
        for dependency in sorted(compatibility_dependencies):
            value = dependencies[dependency]
            if not valid_semver_spec(value):
                findings.append(
                    f"apps/mobile {section} {dependency} must use numeric semver with ^, ~, or no operator"
                )

    expected_react = react_versions[0][2]
    for workspace_path, workspace_package in workspace_packages:
        for section in DEPENDENCY_SECTIONS:
            dependencies = workspace_package.get(section) or {}
            if not isinstance(dependencies, dict):
                continue
            for dependency in ("react", "react-dom"):
                if dependency not in dependencies:
                    continue
                value = dependencies[dependency]
                version = numeric_version(value) if isinstance(value, str) else None
                if version is None or version[:2] != (19, 2) or value != expected_react:
                    findings.append(
                        f"{workspace_path} {section} {dependency} must equal shared exact React {expected_react}"
                    )

    for label, dependency, prefix, expected_major, expected_minor in MOBILE_VERSION_POLICIES:
        require_version_shape(
            findings,
            label,
            dependency_value(mobile, "dependencies", dependency),
            prefix,
            expected_major,
            expected_minor,
        )

    for label, package_name, section in TANSTACK_QUERY_LOCATIONS:
        require_version_shape(
            findings,
            f"{label} @tanstack/react-query",
            dependency_value(packages[package_name], section, "@tanstack/react-query"),
            "^",
            5,
        )

    for dependency, expected in (
        ("nativewind", "5.0.0-rc.0"),
        ("react-native-css", "3.1.0-rc.0"),
    ):
        actual = dependency_value(mobile, "dependencies", dependency)
        if actual != expected:
            findings.append(f"apps/mobile {dependency} must be pinned exactly to {expected}")

    router_value = dependency_value(packages["web"], "dependencies", "react-router")
    router_version = numeric_version(router_value[1:]) if router_value.startswith("^") else None
    if router_version is None or router_version[0] != 7 or router_version < (7, 13, 1):
        findings.append("apps/web react-router must use a caret range on 7.13.1 or newer within major 7")

    root_overrides = packages["root"].get("overrides") or {}
    if not isinstance(root_overrides, dict):
        findings.append("root overrides must be an object")
    else:
        forbidden_overrides = sorted(
            {"react", "react-dom", "lightningcss"} & override_dependency_names(root_overrides)
        )
        if forbidden_overrides:
            findings.append(
                "root overrides must not replace compatibility contracts: "
                + ", ".join(forbidden_overrides)
            )

    return findings


def check(root: pathlib.Path) -> list[str]:
    findings: list[str] = []
    workflow_path = root / ".github/workflows/test.yml"
    warm_cache_workflow_path = root / ".github/workflows/warm-ci-cache.yml"
    dependabot_path = root / ".github/dependabot.yml"
    package_paths = {
        "root": root / "package.json",
        "web": root / "apps/web/package.json",
        "mobile": root / "apps/mobile/package.json",
        "api-client": root / "packages/api-client/package.json",
    }

    try:
        workflow = load_yaml(workflow_path)
        warm_cache_workflow = load_yaml(warm_cache_workflow_path)
        dependabot = load_yaml(dependabot_path)
        packages = {name: load_json(path) for name, path in package_paths.items()}
        mobile_package = packages["mobile"]
    except ValueError as exc:
        return [str(exc)]

    workspace_directories, workspace_findings = declared_workspace_directories(
        root, packages["root"]
    )
    findings.extend(workspace_findings)
    workspace_packages: list[tuple[pathlib.Path, dict]] = []
    for workspace in workspace_directories:
        try:
            workspace_packages.append(
                (workspace.relative_to(root), load_json(workspace / "package.json"))
            )
        except ValueError as exc:
            findings.append(str(exc))

    bun_updates = [
        update
        for update in dependabot.get("updates") or []
        if update.get("package-ecosystem") == "bun"
    ]
    compatibility_patterns: list[str] = []
    if len(bun_updates) == 1:
        group = ((bun_updates[0].get("groups") or {}).get("expo-react-native-compatibility") or {})
        raw_patterns = group.get("patterns") or []
        if isinstance(raw_patterns, list):
            compatibility_patterns = [pattern for pattern in raw_patterns if isinstance(pattern, str)]

    findings.extend(
        check_dependency_policy(packages, workspace_packages, compatibility_patterns)
    )
    findings.extend(check_single_lockfile_policy(root, workspace_directories))

    jobs = workflow.get("jobs") or {}

    for label, candidate_workflow in (
        ("test workflow", workflow),
        ("cache-warm workflow", warm_cache_workflow),
    ):
        resolution_steps = [
            step
            for job in (candidate_workflow.get("jobs") or {}).values()
            for step in (job.get("steps") or [])
            if isinstance(step, dict) and step.get("name") == "Resolve Playwright version"
        ]
        executable_lines = []
        if len(resolution_steps) == 1:
            executable_lines = [
                " ".join(line.split())
                for line in str(resolution_steps[0].get("run", "")).splitlines()
                if line.strip() and not line.lstrip().startswith("#")
            ]
        if (
            len(resolution_steps) != 1
            or "if" in resolution_steps[0]
            or PLAYWRIGHT_VERSION_ASSIGNMENT not in executable_lines
        ):
            findings.append(
                f"{label} must resolve Playwright from the apps/web workspace without assuming a linker layout"
            )
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
        steps = [step for step in job.get("steps") or [] if isinstance(step, dict)]
        action_steps = [step for step in steps if isinstance(step.get("uses"), str)]
        enabled_action_steps = [step for step in action_steps if "if" not in step]
        for step in action_steps:
            if not PINNED_ACTION.fullmatch(str(step["uses"])):
                findings.append(
                    f"mobile-test action must use a 40-character commit SHA: {step['uses']}"
                )
        checkout_steps = [
            step
            for step in enabled_action_steps
            if str(step.get("uses", "")).startswith("actions/checkout@")
        ]
        if len(checkout_steps) != 1:
            findings.append(
                f"mobile-test must contain exactly one enabled checkout action; found {len(checkout_steps)}"
            )
        node_steps = [
            step
            for step in enabled_action_steps
            if str(step.get("uses", "")).startswith("actions/setup-node@")
        ]
        if len(node_steps) != 1:
            findings.append(
                f"mobile-test must contain exactly one enabled setup-node action; found {len(node_steps)}"
            )
        node_version = str(((node_steps[0].get("with") or {}).get("node-version", ""))) if node_steps else ""
        if not node_version.startswith("22.13"):
            findings.append("mobile-test must pin Node 22.13.x or newer within the 22.13 line")
        bun_steps = [
            step
            for step in enabled_action_steps
            if str(step.get("uses", "")).startswith("oven-sh/setup-bun@")
        ]
        if len(bun_steps) != 1:
            findings.append(
                f"mobile-test must contain exactly one enabled setup-bun action; found {len(bun_steps)}"
            )
        bun_version = str(((bun_steps[0].get("with") or {}).get("bun-version", ""))) if bun_steps else ""
        if bun_version != "1.3.14":
            findings.append("mobile-test must pin Bun 1.3.14")
        for command, cwd in REQUIRED_STEPS:
            step_matches = enabled_exact_steps(steps, command, cwd)
            if len(step_matches) != 1:
                findings.append(
                    "mobile-test must contain exactly one enabled step running "
                    f"{command!r} from {cwd!r}; found {len(step_matches)}"
                )

        doctor_version = str((mobile_package.get("devDependencies") or {}).get("expo-doctor", ""))
        if doctor_version != "1.20.4":
            findings.append("apps/mobile must pin expo-doctor exactly to 1.20.4")
        if (mobile_package.get("scripts") or {}).get("doctor") != "expo-doctor":
            findings.append("apps/mobile scripts.doctor must invoke the locked expo-doctor binary")

    e2e = jobs.get("e2e-test") or {}
    e2e_steps = [step for step in e2e.get("steps") or [] if isinstance(step, dict)]
    for command in (
        "bash scripts/__tests__/check-mobile-ci.test.sh",
        "python3 scripts/check-mobile-ci.py",
    ):
        step_matches = enabled_exact_steps(e2e_steps, command, ".")
        if len(step_matches) != 1:
            findings.append(
                f"unconditional e2e-test job must contain one enabled exact step: {command}"
            )
    if e2e.get("needs") is not None:
        findings.append("e2e-test hosts mobile CI guards and must remain ungated")
    if e2e.get("if") != "always() && !cancelled()":
        findings.append("e2e-test hosts mobile CI guards and must remain enabled on every PR")

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
        actual_mobile_deps = set(mobile_package.get("dependencies") or {}) | set(
            mobile_package.get("devDependencies") or {}
        )
        compatibility_dependencies = {
            dependency
            for dependency in actual_mobile_deps
            if dependency_matches(dependency, patterns)
        }
        for dependency in sorted(compatibility_dependencies):
            matching_ignores = [
                item
                for item in ignores
                if dependency_matches(dependency, [str(item.get("dependency-name", ""))])
            ]
            effective_types = {
                update_type
                for item in matching_ignores
                for update_type in (item.get("update-types") or [])
            }
            expected_effective_types = MANUAL_UPDATE_POLICIES.get(
                dependency, {"version-update:semver-major"}
            )
            if effective_types != expected_effective_types:
                findings.append(
                    f"Dependabot effective update policy for {dependency} must hold exactly "
                    + ", ".join(sorted(expected_effective_types))
                )
            if any(item.get("versions") for item in matching_ignores):
                findings.append(
                    f"Dependabot compatibility ignores for {dependency} must not hold specific versions"
                )
            if any(
                not item.get("update-types") and not item.get("versions")
                for item in matching_ignores
            ):
                findings.append(
                    f"Dependabot compatibility ignores for {dependency} must not hold every update"
                )
        for dependency, expected_update_types in MANUAL_UPDATE_POLICIES.items():
            exact_entries = [
                item for item in ignores if item.get("dependency-name") == dependency
            ]
            if len(exact_entries) != 1:
                findings.append(
                    f"Dependabot must define one explicit manual-update policy for {dependency}"
                )
                continue
            actual_update_types = set(exact_entries[0].get("update-types") or [])
            if actual_update_types != expected_update_types:
                findings.append(
                    f"Dependabot manual-update policy for {dependency} must hold "
                    + ", ".join(sorted(expected_update_types))
                )
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
    return findings


def main() -> int:
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    findings = check(root)
    if findings:
        print("FAIL: mobile CI/dependency-policy findings:", file=sys.stderr)
        for finding in findings:
            print(f"  - {finding}", file=sys.stderr)
        return 1
    print(
        "check-mobile-ci: OK "
        "(trigger matrix, build gates, runners, dependency compatibility, and Dependabot policy)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
