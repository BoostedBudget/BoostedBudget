---
description: Review GitHub issues and recommend which one to work on next using structured prioritization
---

# Plan Next Task

Review GitHub issues and recommend which one to work on next using structured prioritization.

## Usage

```
/plan-next-task
```

## Workflow

### 1. Fetch Open Issues

```bash
gh issue list --state open --limit 50 --json number,title,labels,milestone,assignees
```

### 2. Apply Prioritization Framework

#### Priority Labels (Highest to Lowest)

| Label | Description | Action |
|-------|-------------|--------|
| `P0` | Critical/blocker | Work on immediately |
| `P1` | High priority | Work on next |
| `P2` | Medium priority | Plan for soon |
| `P3` | Low priority | Backlog |

#### Phase Labels

Issues are organized by project phase:
- `phase-0` - Foundation/infrastructure
- `phase-1` - Core features (MVP)
- `phase-2` - Enhanced features
- `phase-3+` - Future features

**Prioritize lower phases first** (phase-0 before phase-1).

#### Status Labels

| Label | Meaning |
|-------|---------|
| `blocked` | Cannot proceed, needs something else |
| `in-progress` | Already being worked on |
| `needs-review` | Waiting for code review |
| `ready` | Ready to start |

**Skip blocked and in-progress issues.**

#### Size Labels

| Label | Effort |
|-------|--------|
| `small` | < 2 hours |
| `medium` | 2-8 hours |
| `large` | 1-3 days |

Consider current time availability when selecting.

### 3. Check Dependencies

For each candidate issue:
```bash
gh issue view [number] --json body
```

Look for:
- "Depends on #X"
- "Blocked by #X"
- "After #X is merged"

Skip issues with unresolved dependencies.

### 4. Consider Context

- **Recently worked on**: Related issues may be faster
- **Skill match**: Backend vs frontend vs full-stack
- **Test coverage**: Issues improving test coverage are valuable

### 5. Generate Recommendation

Format:
```markdown
## Recommended Next Task

### Issue #[number]: [title]

**Priority**: P[0-3]
**Phase**: phase-[N]
**Size**: [small/medium/large]
**Type**: [feature/bug/enhancement/refactor]

### Why This Issue

1. [Reason based on priority]
2. [Reason based on dependencies]
3. [Reason based on context]

### Prerequisites

- [x] No blocking issues
- [x] Dependencies resolved
- [x] Required context available

### Alternative Options

If the above doesn't fit your current focus:

1. **#[number]**: [title] - [brief reason]
2. **#[number]**: [title] - [brief reason]
```

## Example Prioritization

```
Issues sorted by priority:

HIGH PRIORITY (work on these):
- #123 [P0, phase-0, small] Fix authentication crash
- #456 [P1, phase-1, medium] Add transaction filtering

MEDIUM PRIORITY (plan for soon):
- #789 [P2, phase-1, large] Implement budget categories

LOW PRIORITY (backlog):
- #101 [P3, phase-2, small] Add dark mode toggle

BLOCKED (skip):
- #102 [blocked] Waiting on #456
- #103 [in-progress] Being worked on
```

## Quick Commands

```bash
# List P0/P1 issues
gh issue list --label "P0" --label "P1" --state open

# List phase-0 issues
gh issue list --label "phase-0" --state open

# List unassigned ready issues
gh issue list --state open --json number,title,labels,assignees | \
  jq '[.[] | select(.assignees | length == 0)]'
```

## Related

- `.claude/skills/git-workflow/` - Branch and PR conventions
- `.claude/commands/smart-plan.md` - Plan implementation for selected issue
