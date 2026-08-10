#!/usr/bin/env python3
"""Validates a Playwright storage-state JSON file actually contains a real Clerk session.

Extracted from .github/workflows/test.yml's "Guard — E2E auth session must have been obtained"
step (#1464 batch T2, G5-13) so the guard's logic can be exercised by
scripts/__tests__/check-e2e-storage-state.test.sh with malformed-shape fixtures. Before this
extraction, the guard was inline Python inside a workflow step and CI only ever fed it the real,
valid storage-state file — none of the malformed shapes it was written to reject had ever
actually been rejected by a test run.

Usage: python3 check-e2e-storage-state.py <path-to-storage-state.json>
Exit 0 and print a summary on success; exit 1 and print "::error::..." on any rejection.

Defect fix (kept from the original inline version): `grep -o '"name"' | wc -l` counts ANY JSON
"name" key anywhere in the file, not just cookies. Playwright storage state is shaped
{"cookies":[...],"origins":[{"origin":"...","localStorage":[{"name":...,"value":...}]}]} — so a
file with "cookies":[] plus a single localStorage entry (which also has a "name" key) passed that
check reporting a non-zero count, i.e. false confidence with zero actual cookies. Parse the
`cookies` array itself instead.

Second defect fix (also kept): a naive `len(data.get('cookies', []))` also passes for
{"cookies":"x"} (len of the STRING "x" is 1), {"cookies":[null]} (len 1, zero real cookies), and
{"cookies":{"k":1}} (len of the dict's keys is 1) — Python's len() doesn't care what type it's
counting. Every shape below is checked explicitly: root must be a JSON object, 'cookies' must be
a JSON array, and each counted entry must be an object with non-empty 'name' and 'value' fields.
"""
import json
import sys


def check(path: str) -> int:
    try:
        with open(path) as f:
            data = json.load(f)
    except (ValueError, OSError) as exc:
        print(f"::error::{path} could not be parsed as JSON: {exc}")
        return 1

    if not isinstance(data, dict):
        print(f"::error::{path}'s root is not a JSON object (got {type(data).__name__}) — malformed Playwright storage state.")
        return 1

    cookies = data.get("cookies")
    if not isinstance(cookies, list):
        print(f"::error::{path}'s 'cookies' key is not a JSON array (got {type(cookies).__name__ if cookies is not None else 'missing'}) — malformed storage state.")
        return 1

    if len(cookies) == 0:
        print(f"::error::{path} has zero cookies — a Clerk session was never actually established.")
        return 1

    valid_cookies = [c for c in cookies if isinstance(c, dict) and c.get("name") and c.get("value")]
    if not valid_cookies:
        print(f"::error::{path}'s cookies array has {len(cookies)} entrie(s), but none are objects with non-empty 'name'/'value' fields — malformed storage state.")
        return 1

    # The actual Clerk session cookie (holds the session JWT). Clerk dev instances suffix it
    # with an instance ID (e.g. "__session_JB79cyGe"); production instances use the bare name.
    # __client_uat/__clerk_db_jwt are also set by Clerk but do not by themselves prove an
    # authenticated session (see apps/web/e2e/auth.setup.ts), so they don't count.
    SESSION_COOKIE = "__session"
    session_cookies = [c for c in valid_cookies if c["name"] == SESSION_COOKIE or c["name"].startswith(SESSION_COOKIE + "_")]
    if not session_cookies:
        names = ", ".join(sorted({c["name"] for c in valid_cookies}))
        print(f"::error::{path} has {len(valid_cookies)} valid cookie(s) but none named '{SESSION_COOKIE}' (or '{SESSION_COOKIE}_<instance>') — the Clerk session cookie is missing, so no real session was established. Cookies present: {names}")
        return 1

    print(f"E2E auth session verified: {len(valid_cookies)} valid cookie(s) in {path}, including Clerk session cookie(s): {', '.join(sorted(c['name'] for c in session_cookies))}")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: check-e2e-storage-state.py <path-to-storage-state.json>", file=sys.stderr)
        sys.exit(2)
    sys.exit(check(sys.argv[1]))
