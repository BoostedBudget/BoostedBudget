#!/usr/bin/env python3
"""
Cloudflare infrastructure setup for the BoostedBudget MCP server.
Uses the Cloudflare Rulesets API (same patterns as issue #1192).

Usage:
    CF_TOKEN=<your-cf-token> ORIGIN_SECRET=<your-origin-secret> python3 scripts/setup-cloudflare.py

The CF token needs:
    Zone > Transform Rules > Edit

Zone ID: 237fcc86cbd5635cbcacb51cfe5122a6 (boostedbudget.com)

See issue #1192 for the full Cloudflare API reference.
"""

import json
import os
import sys
import urllib.request
import urllib.error

ZONE_ID = "237fcc86cbd5635cbcacb51cfe5122a6"

CF_TOKEN = os.environ.get("CF_TOKEN", "")
ORIGIN_SECRET = os.environ.get("ORIGIN_SECRET", "")

if not CF_TOKEN:
    print("❌ CF_TOKEN environment variable not set.")
    print("   Get a token at: dash.cloudflare.com/profile/api-tokens")
    print("   Required permission: Zone > Transform Rules > Edit")
    sys.exit(1)

if not ORIGIN_SECRET:
    print("❌ ORIGIN_SECRET environment variable not set.")
    print("   This is the same value as BOOSTEDBUDGET_ORIGIN_SECRET in your backend.")
    sys.exit(1)


def cf(method, path, data=None):
    url = f"https://api.cloudflare.com/client/v4{path}"
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(
        url,
        data=body,
        method=method,
        headers={
            "Authorization": f"Bearer {CF_TOKEN}",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        return json.loads(e.read().decode())


# ── Step 1: GET existing Transform Rules ─────────────────────────────────────

print("Fetching existing Transform Rules...")
r = cf("GET", f"/zones/{ZONE_ID}/rulesets/phases/http_request_late_transform/entrypoint")

if not r.get("success"):
    errs = r.get("errors", [])
    # 7003 = phase not yet initialized (no rules) — safe to treat as empty
    if any(e.get("code") == 7003 for e in errs):
        existing_rules = []
        print("  ℹ️  No existing Transform Rules (phase not yet initialized)")
    else:
        print(f"❌ Failed to GET existing rules: {errs}")
        sys.exit(1)
else:
    existing_rules = r.get("result", {}).get("rules", [])
    print(f"  Found {len(existing_rules)} existing rule(s):")
    for rule in existing_rules:
        status = "ON " if rule.get("enabled") else "OFF"
        print(f"    [{status}] {rule.get('description', '(no desc)')} — {rule['id'][:8]}...")


# ── Step 2: Check if the MCP Worker rule already exists ──────────────────────

MCP_RULE_DESCRIPTION = "Inject Origin Secret — MCP Worker subrequests"

already_exists = any(
    MCP_RULE_DESCRIPTION in rule.get("description", "")
    or 'cf.worker.upstream_zone' in rule.get("expression", "")
    for rule in existing_rules
)

if already_exists:
    print(f"\n✅ MCP Worker subrequest rule already exists — nothing to do.")
    sys.exit(0)


# ── Step 3: Append the new rule and PUT the full updated ruleset ─────────────

new_rule = {
    "action": "rewrite",
    "action_parameters": {
        "headers": {
            "X-Origin-Secret": {"operation": "set", "value": ORIGIN_SECRET}
        }
    },
    "expression": '(cf.worker.upstream_zone != "")',
    "description": MCP_RULE_DESCRIPTION,
    "enabled": True,
}

updated_rules = existing_rules + [new_rule]

print(f"\nAdding MCP Worker subrequest rule...")
r = cf(
    "PUT",
    f"/zones/{ZONE_ID}/rulesets/phases/http_request_late_transform/entrypoint",
    {"rules": updated_rules},
)

if r.get("success"):
    new_rules = r.get("result", {}).get("rules", [])
    print(f"✅ Transform Rules updated — {len(new_rules)} rule(s) now active:")
    for rule in new_rules:
        status = "ON " if rule.get("enabled") else "OFF"
        print(f"  [{status}] {rule.get('description', '(no desc)')} — {rule['id'][:8]}...")
else:
    print(f"❌ Failed to update Transform Rules: {r.get('errors')}")
    sys.exit(1)

print("\n✅ Cloudflare setup complete!")
print("   The MCP Worker's subrequests to api.boostedbudget.com will now")
print("   have X-Origin-Secret injected by Cloudflare as belt-and-suspenders.")
print("   (The Worker also injects it from its own BOOSTEDBUDGET_ORIGIN_SECRET secret.)")
