# Test Scenarios Implementation Progress

**Started:** 2025-11-05
**Purpose:** Track implementation of configuration-driven E2E test framework for staging environment
**Goal:** Extensible YAML-based scenarios for populating staging with comprehensive test data across ALL app features

**Scope:**
- Transaction sync scenarios (Plaid webhooks, pagination, errors, tier limits)
- Budgeting scenarios (budget creation, spending tracking, alerts, overages)
- Automation rules scenarios (rule execution, triggers, conditions, actions)
- General staging data population (realistic users, accounts, transactions, budgets, rules)
- End-to-end user journeys (onboarding, subscription changes, feature usage)

---

## Implementation Status

### Phase 1: Core Infrastructure ✅ COMPLETE (2025-11-05)
- [x] Created directory structure
- [x] Built `lib/api-client.sh` - Admin API wrapper (445 lines)
- [x] Built `lib/db-utils.sh` - Database utilities (485 lines)
- [x] Built `lib/assertions.sh` - Test assertions (326 lines)
- [x] Created `config/staging.env.example` - Environment template
- [x] Created `README.md` - Usage guide (375 lines)
- [x] Updated `CLAUDE.md` - Framework integration (180 lines)
- [x] Committed Phase 1 (commit: 0ed3eb4)

### Phase 2: Templates & Schema ✅ COMPLETE (2025-11-05)
- [x] Built `lib/fixtures.sh` - Data generators (465 lines)
- [x] Built `lib/reporting.sh` - Report generator (385 lines)
- [x] Created `scenarios/schema.yaml` - YAML schema definition (350 lines)
- [x] Created `templates/transactions.yaml` - Transaction templates (280 lines)
- [x] Created `templates/users.yaml` - User tier templates (140 lines)
- [x] Created `templates/accounts.yaml` - Account type templates (185 lines)
- [x] Created `run-scenario.sh` - Main scenario runner (485 lines)

### Phase 3: Core Scenarios ✅ COMPLETE (2025-11-05)
- [x] `01-free-tier-first-sync.yaml` - Tests free tier 30-day limit
- [x] `02-pagination-multi-page.yaml` - Tests multi-page transaction sync
- [x] `03-preserve-user-notes.yaml` - CRITICAL regression test for data loss
- [x] `04-tier-limit-enforcement.yaml` - Tests monthly sync limits (revenue protection)
- [x] `05-item-error-login-required.yaml` - Tests LOGIN_REQUIRED error handling
- [x] `run-all-scenarios.sh` - Batch runner for sequential/parallel execution

### Phase 4: Future-Ready Templates ✅ COMPLETE (2025-11-05)
- [x] `templates/budgets.yaml` - Budget templates for Phase 3 budgeting features (350+ lines)
- [x] `templates/rules.yaml` - Rule templates for Phase 4 automation features (610+ lines)
- [x] Budget fixture generators added to `lib/fixtures.sh` (4 functions)
- [x] Rule fixture generators added to `lib/fixtures.sh` (5 functions)
- [x] Budget assertions added to `lib/assertions.sh` (5 functions)
- [x] Rule assertions added to `lib/assertions.sh` (7 functions)

### Phase 5: Advanced Scenarios ⏳ FUTURE
- [ ] 06-pending-to-posted-transition.yaml
- [ ] 07-refund-and-adjustment.yaml
- [ ] 08-foreign-currency.yaml
- [ ] 09-multi-account-transfer.yaml
- [ ] 10-historical-backfill.yaml

---

## Design Decisions

### Architecture Choices

**✅ Decision:** Use Bash + YAML instead of Go/TypeScript
**Rationale:**
- Simpler for manual testing workflows
- Easy to run directly on staging server
- No compilation needed
- Familiar toolchain (curl, psql, jq, yq)

**✅ Decision:** YAML for scenarios, not JSON
**Rationale:**
- More human-readable
- Better for version control (comments, multi-line strings)
- Can use templates and anchors for DRY patterns

**✅ Decision:** Separate libraries (api-client, db-utils, assertions)
**Rationale:**
- Modular design allows reuse
- Easy to test individual components
- Clear separation of concerns

### Critical Features

**🔥 MUST-HAVE:**
1. **User Notes Preservation** - Test that prevents data loss bug
2. **Tier Limit Enforcement** - Revenue protection validation
3. **Pagination Handling** - Ensures large datasets work
4. **Error Recovery Flows** - LOGIN_REQUIRED, PENDING_EXPIRATION

**🎯 NICE-TO-HAVE:**
1. Parallel scenario execution
2. Visual diff for failed assertions
3. Automatic retry on transient failures

---

## Knowledge Base

### Admin Webhook Endpoint (Issue #308)
```bash
POST /api/v1/admin/plaid/fire-webhook
Authorization: Bearer <auth0-jwt>
Content-Type: application/json

{
  "item_id": "uuid",
  "webhook_code": "DEFAULT_UPDATE"
}
```

