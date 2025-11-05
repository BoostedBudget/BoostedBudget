# Test Scenarios Implementation Progress

**Started:** 2025-11-05
**Purpose:** Track implementation of configuration-driven E2E transaction test scripts
**Goal:** Extensible YAML-based scenarios for populating staging with comprehensive transaction data

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

### Phase 3: Core Scenarios ⏳
- [ ] 01-free-tier-first-sync.yaml
- [ ] 02-pagination-multi-page.yaml
- [ ] 03-preserve-user-notes.yaml
- [ ] 04-tier-limit-enforcement.yaml
- [ ] 05-item-error-login-required.yaml

### Phase 4: Advanced Scenarios ⏳
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

**Phase 3 - NEXT 🎯**
1. ⏳ Create `scenarios/01-free-tier-first-sync.yaml`
2. ⏳ Create `scenarios/02-pagination-multi-page.yaml`
3. ⏳ Create `scenarios/03-preserve-user-notes.yaml`
4. ⏳ Create `scenarios/04-tier-limit-enforcement.yaml`
5. ⏳ Create `scenarios/05-item-error-login-required.yaml`
6. ⏳ Test all scenarios end-to-end
7. ⏳ Commit Phase 3

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

## References

- **Issue #308:** Admin API for Manual Webhook Firing
- **Issue #309:** Role-Based Admin Authentication (future)
- **Issue #310:** Bulk Webhook Firing (future)
- **Issue #311:** Webhook Replay (future)
- **Issue #242:** Monthly Sync Limits
- **Issue #256:** Transaction Sync Service
- **Issue #257:** Item Error Handler

---

**Last Updated:** 2025-11-05 19:30 UTC (Phase 1 & 2 complete, ready for Phase 3)
