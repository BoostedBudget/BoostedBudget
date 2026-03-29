---
description: Plan implementing a GitHub issue using structured workflow
---

# Smart Plan

Plan implementing a GitHub issue using structured workflow.

## Usage

```
/smart-plan [issue-number]
```

Example: `/smart-plan 627`

For Plaid-specific work, add `--plaid` flag:
```
/smart-plan 627 --plaid
```

## Planning Steps

### 1. Fetch Issue
```bash
gh issue view [issue-number]
```

### 2. Brainstorm (superpowers:brainstorming)
Explore intent, clarify requirements, identify unknowns.

### 3. Explore Codebase
Use Explore agents + Grep/Glob:
- Find relevant files and symbols
- Understand existing patterns
- Identify dependencies and reusable code

### 4. Analyze (Sequential Thinking MCP)
Multi-step reasoning for:
- Architecture decisions with >3 options
- Trade-off analysis
- Design pattern selection

### 5. Library Docs (Context7 MCP)
Third-party library/framework questions.

### 6. Visual Context (Playwright MCP)
When UI understanding needed.

### 7. Draft Plan (superpowers:writing-plans)
Structure:
```markdown
## Issue #N: [Title]

### Context
[What this issue is about and why it matters]

### Relevant Files
- `path/to/file.go` - [why relevant]

### Implementation Steps
1. [Specific step with TDD approach]
2. [Specific step with TDD approach]

### Testing Strategy
- Unit tests for...
- Integration tests for...
- E2E tests for...

### Risks/Considerations
- [Potential issues to watch for]
```

## Plaid-Specific Planning (--plaid flag)

When working on Plaid integration:

1. Reference plaid-go examples: https://github.com/plaid/plaid-go
2. Check existing Plaid services:
   - `backend/internal/services/plaid_service.go`
   - `backend/internal/huma/operations/plaid.go`
   - `backend/internal/huma/operations/plaid_webhook.go`
3. Understand webhook flow:
   - `backend/internal/workers/webhook_processor.go`
4. Review transaction sync:
   - `backend/internal/services/transaction_sync_service.go`

**Webhook Codes:**
- `INITIAL_UPDATE` - First sync (historical depth by tier)
- `DEFAULT_UPDATE` - Incremental sync
- `HISTORICAL_UPDATE` - Backfill
- `TRANSACTIONS_REMOVED` - Retroactive deletions

## TDD Reminder

Always plan with TDD approach:

1. **RED**: What tests will you write first?
2. **GREEN**: What's the minimal implementation?
3. **REFACTOR**: What cleanup is needed?

## Related Skills

- `go-patterns` - Go implementation patterns
- `typescript-patterns` - Frontend patterns
- `integration-guides` - Integration details
- `superpowers:test-driven-development` - TDD workflow
