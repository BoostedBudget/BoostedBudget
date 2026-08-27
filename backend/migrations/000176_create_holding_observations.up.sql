-- #1569 task 9-FIX item 2: "we looked and it held nothing" needs somewhere to be written down.
--
-- THE HOLE THIS CLOSES
-- holding_snapshots is a record of POSITIONS. A capture that observes an account holding nothing
-- writes no row at all: markHoldingSnapshotsAbsentForAccountSQL can only mark rows that already
-- exist, and captureHoldingSnapshotsSQL's INSERT ... SELECT selects nothing. For an account with
-- prior history that is survivable — the day's rows all read present = false, which IS the
-- observation. For an account with NO prior history it is not: a re-link successor whose very
-- first /investments/holdings/get comes back empty leaves zero rows behind, and every reader has
-- to guess whether the capture ran.
--
-- The portfolio re-link rule (account_liveness_sql.go) is the reader that guesses wrong with
-- money attached. It drops a retained predecessor only when its successor HAS an observation, so
-- that the window between a re-link and the successor's first sync does not write a zero into the
-- curve. A confirmed-empty successor is indistinguishable from that window, so the predecessor is
-- kept and the user's portfolio is reported at the predecessor's STALE valuation instead of the
-- zero that is true.
--
-- This branch has already ruled on exactly this distinction once, for the holdings day-picker
-- (holding_snapshot_repository.go, ListHistoryDatesForAccount): "we observed it and it held
-- nothing" is a TRUE OBSERVATION we kept, and it is a different fact from "we kept nothing for
-- that day". That ruling needed no new storage because an absent row carried it. Here there is no
-- row to carry it, so the observation gets its own record.
--
-- WHAT THIS TABLE IS NOT
-- It is not history. It holds no value, no position and nothing the user would notice losing —
-- one row per (account, UTC day) saying a capture completed. It is classified in
-- deliberatelyNotHistoryBearing for exactly that reason. Its user_id CASCADE is the GDPR erasure
-- route DeleteUserCascade relies on; its account_id CASCADE matches holding_snapshots'.
--
-- WHY NOT A COLUMN ON accounts
-- A single "last observed on" column answers only about the most recent day. The portfolio REPAIR
-- rebuilds arbitrary PAST days and needs the same distinction for each of them, so the record has
-- to be per-day.

CREATE TABLE IF NOT EXISTS holding_observations (
    user_id     UUID        NOT NULL,
    account_id  UUID        NOT NULL,
    as_of_date  DATE        NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (account_id, as_of_date)
);

COMMENT ON TABLE holding_observations IS
    'One row per (account, UTC day) on which a holdings capture COMPLETED for that account '
    '(#1569 task 9-FIX). It records that we looked, not what we found — the positions live in '
    'holding_snapshots, and an account that held nothing produces a row here and none there. '
    'Written in the same transaction as the capture. Not history: it carries no value and nothing '
    'the user would notice losing, which is why it sits in deliberatelyNotHistoryBearing.';

COMMENT ON COLUMN holding_observations.as_of_date IS
    'UTC calendar date the capture filed under, taken from the SERVER (serverUTCDate) and passed '
    'as a parameter, exactly like holding_snapshots.as_of_date — so it cannot drift with the '
    'session TimeZone and it always matches the positions written beside it.';

CREATE INDEX IF NOT EXISTS idx_holding_observations_user_date
    ON holding_observations (user_id, as_of_date);

DO $$ BEGIN
    ALTER TABLE holding_observations
        ADD CONSTRAINT fk_holding_observations_user_id
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holding_observations VALIDATE CONSTRAINT fk_holding_observations_user_id;

DO $$ BEGIN
    ALTER TABLE holding_observations
        ADD CONSTRAINT fk_holding_observations_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holding_observations VALIDATE CONSTRAINT fk_holding_observations_account_id;
