# Smart Plan

Plan implementing a GitHub issue using structured MCP workflow and thorough analysis.

## Usage

```
/smart-plan [issue-number]
```

Example: `/smart-plan 627`

For Plaid-specific work, add `--plaid` flag:
```
/smart-plan 627 --plaid
```

## MCP Workflow Order (CRITICAL)

Follow this exact order per CLAUDE.md requirements:

### 1. Serena FIRST (Code Search)

Use Serena for ALL code exploration:

```
- Find where X is implemented
- How does Y work?
- What files handle Z?
- Show me examples of...
```

Serena tools:
- `find_symbol` - Find code symbols by name
- `search_for_pattern` - Regex search in codebase
- `get_symbols_overview` - File structure overview
- `find_referencing_symbols` - Find usages

### 2. In-Memoria (Decision Context)

Use for architectural questions and decision tracking:

```
- Why did we decide X?
- What patterns do we use for Y?
- Historical context for Z?
```

Document decisions AS YOU MAKE THEM during planning.

### 3. Sequential Thinking (Complex Analysis)

Use for multi-step reasoning:

```
- Architecture decisions with >3 options
- Complex debugging across >2 systems
- Trade-off analysis
- Design pattern selection
```

### 4. Context7 (Library Knowledge)

Use for third-party library/framework questions:

```
- How does [library] handle X?
- What's the recommended pattern for Y in [framework]?
```

### 5. Playwright MCP (Visual Context)

Use when visual understanding needed:

```
- What does the current UI look like?
- How is the form laid out?
- What's the current state of the page?
```

## Planning Steps

### Step 1: Fetch Issue Details

```bash
gh issue view [issue-number]
```

### Step 2: Explore Codebase (Serena)

- Find relevant files and symbols
- Understand existing patterns
- Identify dependencies

### Step 3: Document Context (In-Memoria)

- Record what you've learned
- Note existing patterns to follow
- Track decisions and reasoning

### Step 4: Analyze Approach (Sequential Thinking)

- Break down into steps
- Consider alternatives
- Evaluate trade-offs

### Step 5: Draft Implementation Plan

Structure:
```markdown
## Issue #N: [Title]

### Context
[What this issue is about]

### Relevant Files
- `path/to/file.go` - [why relevant]

### Implementation Steps
1. [Step with specific details]
2. [Step with specific details]

### Testing Strategy
- Unit tests for...
- Integration tests for...

### Risks/Considerations
- [Potential issues]
```

## Plaid-Specific Planning (--plaid flag)

When working on Plaid integration:

1. Reference plaid-go examples: https://github.com/plaid/plaid-go
2. Check existing Plaid services:
   - `backend/internal/services/plaid_service.go`
   - `backend/internal/handlers/plaid_handler.go`
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

## Time Investment

Spend adequate time planning:
- Simple features: 5-10 minutes
- Medium features: 15-30 minutes
- Complex features: 30-60 minutes

## Related

- `.claude/skills/go-patterns/` - Go implementation patterns
- `.claude/skills/typescript-patterns/` - Frontend patterns
- `.claude/skills/integration-guides/` - Integration details
