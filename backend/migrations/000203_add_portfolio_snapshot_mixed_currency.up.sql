-- #1631 M8a fix round 2 (I-1): let a portfolio snapshot say it was MIXED, not merely unlabelled.
--
-- Migration 000202 gave portfolio_snapshots a currency_code whose NULL was deliberately overloaded:
-- it meant "the contributing holdings disagreed, so these totals are a currency-blind sum" AND it
-- meant "no contributing holding stated a currency" (every row written before 000202). The comment
-- on that column told readers to treat NULL as "do not assume", and the report justified the
-- overload on the grounds that no reader needed the two apart.
--
-- That was WRONG, and there is exactly one reader that needs them apart: GetPortfolioSummary.
-- It subtracts a prior snapshot from a live total, and its guard is CurrenciesConflict, which
-- treats an unknown currency as NO conflict -- correct for "unknown", catastrophic for "mixed":
--
--   Day 1  holdings USD + GBP  -> snapshot currency_code NULL, total is a currency-blind sum
--   Day 2  the GBP sleeve is sold; live holdings are all USD -> live currency "USD", not mixed
--   -> CurrenciesConflict(NULL, "USD") is false, so day_change = USD_today - (USD+GBP)_yesterday
--      is computed and returned as a plain number, on a response whose is_mixed_currency is FALSE.
--
-- An unlabelled cross-currency subtraction is precisely what M8a exists to stop, and it was being
-- emitted by the one surface the design gave the STRICTER treatment to. This column splits the
-- overloaded NULL so the guard can tell the two cases apart.
--
-- NOT NULL DEFAULT FALSE, unlike 000202's nullable columns, and the asymmetry is deliberate. This
-- is a three-valued question collapsed onto a pair of columns:
--
--   currency_code   is_mixed_currency   meaning
--   --------------  -----------------   -------------------------------------------------------
--   'USD'           FALSE               single currency, named
--   NULL            FALSE               no contributing holding stated a currency -- UNKNOWN
--   NULL            TRUE                two or more currencies -- a currency-blind sum
--
-- So FALSE is not a claim that a legacy row was single-currency; paired with a NULL currency_code
-- it still reads as "unknown", exactly as it did before this migration. Backfilling TRUE for
-- historical rows is impossible anyway (their contributing holdings had no currency to disagree
-- about), and defaulting to TRUE would suppress the day change for every existing user.
--
-- The ('USD', TRUE) combination is meaningless by construction: the writer emits a currency_code
-- only when COUNT(DISTINCT ...) = 1, which is the same predicate that makes this column FALSE.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'portfolio_snapshots' AND column_name = 'is_mixed_currency'
    ) THEN
        ALTER TABLE portfolio_snapshots
            ADD COLUMN is_mixed_currency BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

COMMENT ON COLUMN portfolio_snapshots.is_mixed_currency IS
    'TRUE when the holdings behind this row''s totals were denominated in two or more DIFFERENT '
    'currencies, making every total on the row a currency-blind sum -- there is no FX rate source '
    'in this system to convert with. Read TOGETHER with currency_code: (code, FALSE) is a named '
    'single currency, (NULL, FALSE) is "nobody stated one" i.e. unknown, and (NULL, TRUE) is '
    'mixed. FALSE on a row written before this migration therefore still reads as unknown rather '
    'than as a claim, because its currency_code is NULL. GetPortfolioSummary refuses to compute a '
    'day change against a row where this is TRUE.';