**Supported webhook codes:**
- `INITIAL_UPDATE` - First sync (historical depth based on tier)
- `DEFAULT_UPDATE` - Incremental sync (cursor-based, no limit)
- `HISTORICAL_UPDATE` - Backfill of old transactions
- `TRANSACTIONS_REMOVED` - Retroactive deletions

### Database Tables (Key Fields)

**transactions:**
- `user_id`, `account_id`, `linked_item_id` (FKs)
- `external_transaction_id` (Plaid's transaction_id)
- `amount` (negative=debit, positive=credit)
- `user_notes` (MUST preserve on updates)
- `pending` (bool)

**linked_items:**
- `cursor` (for pagination)
- `status` (active|error|login_required)
- `access_token_encrypted` (AES-256-GCM)

**account_sync_audit:**
- `sync_month` (YYYY-MM format)
- `tier_at_sync` (for limit enforcement)
- `days_historical` (30/365/730 based on tier)

### Transaction Pipeline Flow
```
fire_webhook() → Plaid Sandbox API → Webhook sent to /webhooks/plaid
  ↓
WebhookRepository.Create() → webhook_events table
  ↓
WebhookProcessor (30s poll) → Fetch unprocessed
  ↓
TransactionSyncService.SyncTransactions()
  ├─ Check sync limits (SyncLimitService)
  ├─ Decrypt access token (EncryptionService)
  ├─ Call Plaid TransactionsSync() (pagination loop)
  ├─ Process Added/Modified/Removed
  └─ Update cursor after each page
  ↓
TransactionRepository.CreateBatch() / Update() / Delete()
```

### Tier Limits (Critical)
```yaml
free:
  accounts_synced_per_month: 3
  transaction_history_days: 30

premium:
  accounts_synced_per_month: 30
  transaction_history_days: 730

premium_plus:
  accounts_synced_per_month: -1  # Unlimited
  transaction_history_days: 730
```

---

## Blockers & Resolutions

### Blocker 1: Auth Token for Staging
**Status:** ⏳ Pending
**Issue:** Need Auth0 JWT token with admin role
**Resolution:** TBD - Will document in config/staging.env.example

### Blocker 2: Plaid Sandbox Items
**Status:** ⏳ Pending
**Issue:** Need pre-created Plaid sandbox items for testing
**Resolution:** TBD - May need to create via Plaid Link flow first

---

## Next Steps

**Phase 1 - COMPLETE ✅**
1. ✅ Create directory structure
2. ✅ Build `lib/api-client.sh` with `fire_webhook()` function
3. ✅ Build `lib/db-utils.sh` with `query_db()` and insert helpers
4. ✅ Build `lib/assertions.sh` with test assertion functions
5. ✅ Create README.md and update CLAUDE.md
6. ✅ Commit Phase 1 infrastructure

**Phase 2 - COMPLETE ✅**
1. ✅ Build `lib/fixtures.sh` - Data generators for test data
2. ✅ Build `lib/reporting.sh` - HTML/JSON report generation
3. ✅ Create `scenarios/schema.yaml` - YAML schema definition
4. ✅ Create `templates/transactions.yaml` - Transaction templates
5. ✅ Create `templates/users.yaml` - User tier templates
6. ✅ Create `templates/accounts.yaml` - Account type templates
7. ✅ Create `run-scenario.sh` - Main runner script

**Phase 3 - COMPLETE ✅**
1. ✅ Create `scenarios/01-free-tier-first-sync.yaml`
2. ✅ Create `scenarios/02-pagination-multi-page.yaml`
3. ✅ Create `scenarios/03-preserve-user-notes.yaml`
4. ✅ Create `scenarios/04-tier-limit-enforcement.yaml`
5. ✅ Create `scenarios/05-item-error-login-required.yaml`
6. ✅ Create `run-all-scenarios.sh` batch runner
7. ✅ Commit Phase 3

**Phase 4 - COMPLETE ✅**
1. ✅ Create `templates/budgets.yaml` - Future-ready for Phase 3 budgeting
2. ✅ Create `templates/rules.yaml` - Future-ready for Phase 4 automation
3. ✅ Add budget/rule fixture generators to `lib/fixtures.sh`
4. ✅ Add budget/rule assertions to `lib/assertions.sh`
5. ✅ Create 15 out-of-scope GitHub issues (#316-330)
6. ✅ Update PROGRESS.md with all work completed
7. ✅ Commit Phase 4

**Phase 5 - NEXT 🎯**
1. ⏳ Test all scenarios end-to-end in staging (requires Auth0 token + Plaid sandbox)
2. ⏳ Document any discovered issues
3. ⏳ Create advanced scenarios (06-10) as needed
4. ⏳ Mark PR #315 ready for review

---

## Testing Strategy

### Local Testing (Docker)
```bash
# Start local stack
docker-compose up -d

# Run scenario against local
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml --env local
```

### Staging Testing
```bash
# Set staging credentials
export AUTH_TOKEN="..."
export DATABASE_URL="..."

# Run scenario against staging
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml --env staging
```

---

## Lessons Learned

### Phase 1 & 2 Implementation (2025-11-05)

**What Went Well:**
- Bash + YAML approach provides excellent flexibility without compilation overhead
- Modular library design (api-client, db-utils, assertions, fixtures, reporting) makes code reusable
- YAML templates with anchors (<<: *template) enable DRY patterns
- PROGRESS.md successfully preserves context across work sessions
- Comprehensive documentation in README.md reduces onboarding friction

**Technical Decisions:**
- **yq for YAML parsing**: Required dependency, but worth it for configuration-driven approach
- **Pipe-delimited fixture output**: Simple format for bash processing, easy to parse
- **HTML + JSON reports**: HTML for humans, JSON for automation/CI integration
- **Exported bash functions**: Enables library composition and testing

**Gotchas Discovered:**
- Date command differs between Linux (GNU) and macOS (BSD) - need compatibility checks
- YAML null values need special handling in bash (check for "null" string)
- Cursor values can be NULL in database - need explicit NULL handling in SQL
- User notes preservation is CRITICAL - must test this extensively

**Best Practices Established:**
- Always validate environment (API, DB, dependencies) before executing scenarios
- Use meaningful test IDs (test-user-free-001, not test-123)
- Generate realistic data (merchant names, amounts, dates) for meaningful testing
- Cleanup test data by default (opt-out with --no-cleanup)
- Track all assertions with pass/fail counts for clear reporting

**Future Improvements:**
- Consider parallel scenario execution for faster test runs
- Add retry logic for transient failures
- Implement visual diff for failed assertions
- Create CI/CD integration for automated regression testing

---

## Out-of-Scope Work Identified

During planning, 15 additional testing initiatives were identified as out-of-scope for this framework but valuable for future implementation. GitHub issues created:

### Security Testing (4 issues)
- **Issue #316:** OWASP ZAP Automated Security Scanning (P1, Phase 5+)
- **Issue #317:** Snyk Dependency Vulnerability Scanning (P1, Current phase)
- **Issue #318:** GDPR Compliance Automated Testing (P1, Phase 5+)
- **Issue #319:** Stripe Subscription Webhook Testing (P1, Phase 5+)

### Performance Testing (2 issues)
- **Issue #320:** k6 Load Testing Framework (P1, Phase 5+)
- **Issue #321:** Percy Visual Regression Testing (P2, Phase 5+)

### Chaos Engineering (1 issue)
- **Issue #322:** Chaos Monkey Resilience Testing (P2, Phase 6+)

### Testing Infrastructure (5 issues)
- **Issue #323:** Pact API Contract Testing (P2, Phase 5+)
- **Issue #324:** axe-core Accessibility Testing (P1, Phase 4)
- **Issue #325:** Detox Mobile App E2E Testing (P2, Phase 6)
- **Issue #326:** Database Migration Testing Framework (P1, Current phase)
- **Issue #327:** Multi-Region Consistency Testing (P2, Phase 7+)

### Feature-Specific Testing (3 issues)
- **Issue #328:** AI Model Validation Testing - Premium+ Features (P2, Phase 6)
- **Issue #329:** Budget Alert Notification Testing (P1, Phase 3)
- **Issue #330:** Rule Execution Audit Testing (P1, Phase 4)

**Rationale for Separation:**
These testing initiatives either:
1. Target features not yet implemented (budgets, rules, AI)
2. Require specialized tooling beyond Bash/YAML (k6, Percy, Detox)
3. Focus on different testing dimensions (security, performance, chaos)
4. Belong in CI/CD rather than manual staging population

**Integration Points:**
- This framework (#314) can generate test data used by other testing tools
- Scenarios created here can seed E2E tests, load tests, chaos experiments
- Budget/rule templates prepared here accelerate Phase 3/4 testing implementation

---

## References

### Core Framework Issues
- **Issue #314:** Configuration-Driven E2E Test Scenarios Framework (this work)
- **Issue #308:** Admin API for Manual Webhook Firing (enables webhook testing)

### Related Implementation Issues
- **Issue #242:** Monthly Sync Limits
- **Issue #256:** Transaction Sync Service
- **Issue #257:** Item Error Handler
- **Issue #309:** Role-Based Admin Authentication (future)
- **Issue #310:** Bulk Webhook Firing (future)
- **Issue #311:** Webhook Replay (future)

### Out-of-Scope Testing Issues (see section above)
- Issues #316-330: Security, performance, chaos, infrastructure, feature-specific testing

---

**Last Updated:** 2025-11-05 21:45 UTC (Phases 1-4 complete! 5 core scenarios, 2 template files, 15 GitHub issues created, ready for staging testing)
