# How the Test Scenarios Framework Works

**Last Updated:** 2025-11-06
**Framework Version:** 1.0
**Related Issue:** #314 - Configuration-Driven E2E Test Scenarios Framework

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Data Flow](#data-flow)
4. [Components](#components)
5. [Execution Process](#execution-process)
6. [Real-World Example](#real-world-example)
7. [Common Use Cases](#common-use-cases)
8. [FAQ](#faq)

---

## Overview

### What Is This Framework?

The Test Scenarios Framework is a **configuration-driven system** for populating the staging environment with comprehensive test data and validating transaction sync functionality. It's designed for **manual testing** and **staging environment population**, not automated CI/CD testing (that's handled by Issue #331).

### Key Characteristics

- **Manual Execution** - Run by developers, QA, or ops when needed
- **Staging Target** - Designed for staging environment (Mac Mini), not production
- **Bash + YAML** - Simple, portable, no compilation required
- **Admin API Driven** - Uses `POST /api/v1/admin/plaid/fire-webhook` endpoint
- **Database Direct Access** - Reads/writes PostgreSQL directly for setup and validation

### When to Use This Framework

✅ **Use it for:**
- Populating staging with realistic transaction data
- Testing transaction sync scenarios before production deployment
- Validating bug fixes (e.g., user notes preservation)
- Regression testing critical business logic (tier limits, pagination)
- Creating reproducible test cases for QA

❌ **Don't use it for:**
- Automated CI/CD testing (use Issue #331 E2E framework instead)
- Production environment testing (use monitoring/observability)
- Unit testing (use Go/TypeScript tests)
- Load testing (use Issue #320 k6 framework)

---

## Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     STAGING ENVIRONMENT                         │
│                                                                 │
│  ┌──────────────┐                    ┌──────────────┐          │
│  │   Frontend   │◄───────────────────│   Backend    │          │
│  │  (React App) │                    │  (Go Server) │          │
│  └──────────────┘                    └───────┬──────┘          │
│                                              │                  │
│                                              │                  │
│                       ┌──────────────────────┼─────────┐        │
│                       │                      │         │        │
│                       ▼                      ▼         ▼        │
│              ┌─────────────┐       ┌──────────────┐  ┌────┐    │
│              │ PostgreSQL  │       │ Plaid Sandbox│  │Redis│   │
│              │  Database   │       │     API      │  │    │    │
│              └─────────────┘       └──────────────┘  └────┘    │
│                       ▲                      ▲                  │
└───────────────────────┼──────────────────────┼──────────────────┘
                        │                      │
                        │                      │
        ┌───────────────┴──────────────────────┴───────────┐
        │   TEST SCENARIOS FRAMEWORK (Bash Scripts)        │
        │                                                   │
        │  ┌──────────────┐         ┌──────────────┐       │
        │  │ YAML Scenario│────────►│run-scenario.sh│      │
        │  │    Files     │         │   (Runner)   │       │
        │  └──────────────┘         └───────┬──────┘       │
        │                                   │               │
        │                    ┌──────────────┼───────────┐   │
        │                    │              │           │   │
        │                    ▼              ▼           ▼   │
        │            ┌─────────────┐  ┌─────────┐  ┌──────┐│
        │            │ api-client  │  │db-utils │  │assert││
        │            │    .sh      │  │  .sh    │  │ions  ││
        │            └─────────────┘  └─────────┘  └──────┘│
        │                    │              │           │   │
        │                    └──────────────┴───────────┘   │
        └───────────────────────────────────────────────────┘
                           (Developer's Machine)
```

### Component Overview

| Component | Type | Purpose | Lines of Code |
|-----------|------|---------|---------------|
| **YAML Scenarios** | Configuration | Define test cases declaratively | ~200 lines each |
| **run-scenario.sh** | Bash Script | Main scenario executor | 485 lines |
| **run-all-scenarios.sh** | Bash Script | Batch runner (sequential/parallel) | 314 lines |
| **lib/api-client.sh** | Bash Library | Admin API wrapper | 445 lines |
| **lib/db-utils.sh** | Bash Library | PostgreSQL utilities | 485 lines |
| **lib/assertions.sh** | Bash Library | Test assertion helpers | 326 lines |
| **lib/fixtures.sh** | Bash Library | Data generators | 465 lines |
| **lib/reporting.sh** | Bash Library | Report generation | 385 lines |
| **Templates** | YAML | Reusable test data patterns | 1,400+ lines total |

---

## Data Flow

### Complete Execution Flow

```
1. Developer creates YAML scenario
   └─> 01-free-tier-first-sync.yaml

2. Developer sources environment
   └─> source config/staging.env
       ├─> API_BASE_URL=https://staging.boostedbudget.com
       ├─> AUTH_TOKEN=eyJ0eXAiOiJKV1QiLCJ... (Auth0 JWT)
       └─> DATABASE_URL=postgres://user:pass@staging-db:5432/boostedbudget

3. Developer runs scenario
   └─> ./run-scenario.sh scenarios/01-free-tier-first-sync.yaml

4. run-scenario.sh parses YAML (using yq)
   └─> Extracts: scenario.setup, scenario.steps, scenario.assertions

5. SETUP PHASE: Seed database with test data
   ├─> Call insert_user() from db-utils.sh
   │   └─> INSERT INTO users (id, tier, email, ...) VALUES (...)
   ├─> Call insert_linked_item() from db-utils.sh
   │   └─> INSERT INTO linked_items (id, user_id, item_id, ...) VALUES (...)
   ├─> Call insert_account() from db-utils.sh
   │   └─> INSERT INTO accounts (id, linked_item_id, ...) VALUES (...)
   └─> Database now has: 1 user, 1 linked_item, 1 account

6. EXECUTION PHASE: Run scenario steps
   ├─> Step 1: fire_webhook
   │   └─> Call fire_webhook() from api-client.sh
   │       └─> curl -X POST $API_BASE_URL/api/v1/admin/plaid/fire-webhook \
   │           -H "Authorization: Bearer $AUTH_TOKEN" \
   │           -d '{"item_id":"plaid-item-001","webhook_code":"INITIAL_UPDATE"}'
   │       └─> Backend receives webhook request
   │           └─> WebhookRepository.Create() inserts webhook_events record
   │           └─> Returns HTTP 200 {"webhook_id": "..."}
   │
   ├─> Step 2: wait (5 seconds)
   │   └─> sleep 5
   │       └─> During this time:
   │           ├─> WebhookProcessor (backend) polls unprocessed webhooks (30s interval)
   │           ├─> Finds new webhook event
   │           └─> TransactionSyncService.SyncTransactions() executes
   │               ├─> Check sync limits (SyncLimitService)
   │               ├─> Decrypt access token (EncryptionService)
   │               ├─> Call Plaid TransactionsSync() API
   │               ├─> Plaid Sandbox returns transactions
   │               ├─> Process Added/Modified/Removed transactions
   │               ├─> TransactionRepository.CreateBatch() inserts transactions
   │               ├─> Update cursor in linked_items
   │               └─> Create account_sync_audit record
   │
   └─> Webhook processing complete

7. ASSERTION PHASE: Validate results
   ├─> Database assertions
   │   ├─> assert_transaction_count "test-user-free-001" ">= 1"
   │   │   └─> SELECT COUNT(*) FROM transactions WHERE user_id = '...'
   │   │   └─> Compare actual vs expected
   │   │   └─> ✅ PASS (found 23 transactions)
   │   │
   │   ├─> assert_sync_audit_count "test-user-free-001" 1
   │   │   └─> SELECT COUNT(*) FROM account_sync_audit WHERE user_id = '...'
   │   │   └─> ✅ PASS (found 1 audit record)
   │   │
   │   └─> Custom query: Check 30-day limit
   │       └─> SELECT COUNT(*) FROM transactions
   │           WHERE date < (CURRENT_DATE - INTERVAL '30 days')
   │       └─> ✅ PASS (found 0 old transactions)
   │
   ├─> Webhook status assertions
   │   ├─> assert_webhook_processed "plaid-item-001"
   │   │   └─> SELECT processed FROM webhook_events WHERE item_id = '...'
   │   │   └─> ✅ PASS (processed = true)
   │   │
   │   └─> Check no errors
   │       └─> SELECT error_message FROM webhook_events WHERE item_id = '...'
   │       └─> ✅ PASS (error_message IS NULL)
   │
   └─> Print assertion summary:
       ✅ 5/5 assertions passed (100%)

8. REPORTING PHASE: Generate reports
   ├─> generate_html_report()
   │   └─> Creates reports/scenario-01-20251106-143052.html
   │       ├─> Scenario metadata
   │       ├─> Setup data summary
   │       ├─> Step execution timeline
   │       ├─> Assertion results (pass/fail)
   │       └─> Database snapshots
   │
   └─> generate_json_report()
       └─> Creates reports/scenario-01-20251106-143052.json
           └─> Machine-readable format for automation

9. CLEANUP PHASE: Remove test data (optional)
   └─> cleanup_test_user "test-user-free-001"
       └─> DELETE FROM users WHERE id = '...'
           └─> CASCADE deletes:
               ├─> transactions (FK constraint)
               ├─> accounts (FK constraint)
               ├─> linked_items (FK constraint)
               └─> account_sync_audit (FK constraint)

10. EXIT: Return status code
    └─> exit 0 (success) or exit 1 (failure)
```

---

## Components

### 1. YAML Scenario Files

**Purpose:** Declaratively define test cases without writing code

**Location:** `scenarios/*.yaml`

**Structure:**
```yaml
version: "1.0"

scenario:
  # Metadata
  name: "Scenario Name"
  description: "What this tests"
  category: "normal|edge_case|error|tier_limit|special"
  tags: ["tag1", "tag2"]

  # Setup: Database seeding
  setup:
    user: {...}
    linked_items: [...]
    accounts: [...]
    transactions: [...]

  # Execution: Steps to perform
  steps:
    - action: "fire_webhook"
      webhook_code: "INITIAL_UPDATE"
      item_id: "..."
    - action: "wait"
      wait_seconds: 5

  # Validation: What to check
  assertions:
    database:
      transactions_count: ">= 1"
      sync_audits_count: 1
    webhook_status:
      processed: true
      error: null
    custom:
      - description: "..."
        query: "SELECT ..."
        expected: {...}
```

**Real Example:**
```yaml
# scenarios/01-free-tier-first-sync.yaml
version: "1.0"

scenario:
  name: "Free Tier First Sync"
  description: "Verify free tier 30-day historical limit on initial sync"
  category: "tier_limit"
  tags: ["free-tier", "first-sync", "regression"]

  setup:
    user:
      id: "test-user-free-001"
      tier: "free"
      email: "test-free@example.com"
      auth0_user_id: "auth0|test-free-001"

    linked_items:
      - id: "test-item-free-001"
        user_id: "test-user-free-001"
        item_id: "plaid-item-free-sandbox-001"
        institution_name: "Chase"
        institution_id: "ins_3"
        cursor: null  # First sync
        status: "active"

    accounts:
      - id: "test-acc-free-001"
        linked_item_id: "test-item-free-001"
        user_id: "test-user-free-001"
        plaid_account_id: "plaid-checking-001"
        name: "Chase Everyday Checking"
        type: "depository"
        subtype: "checking"
        current_balance: 1250.00

  steps:
    - action: "fire_webhook"
      description: "Trigger initial sync"
      webhook_code: "INITIAL_UPDATE"
      item_id: "plaid-item-free-sandbox-001"

    - action: "wait"
      description: "Wait for async processing"
      wait_seconds: 10

  assertions:
    database:
      transactions_count: ">= 1"
      sync_audits_count: 1

    webhook_status:
      processed: true
      error: null

    custom:
      - description: "Only transactions within 30 days should be synced"
        query: |
          SELECT COUNT(*) as count
          FROM transactions
          WHERE user_id = 'test-user-free-001'
          AND date < (CURRENT_DATE - INTERVAL '30 days')
        expected:
          count: 0
```

---

### 2. run-scenario.sh (Main Runner)

**Purpose:** Execute a single YAML scenario from start to finish

**Key Functions:**

```bash
# Parse YAML file
parse_scenario() {
    local scenario_file="$1"

    # Extract metadata
    SCENARIO_NAME=$(yq eval '.scenario.name' "$scenario_file")
    SCENARIO_CATEGORY=$(yq eval '.scenario.category' "$scenario_file")

    # Extract setup
    SETUP_USER=$(yq eval '.scenario.setup.user' "$scenario_file" -o json)
    SETUP_LINKED_ITEMS=$(yq eval '.scenario.setup.linked_items' "$scenario_file" -o json)
    # ... etc
}

# Seed database with test data
run_setup() {
    log_info "Setting up test data..."

    # Create user
    local user_id=$(echo "$SETUP_USER" | jq -r '.id')
    local tier=$(echo "$SETUP_USER" | jq -r '.tier')
    insert_user "$user_id" "$tier" "$(echo "$SETUP_USER" | jq -r '.email')"

    # Create linked items
    # Create accounts
    # Create transactions (if specified)
}

# Execute scenario steps
run_steps() {
    local step_count=$(yq eval '.scenario.steps | length' "$SCENARIO_FILE")

    for ((i=0; i<step_count; i++)); do
        local action=$(yq eval ".scenario.steps[$i].action" "$SCENARIO_FILE")

        case "$action" in
            fire_webhook)
                local webhook_code=$(yq eval ".scenario.steps[$i].webhook_code" "$SCENARIO_FILE")
                local item_id=$(yq eval ".scenario.steps[$i].item_id" "$SCENARIO_FILE")
                fire_webhook "$item_id" "$webhook_code"
                ;;
            wait)
                local seconds=$(yq eval ".scenario.steps[$i].wait_seconds" "$SCENARIO_FILE")
                sleep "$seconds"
                ;;
            *)
                log_error "Unknown action: $action"
                return 1
                ;;
        esac
    done
}

# Run all assertions
run_assertions() {
    log_info "Running assertions..."

    # Database assertions
    local txn_count=$(get_transaction_count "$USER_ID")
    assert_gt "$txn_count" 0 "Should have synced transactions"

    # Webhook status assertions
    assert_webhook_processed "$ITEM_ID"

    # Custom SQL assertions
    # ...

    print_assertion_summary
}

# Main execution
main() {
    parse_scenario "$SCENARIO_FILE"
    validate_environment
    run_setup
    run_steps
    run_assertions
    generate_reports
    cleanup
}
```

**Execution:**
```bash
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml
```

**Output:**
```
[INFO] Loading scenario: scenarios/01-free-tier-first-sync.yaml
[INFO] Scenario: Free Tier First Sync
[INFO] Category: tier_limit
[INFO]
[INFO] ======================================
[INFO] SETUP PHASE
[INFO] ======================================
[INFO] Creating user: test-user-free-001 (tier: free)
[INFO] Creating linked item: test-item-free-001 (Chase)
[INFO] Creating account: test-acc-free-001 (checking)
[INFO] Setup complete (3 entities created)
[INFO]
[INFO] ======================================
[INFO] EXECUTION PHASE
[INFO] ======================================
[INFO] Step 1: fire_webhook
[INFO]   Firing INITIAL_UPDATE webhook for plaid-item-free-sandbox-001
[INFO]   Response: HTTP 200 {"webhook_id":"wb-001","status":"queued"}
[INFO] Step 2: wait
[INFO]   Waiting 10 seconds for processing...
[INFO]
[INFO] ======================================
[INFO] ASSERTION PHASE
[INFO] ======================================
[INFO] Running database assertions...
[PASS] ✓ transactions_count: 23 (expected >= 1)
[PASS] ✓ sync_audits_count: 1 (expected = 1)
[INFO] Running webhook status assertions...
[PASS] ✓ webhook_processed: true
[PASS] ✓ webhook_error: null
[INFO] Running custom assertions...
[PASS] ✓ Only transactions within 30 days: count=0 (expected = 0)
[INFO]
[INFO] ======================================
[INFO] RESULTS
[INFO] ======================================
[INFO] Assertions: 5/5 passed (100%)
[INFO] Duration: 15.2 seconds
[INFO] Report: reports/scenario-01-20251106-143052.html
[INFO]
[INFO] ✅ Scenario PASSED
```

---

### 3. lib/api-client.sh (Admin API Wrapper)

**Purpose:** Interact with staging backend's admin API

**Key Functions:**

```bash
# Fire a Plaid webhook
fire_webhook() {
    local item_id="$1"
    local webhook_code="$2"

    local response=$(curl -s -X POST \
        "$API_BASE_URL/api/v1/admin/plaid/fire-webhook" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"item_id\":\"$item_id\",\"webhook_code\":\"$webhook_code\"}")

    local status=$(echo "$response" | jq -r '.status')

    if [[ "$status" != "queued" ]]; then
        log_error "Webhook firing failed: $response"
        return 1
    fi

    log_success "Webhook fired: $webhook_code for $item_id"
}

# Wait for webhook to be processed
wait_for_webhook_processed() {
    local item_id="$1"
    local timeout="${2:-30}"
    local elapsed=0

    while [[ $elapsed -lt $timeout ]]; do
        local processed=$(query_db "SELECT processed FROM webhook_events WHERE item_id = '$item_id' ORDER BY created_at DESC LIMIT 1" | tail -1)

        if [[ "$processed" == "t" ]]; then
            log_success "Webhook processed"
            return 0
        fi

        sleep 2
        ((elapsed+=2))
    done

    log_error "Timeout waiting for webhook processing"
    return 1
}

# Test auth token validity
test_auth_token() {
    local response=$(curl -s -w "\n%{http_code}" \
        "$API_BASE_URL/api/v1/health" \
        -H "Authorization: Bearer $AUTH_TOKEN")

    local http_code=$(echo "$response" | tail -1)

    if [[ "$http_code" == "200" ]]; then
        log_success "Auth token valid"
        return 0
    else
        log_error "Auth token invalid (HTTP $http_code)"
        return 1
    fi
}
```

**Usage:**
```bash
source lib/api-client.sh

# Fire webhook
fire_webhook "plaid-item-001" "INITIAL_UPDATE"

# Wait for processing
wait_for_webhook_processed "plaid-item-001" 30

# Check auth
test_auth_token
```

---

### 4. lib/db-utils.sh (Database Utilities)

**Purpose:** Direct PostgreSQL database operations

**Key Functions:**

```bash
# Execute SQL query
query_db() {
    local query="$1"
    psql "$DATABASE_URL" -t -A -c "$query" 2>/dev/null
}

# Insert user
insert_user() {
    local id="$1"
    local tier="$2"
    local email="$3"
    local auth0_id="${4:-auth0|$id}"

    query_db "
        INSERT INTO users (id, tier, email, auth0_user_id, subscription_status, created_at)
        VALUES ('$id', '$tier', '$email', '$auth0_id', 'active', NOW())
        ON CONFLICT (id) DO NOTHING
    "

    log_info "Created user: $id ($tier)"
}

# Insert linked item
insert_linked_item() {
    local id="$1"
    local user_id="$2"
    local item_id="$3"
    local institution="$4"
    local cursor="${5:-null}"

    query_db "
        INSERT INTO linked_items (id, user_id, item_id, institution_name, cursor, status, created_at)
        VALUES ('$id', '$user_id', '$item_id', '$institution', $([ "$cursor" == "null" ] && echo "NULL" || echo "'$cursor'"), 'active', NOW())
        ON CONFLICT (id) DO NOTHING
    "

    log_info "Created linked item: $id ($institution)"
}

# Get transaction count
get_transaction_count() {
    local user_id="$1"
    query_db "SELECT COUNT(*) FROM transactions WHERE user_id = '$user_id'" | tail -1
}

# Cleanup test user (cascade deletes)
cleanup_test_user() {
    local user_id="$1"

    query_db "DELETE FROM users WHERE id = '$user_id'"

    log_info "Cleaned up user: $user_id (cascaded to transactions, accounts, linked_items)"
}
```

**Usage:**
```bash
source lib/db-utils.sh

# Create test data
insert_user "test-user-001" "free" "test@example.com"
insert_linked_item "test-item-001" "test-user-001" "plaid-item-001" "Chase"

# Query data
count=$(get_transaction_count "test-user-001")
echo "Transaction count: $count"

# Cleanup
cleanup_test_user "test-user-001"
```

---

### 5. lib/assertions.sh (Test Assertions)

**Purpose:** Validate test results and track pass/fail

**Key Functions:**

```bash
# Global counters
ASSERTIONS_PASSED=0
ASSERTIONS_FAILED=0

# Basic equality assertion
assert_equal() {
    local actual="$1"
    local expected="$2"
    local message="$3"

    if [[ "$actual" == "$expected" ]]; then
        log_pass "✓ $message: $actual"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_fail "✗ $message: expected '$expected', got '$actual'"
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Greater than assertion
assert_gt() {
    local actual="$1"
    local threshold="$2"
    local message="$3"

    if [[ "$actual" -gt "$threshold" ]]; then
        log_pass "✓ $message: $actual > $threshold"
        ((ASSERTIONS_PASSED++))
        return 0
    else
        log_fail "✗ $message: $actual not greater than $threshold"
        ((ASSERTIONS_FAILED++))
        return 1
    fi
}

# Domain-specific: Assert webhook processed
assert_webhook_processed() {
    local item_id="$1"

    local processed=$(query_db "
        SELECT processed
        FROM webhook_events
        WHERE item_id = '$item_id'
        ORDER BY created_at DESC
        LIMIT 1
    " | tail -1)

    assert_equal "$processed" "t" "Webhook should be processed"
}

# Domain-specific: Assert user notes preserved
assert_user_notes_preserved() {
    local txn_id="$1"
    local user_id="$2"
    local expected_notes="$3"

    local actual_notes=$(query_db "
        SELECT user_notes
        FROM transactions
        WHERE id = '$txn_id'
        AND user_id = '$user_id'
    " | tail -1)

    assert_equal "$actual_notes" "$expected_notes" "User notes should be preserved"
}

# Print summary
print_assertion_summary() {
    local total=$((ASSERTIONS_PASSED + ASSERTIONS_FAILED))
    local percentage=$(awk "BEGIN {printf \"%.1f\", ($ASSERTIONS_PASSED / $total) * 100}")

    echo ""
    echo "========================================"
    echo "ASSERTION SUMMARY"
    echo "========================================"
    echo "Passed:  $ASSERTIONS_PASSED"
    echo "Failed:  $ASSERTIONS_FAILED"
    echo "Total:   $total"
    echo "Success: ${percentage}%"
    echo "========================================"

    if [[ $ASSERTIONS_FAILED -gt 0 ]]; then
        return 1
    else
        return 0
    fi
}
```

**Usage:**
```bash
source lib/assertions.sh

# Basic assertions
assert_equal "foo" "foo" "Values should match"
assert_gt 10 5 "Count should exceed threshold"

# Domain assertions
assert_webhook_processed "plaid-item-001"
assert_user_notes_preserved "txn-001" "user-001" "My important note"

# Print results
print_assertion_summary
```

---

## Execution Process

### Step-by-Step Walkthrough

Let's walk through executing scenario `01-free-tier-first-sync.yaml`:

#### **Step 1: Prerequisites**

```bash
# Ensure tools installed
which yq jq psql curl
# All should return paths

# Setup staging credentials
cd backend/scripts/test-scenarios
cp config/staging.env.example config/staging.env
vim config/staging.env
```

Edit `config/staging.env`:
```bash
# Staging API
API_BASE_URL="https://staging.boostedbudget.com"

# Auth0 JWT token (get from staging Auth0 tenant)
AUTH_TOKEN="eyJ0eXAiOiJKV1QiLCJhbGc..."

# Staging database
DATABASE_URL="postgres://dbuser:dbpass@staging-db.internal:5432/boostedbudget"
```

#### **Step 2: Source Environment**

```bash
source config/staging.env

# Verify variables loaded
echo $API_BASE_URL
# Output: https://staging.boostedbudget.com
```

#### **Step 3: Run Scenario**

```bash
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml
```

#### **Step 4: Watch Execution** (annotated output)

```
[2025-11-06 14:30:52] [INFO] ========================================
[2025-11-06 14:30:52] [INFO] SCENARIO: Free Tier First Sync
[2025-11-06 14:30:52] [INFO] ========================================
[2025-11-06 14:30:52] [INFO]
[2025-11-06 14:30:52] [INFO] Validating environment...
[2025-11-06 14:30:52] [INFO] ✓ API_BASE_URL set
[2025-11-06 14:30:52] [INFO] ✓ AUTH_TOKEN set
[2025-11-06 14:30:52] [INFO] ✓ DATABASE_URL set
[2025-11-06 14:30:53] [INFO] ✓ Database connection successful
[2025-11-06 14:30:53] [INFO] ✓ API health check passed
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] ========================================
[2025-11-06 14:30:53] [INFO] SETUP PHASE
[2025-11-06 14:30:53] [INFO] ========================================
[2025-11-06 14:30:53] [INFO] Creating user: test-user-free-001
[2025-11-06 14:30:53] [INFO]   Tier: free
[2025-11-06 14:30:53] [INFO]   Email: test-free@example.com
[2025-11-06 14:30:53] [INFO] ✓ User created
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] Creating linked item: test-item-free-001
[2025-11-06 14:30:53] [INFO]   Institution: Chase (ins_3)
[2025-11-06 14:30:53] [INFO]   Status: active
[2025-11-06 14:30:53] [INFO]   Cursor: null (first sync)
[2025-11-06 14:30:53] [INFO] ✓ Linked item created
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] Creating account: test-acc-free-001
[2025-11-06 14:30:53] [INFO]   Name: Chase Everyday Checking
[2025-11-06 14:30:53] [INFO]   Type: depository / checking
[2025-11-06 14:30:53] [INFO]   Balance: $1,250.00
[2025-11-06 14:30:53] [INFO] ✓ Account created
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] Setup complete: 1 user, 1 item, 1 account
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] ========================================
[2025-11-06 14:30:53] [INFO] EXECUTION PHASE (2 steps)
[2025-11-06 14:30:53] [INFO] ========================================
[2025-11-06 14:30:53] [INFO]
[2025-11-06 14:30:53] [INFO] Step 1/2: fire_webhook
[2025-11-06 14:30:53] [INFO]   Description: Trigger initial sync
[2025-11-06 14:30:53] [INFO]   Webhook code: INITIAL_UPDATE
[2025-11-06 14:30:53] [INFO]   Item ID: plaid-item-free-sandbox-001
[2025-11-06 14:30:54] [INFO]   Calling POST /api/v1/admin/plaid/fire-webhook...
[2025-11-06 14:30:54] [INFO]   Response: HTTP 200
[2025-11-06 14:30:54] [INFO]   Webhook ID: wb-20251106-143054-abc123
[2025-11-06 14:30:54] [INFO]   Status: queued
[2025-11-06 14:30:54] [INFO] ✓ Webhook fired successfully
[2025-11-06 14:30:54] [INFO]
[2025-11-06 14:30:54] [INFO] Step 2/2: wait
[2025-11-06 14:30:54] [INFO]   Description: Wait for async processing
[2025-11-06 14:30:54] [INFO]   Duration: 10 seconds
[2025-11-06 14:30:54] [INFO]   Waiting...
[2025-11-06 14:30:59] [INFO]   (5s elapsed)
[2025-11-06 14:31:04] [INFO]   (10s elapsed)
[2025-11-06 14:31:04] [INFO] ✓ Wait complete
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Execution complete
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] ASSERTION PHASE
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Running database assertions...
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Assertion 1: Transaction count
[2025-11-06 14:31:04] [INFO]   Query: SELECT COUNT(*) FROM transactions WHERE user_id = 'test-user-free-001'
[2025-11-06 14:31:04] [INFO]   Result: 23
[2025-11-06 14:31:04] [INFO]   Expected: >= 1
[2025-11-06 14:31:04] [PASS] ✓ Transaction count: 23 (expected >= 1)
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Assertion 2: Sync audit count
[2025-11-06 14:31:04] [INFO]   Query: SELECT COUNT(*) FROM account_sync_audit WHERE user_id = 'test-user-free-001'
[2025-11-06 14:31:04] [INFO]   Result: 1
[2025-11-06 14:31:04] [INFO]   Expected: 1
[2025-11-06 14:31:04] [PASS] ✓ Sync audit count: 1 (expected = 1)
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Running webhook status assertions...
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Assertion 3: Webhook processed
[2025-11-06 14:31:04] [INFO]   Query: SELECT processed FROM webhook_events WHERE item_id = '...'
[2025-11-06 14:31:04] [INFO]   Result: true
[2025-11-06 14:31:04] [PASS] ✓ Webhook processed: true
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Assertion 4: No webhook errors
[2025-11-06 14:31:04] [INFO]   Query: SELECT error_message FROM webhook_events WHERE item_id = '...'
[2025-11-06 14:31:04] [INFO]   Result: null
[2025-11-06 14:31:04] [PASS] ✓ No errors: null
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Running custom assertions...
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] Assertion 5: 30-day limit enforced
[2025-11-06 14:31:04] [INFO]   Description: Only transactions within 30 days should be synced
[2025-11-06 14:31:04] [INFO]   Query: SELECT COUNT(*) FROM transactions WHERE date < (CURRENT_DATE - INTERVAL '30 days')
[2025-11-06 14:31:04] [INFO]   Result: 0
[2025-11-06 14:31:04] [INFO]   Expected: 0
[2025-11-06 14:31:04] [PASS] ✓ 30-day limit enforced: 0 old transactions (expected = 0)
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] ASSERTION SUMMARY
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] Passed:  5
[2025-11-06 14:31:04] [INFO] Failed:  0
[2025-11-06 14:31:04] [INFO] Total:   5
[2025-11-06 14:31:04] [INFO] Success: 100.0%
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] REPORTING PHASE
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] Generating HTML report...
[2025-11-06 14:31:04] [INFO] ✓ Report saved: reports/scenario-01-20251106-143104.html
[2025-11-06 14:31:04] [INFO] Generating JSON report...
[2025-11-06 14:31:04] [INFO] ✓ Report saved: reports/scenario-01-20251106-143104.json
[2025-11-06 14:31:04] [INFO]
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] CLEANUP PHASE
[2025-11-06 14:31:04] [INFO] ========================================
[2025-11-06 14:31:04] [INFO] Deleting test user: test-user-free-001
[2025-11-06 14:31:04] [INFO]   Cascade deletes:
[2025-11-06 14:31:04] [INFO]   - 23 transactions
[2025-11-06 14:31:04] [INFO]   - 1 account
[2025-11-06 14:31:04] [INFO]   - 1 linked item
[2025-11-06 14:31:04] [INFO]   - 1 sync audit
[2025-11-06 14:31:05] [INFO] ✓ Cleanup complete
[2025-11-06 14:31:05] [INFO]
[2025-11-06 14:31:05] [INFO] ========================================
[2025-11-06 14:31:05] [INFO] ✅ SCENARIO PASSED
[2025-11-06 14:31:05] [INFO] ========================================
[2025-11-06 14:31:05] [INFO] Duration: 12.3 seconds
[2025-11-06 14:31:05] [INFO] Report: reports/scenario-01-20251106-143104.html
```

#### **Step 5: View Reports**

```bash
# Open HTML report
open reports/scenario-01-20251106-143104.html

# Or view JSON report
cat reports/scenario-01-20251106-143104.json | jq '.'
```

**HTML Report Contents:**
- Scenario metadata (name, category, tags)
- Setup summary (entities created)
- Step-by-step execution timeline
- Assertion results (pass/fail with details)
- Database snapshots (transactions, audit records)
- Duration and performance metrics

**JSON Report Contents:**
```json
{
  "scenario": {
    "name": "Free Tier First Sync",
    "category": "tier_limit",
    "tags": ["free-tier", "first-sync", "regression"]
  },
  "execution": {
    "start_time": "2025-11-06T14:30:52Z",
    "end_time": "2025-11-06T14:31:05Z",
    "duration_seconds": 12.3
  },
  "setup": {
    "users": 1,
    "linked_items": 1,
    "accounts": 1,
    "transactions": 0
  },
  "steps": [
    {
      "step": 1,
      "action": "fire_webhook",
      "webhook_code": "INITIAL_UPDATE",
      "item_id": "plaid-item-free-sandbox-001",
      "status": "success",
      "duration_seconds": 1.2
    },
    {
      "step": 2,
      "action": "wait",
      "wait_seconds": 10,
      "status": "success",
      "duration_seconds": 10.0
    }
  ],
  "assertions": {
    "total": 5,
    "passed": 5,
    "failed": 0,
    "results": [
      {
        "description": "Transaction count",
        "expected": ">= 1",
        "actual": 23,
        "status": "passed"
      },
      {
        "description": "Sync audit count",
        "expected": "1",
        "actual": 1,
        "status": "passed"
      },
      {
        "description": "Webhook processed",
        "expected": "true",
        "actual": "true",
        "status": "passed"
      },
      {
        "description": "No errors",
        "expected": "null",
        "actual": "null",
        "status": "passed"
      },
      {
        "description": "30-day limit enforced",
        "expected": "0",
        "actual": 0,
        "status": "passed"
      }
    ]
  },
  "status": "passed"
}
```

---

## Real-World Example

### Scenario: Testing User Notes Preservation Bug Fix

**Context:** Issue #XXX reported that user-added notes on transactions were being deleted during transaction sync when Plaid sent updates. This is a **critical data loss bug**.

**Goal:** Create a regression test that:
1. Seeds a transaction with user notes
2. Fires a webhook that updates that transaction
3. Verifies notes are preserved (not overwritten)

**Scenario File:** `scenarios/03-preserve-user-notes.yaml` (already created in Phase 3)

**Execution:**

```bash
# 1. Run scenario
./run-scenario.sh scenarios/03-preserve-user-notes.yaml

# 2. Scenario creates transaction with notes
# INSERT INTO transactions (..., user_notes) VALUES (..., 'Work laptop - reimbursable')

# 3. Scenario fires DEFAULT_UPDATE webhook
# POST /api/v1/admin/plaid/fire-webhook
# { "item_id": "...", "webhook_code": "DEFAULT_UPDATE" }

# 4. Backend syncs and UPDATES transaction
# UPDATE transactions SET amount = ..., date = ..., name = ...
# (MUST NOT update user_notes!)

# 5. Scenario asserts notes preserved
# SELECT user_notes FROM transactions WHERE id = '...'
# Expected: 'Work laptop - reimbursable'
# Actual: 'Work laptop - reimbursable'
# ✅ PASS

# 6. If bug still exists:
# Actual: null
# ✗ FAIL - notes were overwritten!
```

**Result:** This scenario becomes a **permanent regression test**. Any future changes that break user notes preservation will fail this test immediately.

---

## Common Use Cases

### Use Case 1: Populate Staging with Realistic Data

**Goal:** Create realistic transaction data for manual QA testing

**Approach:**
```bash
# Run all scenarios to populate staging
./run-all-scenarios.sh --env staging --no-cleanup

# Scenarios create:
# - 5 users (free, premium, premium+)
# - 10 linked items across different institutions
# - 20 accounts (checking, savings, credit cards)
# - 500+ transactions (realistic merchants, amounts, dates)

# QA team can now:
# - Log in as test users
# - See realistic transaction data
# - Test budgeting features
# - Test automation rules
# - Test filtering/sorting/searching
```

**Benefits:**
- Consistent, reproducible data
- Covers edge cases (refunds, pending transactions, etc.)
- Multiple user tiers represented
- Realistic date ranges and amounts

---

### Use Case 2: Validate Bug Fix Before Production

**Goal:** Confirm Issue #242 (monthly sync limits) is fixed

**Approach:**
```bash
# Run tier limit scenario
./run-scenario.sh scenarios/04-tier-limit-enforcement.yaml

# Scenario:
# 1. Creates free tier user (limit: 3 accounts/month)
# 2. Fires webhooks for 4 different accounts
# 3. Asserts that only 3 synced successfully
# 4. Asserts that 4th sync returned "sync limit exceeded" error
# 5. Asserts that user is prompted to upgrade to premium

# If scenario passes:
# ✅ Bug fix confirmed working
# → Safe to deploy to production

# If scenario fails:
# ✗ Bug still present
# → DO NOT deploy to production
# → Check TransactionSyncService.checkSyncLimits() implementation
```

---

### Use Case 3: Regression Testing After Code Changes

**Goal:** Ensure refactoring didn't break existing functionality

**Approach:**
```bash
# Developer refactored TransactionSyncService
# Run all scenarios as smoke test
./run-all-scenarios.sh --continue-on-failure

# Results:
# ✅ 01-free-tier-first-sync.yaml - PASSED
# ✅ 02-pagination-multi-page.yaml - PASSED
# ✗ 03-preserve-user-notes.yaml - FAILED
# ✅ 04-tier-limit-enforcement.yaml - PASSED
# ✅ 05-item-error-login-required.yaml - PASSED

# Summary: 4/5 passed (80%)
# ❌ REGRESSION: User notes preservation broken!

# Developer:
# 1. Reviews scenario-03 failure details
# 2. Finds UPDATE query now includes user_notes field
# 3. Fixes query to exclude user_notes
# 4. Re-runs scenario
# 5. ✅ ALL scenarios pass
# 6. Safe to commit refactoring
```

---

## FAQ

### Q: Can I use this framework for automated CI/CD testing?

**A:** Not directly. This framework is designed for **manual staging population**. For automated CI/CD testing, use **Issue #331 (E2E Test Scenarios Framework)** which:
- Runs automatically on every push
- Uses isolated test environment (docker-compose.test.yml)
- Integrates with Playwright
- Doesn't require manual Auth0 tokens

### Q: How do I get an Auth0 JWT token for staging?

**A:** Two options:

1. **Manual (quick):** Log into staging frontend, open browser DevTools → Application → Local Storage → copy `auth0_token`
2. **Automated (better):** Use Auth0 M2M client credentials:
```bash
curl -X POST https://YOUR-AUTH0-DOMAIN/oauth/token \
  -H 'content-type: application/json' \
  -d '{
    "client_id":"YOUR_M2M_CLIENT_ID",
    "client_secret":"YOUR_M2M_CLIENT_SECRET",
    "audience":"https://api.boostedbudget.com",
    "grant_type":"client_credentials"
  }' | jq -r '.access_token'
```

### Q: Can I run scenarios against my local development environment?

**A:** Yes! Use `config/local.env` instead of `config/staging.env`:

```bash
# config/local.env
API_BASE_URL="http://localhost:8080"
AUTH_TOKEN="your-local-dev-token"
DATABASE_URL="postgres://devuser:devpass@localhost:5432/boostedbudget"

# Run locally
source config/local.env
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml --env local
```

### Q: What happens if a scenario fails mid-execution?

**A:** The framework:
1. Captures failure context (logs, database state)
2. Generates failure report (HTML + JSON)
3. **DOES NOT** run cleanup (leaves data for debugging)
4. Exits with status code 1

**Manual cleanup:**
```bash
# Find test user ID from logs
# Then:
source config/staging.env
source lib/db-utils.sh
cleanup_test_user "test-user-free-001"
```

### Q: Can I run scenarios in parallel?

**A:** Yes, but experimental:
```bash
./run-all-scenarios.sh --parallel
```

**Limitations:**
- No concurrency control (could spawn unlimited processes)
- Database contention (multiple scenarios writing simultaneously)
- Harder to debug failures
- Use with caution

**Future:** Issue #XXX will add proper parallel execution with database isolation.

### Q: How do I create a new scenario?

**A:** See [README.md](README.md) "Adding New Scenarios" section. Quick steps:

1. Copy template: `cp scenarios/00-template.yaml scenarios/99-my-scenario.yaml`
2. Edit YAML (setup, steps, assertions)
3. Test locally: `./run-scenario.sh scenarios/99-my-scenario.yaml --env local`
4. Test on staging: `./run-scenario.sh scenarios/99-my-scenario.yaml --env staging`
5. Commit to Git

### Q: What's the difference between this framework and Issue #331?

| Aspect | Issue #314 (This Framework) | Issue #331 (E2E Framework) |
|--------|----------------------------|----------------------------|
| **Purpose** | Manual staging population | Automated CI/CD testing |
| **Language** | Bash | TypeScript |
| **Target** | Staging environment | Test environment |
| **Execution** | Manual (`./run-scenario.sh`) | Automated (GitHub Actions) |
| **Auth** | Manual JWT token | Auth0 storage state |
| **Data** | Admin API + direct DB | API + UI + DB seeding |
| **Integration** | Standalone scripts | Playwright-integrated |
| **Use Case** | QA testing, bug validation | Regression testing, CI/CD |

**TL;DR:** This framework is for **manual testing**, Issue #331 is for **automated testing**.

---

## Summary

The Test Scenarios Framework provides a **simple, powerful way** to populate staging with comprehensive test data and validate complex transaction sync flows.

**Key Takeaways:**

1. ✅ **Configuration-driven** - Add tests by writing YAML, not code
2. ✅ **Comprehensive** - Covers normal flows, edge cases, errors, tier limits
3. ✅ **Replicatable** - Same scenarios run consistently every time
4. ✅ **Manual focus** - Designed for staging population, not CI/CD
5. ✅ **Production-ready** - 22 files, 7,000+ lines, fully documented

**Next Steps:**

- Read [README.md](README.md) for detailed usage guide
- Review [PROGRESS.md](PROGRESS.md) for implementation history
- Explore `scenarios/*.yaml` for real examples
- Check `templates/*.yaml` for reusable patterns

**Need Help?**

- Check [README.md](README.md) "Troubleshooting" section
- Review scenario examples in `scenarios/` directory
- See [PROGRESS.md](PROGRESS.md) for design decisions and lessons learned
- Create GitHub issue for bugs/feature requests

---

**Happy Testing! 🧪**
