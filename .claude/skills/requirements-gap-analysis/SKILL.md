---
name: requirements-gap-analysis
description: Audits whether the last unit of work fully satisfies its GitHub issue, spec, and implementation plan. Use when asked to "analyze requirements", "check if requirements were met", "gap analysis", "were all requirements implemented", "audit against issue", "verify against plan", "identify gaps", or after finishing a feature to confirm nothing was missed. Runs Codex as co-reviewer for independent validation and plans remaining work if gaps exist.
allowed-tools: Read, Bash, Grep, Glob, Agent
---

# Requirements Gap Analysis

Audit a completed unit of work against its GitHub issue, spec, and plan. Identify gaps. Plan remaining implementation if needed.

## Usage

```
/requirements-gap-analysis [issue-number]
```

Example: `/requirements-gap-analysis 1038`

If no issue number is provided, infer it from the current branch name.

## Review Steps

### 1. Gather Context

```bash
# Infer issue number from branch if not provided
git branch --show-current

# See what changed
git diff main...HEAD --stat

# Fetch the issue
gh issue view <N> --json title,body,labels

# Locate plan and spec
ls .claude/plans/ | grep <N>
ls docs/superpowers/specs/
```

Read the plan file and spec file in full.

### 2. Extract Requirements Checklist

From the issue body, plan, and spec, extract every testable requirement:

- GitHub issue: acceptance criteria, behavior matrices, "must"/"shall" statements
- Plan: implementation steps marked required or not deferred
- Spec: behavior tables, architectural constraints

Produce a flat numbered checklist before proceeding.

### 3. Launch Codex Co-Review (codex:codex-rescue)

Launch Codex in **background** while proceeding with Step 4. Give Codex:
- The full requirements checklist
- `git diff main...HEAD --stat` output
- The 3-5 most relevant changed file paths to read

Ask Codex to mark each requirement MET / PARTIAL / NOT MET and flag missing test coverage.

### 4. Verify Each Requirement

For each requirement, check implementation AND test coverage:

| Requirement type | Verification method |
|-----------------|-------------------|
| New file/component | `ls <path>` or Read |
| Behavior change | Grep for key identifiers; Read relevant section |
| Test coverage | Grep for test descriptions matching requirement |
| Backend field/endpoint | Read handler + check `api/openapi.yaml` |
| API client regenerated | Check `packages/api-client/` for new types |
| Analytics event | Grep event constant in implementation files |
| UI CTA/state | Grep for text or component name in TSX files |

Mark each:
- `[x]` MET — implementation + test found
- `[~]` PARTIAL — implementation without test, or test without implementation
- `[ ]` NOT MET — nothing found

### 5. Analyze (Sequential Thinking MCP)

For requirements that are ambiguous or where implementation vs. test disagrees, use `mcp__sequential-thinking__sequentialthinking` to reason through the evidence before marking final status.

### 6. Synthesize with Codex Findings

Wait for Codex result. Compare:
- Both agree gap exists → high confidence gap
- Codex finds gap you marked MET → re-read the specific code, revise if needed
- You found gap Codex missed → include it

Produce the final gap report:

```
## Gap Analysis — Issue #N: <title>

### Met ✓
- <requirement>

### Gaps ✗

#### <Gap Title>
Requirement: <original text>
Status: NOT MET / PARTIAL
Missing: <specific description>
Codex: agrees / disagrees (<note>)

### Summary: N total, N met, N partial, N not met
```

If no gaps: state "All N requirements verified as MET." and stop — do not create a plan.

### 7. Draft Gap Implementation Plan (superpowers:writing-plans)

If gaps exist, invoke `superpowers:writing-plans` to produce a TDD implementation plan covering only the gaps. Each gap task must follow Red-Green-Refactor:

1. **RED** — test file + test description to write first
2. **GREEN** — minimal implementation change
3. **REFACTOR** — cleanup if needed

Flag any gap requiring a design discussion before implementation (new API contract, data model change, external service behavior).

## TDD Reminder

Always plan gap fixes with TDD:

1. **RED**: What failing test proves the gap?
2. **GREEN**: What's the minimal implementation?
3. **REFACTOR**: What cleanup is needed?

## Related Skills

- `smart-plan` — Plan implementing a GitHub issue from scratch
- `superpowers:brainstorming` — Explore requirements before planning
- `superpowers:writing-plans` — Write a detailed implementation plan
- `superpowers:verification-before-completion` — Verify tests pass before claiming complete
- `codex:codex-rescue` — Adversarial second-pass review
