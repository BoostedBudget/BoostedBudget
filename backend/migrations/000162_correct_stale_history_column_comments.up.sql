-- #1569 Task 10, fix round 2: correct COMMENT ON COLUMN text that is resident IN THE DATABASE.
--
-- 000160 shipped a comment on holding_snapshots.captured_at telling readers to filter on
-- MAX(captured_at) per (account_id, as_of_date) to obtain a day's final position set. 000161
-- deleted that rule from the code — it made a reconstruction OVER-report, returning a morning
-- capture as the day's answer after a confirmed-complete liquidation had cleared the account
-- (measured: live holdings 0, day's history 5000.00). It did not, and could not, correct the
-- comment: `CREATE TABLE IF NOT EXISTS` means editing 000160's text does nothing for a database
-- that has already run it.
--
-- So the deleted rule is still what `\d+ holding_snapshots` teaches, and the next person to build
-- on this table is likely to reach for psql before reaching for the migration file. A stale
-- instruction that reads as authoritative is worse than none. This migration is comments only —
-- no schema change, nothing to roll back but the text.

COMMENT ON COLUMN holding_snapshots.captured_at IS
    'Provenance of this row: the transaction timestamp of the capture that last wrote it. NOT a '
    'selection key. Reconstruction of a day''s positions filters on `present` — a capture marks '
    'everything it did not re-observe absent, so the present rows for an (account_id, as_of_date) '
    'ARE that day''s last capture, by construction. An earlier draft of this table asked readers '
    'to filter on MAX(captured_at) instead; that rule is WRONG and must not be reintroduced — it '
    'resurrects positions a confirmed-complete Plaid response had cleared.';

COMMENT ON COLUMN liability_statements.captured_at IS
    'Provenance of this row: the transaction timestamp of the capture that last wrote it. NOT a '
    'selection key — see holding_snapshots.captured_at. A liability the delete-on-absence path '
    'cleared is identified by present = FALSE, not by any captured_at comparison.';

COMMENT ON TABLE holding_snapshots IS
    'Append-only history of investment positions (#1569 T6). Written in the SAME transaction as '
    'holdings'' full-replace, in addition to it — the holdings table keeps its replace semantics '
    'unchanged. One row per (account_id, security_id, as_of_date) UTC day. Nothing here is ever '
    'deleted; a position that stopped being held is marked present = FALSE and keeps its last '
    'known values, so a disconnected institution''s positions stay queryable. '
    'A capture applies the SAME account_status_history overlap test portfolio_snapshots'' Upsert '
    'applies, so a day''s present rows reconstruct that day''s aggregate exactly; reconstruction '
    'therefore needs no liveness test of its own, because the capture already applied one.';
