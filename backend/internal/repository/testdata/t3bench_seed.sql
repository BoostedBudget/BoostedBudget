-- t3bench_seed.sql — the household #1569 T3 (#1582) was measured on.
--
-- One household, 4 members (one departed), 50 user_accounts, 80 grants (50 open +
-- 30 closed), 200,000 transactions over 2022-01-01..2024-12-31. Five of the accounts
-- are JOINT: two user_accounts share one physical fingerprint, which is the shape that
-- forces the per-transaction tiebreak. That makes 20% of accounts contested — pessimistic
-- for a real household, so the measured win understates the typical case.
--
-- Load into a THROWAWAY database (never the integration DB — a leaked fixture row here
-- breaks unrelated global-scan tests):
--
--   createdb t3bench && pg_dump -s <integration-db> | psql t3bench && psql t3bench -f this
--   T3BENCH_URL=postgres://.../t3bench go test -tags t3bench -run TestT3Bench ./internal/repository/ -v
--
-- ⚠ THIS FILE WILL ROT, SILENTLY. Nothing compiles or executes it — not CI, not `go test` — and it
-- names ~12 columns across 7 tables. The first migration that adds a NOT NULL column without a
-- default, or tightens a CHECK, breaks these INSERTs, and the only person who finds out is whoever
-- next tries to reproduce #1582's numbers. That is the accepted trade (a CI job for a benchmark
-- fixture costs more than it saves). If it fails to load: the schema comes from the `pg_dump -s`
-- step above and is therefore always current, so diff the failing INSERT's column list against
-- `\d <table>` in the freshly dumped t3bench database and add whatever the migration introduced.
-- Keep the SHAPE fixed when you do — 50 accounts / 80 grants / 200k transactions / 5 joint
-- accounts — or the numbers stop being comparable to the ones recorded in analytics_repository.go.
--
-- T3 benchmark seed: one household, 4 members, 50 user_accounts, 80 grants, 200k transactions.
SET client_min_messages = warning;

INSERT INTO users (id, clerk_user_id, email, subscription_tier)
SELECT ('00000000-0000-0000-0000-00000000000' || n)::uuid,
       'clerk_bench_' || n, 'bench' || n || '@example.com', 'household_4'
FROM generate_series(1,4) n;

INSERT INTO households (id, owner_user_id, name)
VALUES ('00000000-0000-0000-0000-0000000000aa', '00000000-0000-0000-0000-000000000001', 'Bench Household');

-- member 4 has departed (status left); members 1-3 active
INSERT INTO household_members (household_id, user_id, role, status, removed_at)
SELECT '00000000-0000-0000-0000-0000000000aa',
       ('00000000-0000-0000-0000-00000000000' || n)::uuid,
       CASE WHEN n = 1 THEN 'owner' ELSE 'member' END,
       CASE WHEN n = 4 THEN 'removed' ELSE 'active' END,
       CASE WHEN n = 4 THEN TIMESTAMPTZ '2023-01-01 00:00:00+00' END
FROM generate_series(1,4) n;

INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
SELECT ('00000000-0000-0000-0000-0000000001' || lpad(n::text,2,'0'))::uuid,
       ('00000000-0000-0000-0000-00000000000' || n)::uuid,
       'item_' || n, 'enc'
FROM generate_series(1,4) n;

-- 50 user_accounts, round-robin over the 4 members
INSERT INTO user_accounts (id, user_id, institution_id, mask, primary_type)
SELECT ('00000000-0000-0000-0000-0000000002' || lpad(n::text,2,'0'))::uuid,
       ('00000000-0000-0000-0000-00000000000' || (1 + (n-1) % 4))::uuid,
       'ins_' || (1 + (n-1) % 7), lpad(n::text,4,'0'), 'depository'
FROM generate_series(1,50) n;

INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, user_account_id, current_balance)
SELECT ('00000000-0000-0000-0000-0000000003' || lpad(n::text,2,'0'))::uuid,
       ('00000000-0000-0000-0000-0000000001' || lpad((1 + (n-1) % 4)::text,2,'0'))::uuid,
       ('00000000-0000-0000-0000-00000000000' || (1 + (n-1) % 4))::uuid,
       'plaid_acct_' || n, 'Account ' || n,
       (ARRAY['depository','credit','depository','depository'])[1 + (n % 4)],
       'checking',
       ('00000000-0000-0000-0000-0000000002' || lpad(n::text,2,'0'))::uuid,
       1000 + n
FROM generate_series(1,50) n;

-- ── Grants: 80 total ────────────────────────────────────────────────────────
-- 50 OPEN grants, one per user_account. Fingerprints: accounts 1..10 pair up
-- (1&2, 3&4, ...) onto 5 shared physical fingerprints = joint accounts held by
-- two different members. The remaining 40 get their own fingerprint.
INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility, shared_at, revoked_at)
SELECT '00000000-0000-0000-0000-0000000000aa',
       ('00000000-0000-0000-0000-0000000002' || lpad(n::text,2,'0'))::uuid,
       CASE WHEN n <= 10 THEN 'phys_joint_' || ((n+1)/2) ELSE 'phys_' || n END,
       'shared',
       TIMESTAMPTZ '2023-01-01 00:00:00+00',
       NULL
FROM generate_series(1,50) n;

-- 30 CLOSED grants: re-fingerprint history on accounts 1..30 (the shape the
-- retention fix admits: a departed/re-linked account keeps its past interval).
INSERT INTO household_account_shares (household_id, user_account_id, physical_account_blind_idx, visibility, shared_at, revoked_at)
SELECT '00000000-0000-0000-0000-0000000000aa',
       ('00000000-0000-0000-0000-0000000002' || lpad(n::text,2,'0'))::uuid,
       CASE WHEN n <= 10 THEN 'phys_joint_' || ((n+1)/2) ELSE 'phys_old_' || n END,
       'shared',
       TIMESTAMPTZ '2022-01-01 00:00:00+00',
       TIMESTAMPTZ '2022-12-31 00:00:00+00'
FROM generate_series(1,30) n;

-- ── 200k transactions over 2022-01-01 .. 2024-12-31 ─────────────────────────
INSERT INTO transactions (user_id, account_id, user_account_id, source, amount, date, name, pending, pfc_primary, merchant_name)
SELECT ('00000000-0000-0000-0000-00000000000' || (1 + ((n % 50)) % 4))::uuid,
       ('00000000-0000-0000-0000-0000000003' || lpad((1 + n % 50)::text,2,'0'))::uuid,
       ('00000000-0000-0000-0000-0000000002' || lpad((1 + n % 50)::text,2,'0'))::uuid,
       'plaid',
       CASE WHEN n % 9 = 0 THEN -(100 + (n % 500))::numeric ELSE ((n % 300) + 1)::numeric END,
       DATE '2022-01-01' + ((n % 1096) || ' days')::interval,
       'Txn ' || n,
       false,
       (ARRAY['FOOD_AND_DRINK','TRANSPORTATION','ENTERTAINMENT','GENERAL_MERCHANDISE','RENT_AND_UTILITIES','MEDICAL','TRAVEL','INCOME'])[1 + (n % 8)],
       'Merchant ' || (n % 200)
FROM generate_series(1,200000) n;

ANALYZE;
COMMIT;

-- GetSpendingByCategory scans effective_category_name into a non-pointer string.
UPDATE transactions SET effective_category_name = pfc_primary WHERE effective_category_name IS NULL;
ANALYZE;
