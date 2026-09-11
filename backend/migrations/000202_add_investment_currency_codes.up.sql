-- #1631 M8a: give the investment domain a currency.
--
-- Until now NOTHING in the investment domain recorded a currency: not `securities`, not
-- `holdings`, not `investment_transactions` (migration 000051), not `holding_snapshots` (000160)
-- and not `portfolio_snapshots` (000096) — even though Plaid returns `iso_currency_code` /
-- `unofficial_currency_code` on every security, holding and investment transaction, and
-- `accounts.currency_code` has existed since migration 000007. Every investment total was
-- therefore a currency-blind sum, and `quantity * (institution_price - close_price)` subtracted
-- two different units for any holding whose price Plaid reports in a currency other than the one
-- it prices the security in (Plaid documents close_price as "priced in USD" for a
-- foreign-currency security while institution_price is "the last price given by the
-- institution").
--
-- NAMING: `currency_code`, matching accounts.currency_code and balance_snapshots.currency_code
-- rather than mirroring Plaid's two-column shape. Plaid guarantees exactly one of its two fields
-- is non-null, so one column loses nothing an aggregate needs: what the aggregates need is a
-- comparable currency IDENTITY, and "USD" and "BTC" compare the same way whether or not one of
-- them is ISO-4217.
--
-- WIDTH: VARCHAR(16), not the VARCHAR(3) that accounts.currency_code uses. Plaid's unofficial
-- currency codes are not constrained to three characters (they cover cryptocurrencies and
-- currencies with no ISO code), so a VARCHAR(3) here would silently truncate — or, under
-- Postgres, hard-error — on the exact rows this migration exists to record. The narrower accounts
-- column is a pre-existing latent problem and is deliberately left alone: widening it would
-- rewrite a table this change has no business touching.
--
-- NULLABLE, WITH NO DEFAULT, deliberately. A default of 'USD' would write a fact we do not have:
-- every row already in these tables was ingested by a mapper that never read Plaid's currency
-- fields, so its currency is genuinely UNKNOWN, and stamping 'USD' on it would turn an absence
-- into a confident wrong answer. Readers therefore treat NULL as "unknown", which by design
-- cannot make an aggregate look mixed — see the mixed-currency rule in
-- investment_analytics_repository.go and domain/investment.go.

-- ============================================================
-- securities
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'securities' AND column_name = 'currency_code'
    ) THEN
        ALTER TABLE securities ADD COLUMN currency_code VARCHAR(16);
    END IF;
END $$;

COMMENT ON COLUMN securities.currency_code IS
    'Currency of the PRICE columns on this row (close_price above all), from Plaid''s '
    'iso_currency_code, falling back to unofficial_currency_code. NULL = unknown (Plaid sent '
    'neither, or the row predates #1631). ⚠ This is NOT necessarily the currency a HOLDING of this '
    'security is priced in: Plaid prices a foreign-currency security in USD while the institution '
    'reports the position in its own currency, which is why holdings.currency_code exists '
    'separately and why a day change is refused when the two disagree.';

-- ============================================================
-- holdings
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'holdings' AND column_name = 'currency_code'
    ) THEN
        ALTER TABLE holdings ADD COLUMN currency_code VARCHAR(16);
    END IF;
END $$;

COMMENT ON COLUMN holdings.currency_code IS
    'Currency of cost_basis, institution_value and institution_price on this row, from Plaid''s '
    'iso_currency_code falling back to unofficial_currency_code. NULL = unknown (row predates '
    '#1631). Not summable across differing values — there is no FX rate source in this system.';

-- ============================================================
-- investment_transactions
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'investment_transactions' AND column_name = 'currency_code'
    ) THEN
        ALTER TABLE investment_transactions ADD COLUMN currency_code VARCHAR(16);
    END IF;
END $$;

COMMENT ON COLUMN investment_transactions.currency_code IS
    'Currency of amount, price and fees on this row, from Plaid''s iso_currency_code falling back '
    'to unofficial_currency_code. NULL = unknown (row predates #1631).';

-- ============================================================
-- holding_snapshots
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'holding_snapshots' AND column_name = 'currency_code'
    ) THEN
        ALTER TABLE holding_snapshots ADD COLUMN currency_code VARCHAR(16);
    END IF;
END $$;

COMMENT ON COLUMN holding_snapshots.currency_code IS
    'Copy of holdings.currency_code as at the capture, so a historical position can be valued in '
    'the currency it was actually reported in rather than in whatever currency the live row later '
    'reports. NULL = unknown.';

-- ============================================================
-- portfolio_snapshots
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'portfolio_snapshots' AND column_name = 'currency_code'
    ) THEN
        ALTER TABLE portfolio_snapshots ADD COLUMN currency_code VARCHAR(16);
    END IF;
END $$;

COMMENT ON COLUMN portfolio_snapshots.currency_code IS
    'The single currency this row''s totals are expressed in, or NULL when the contributing '
    'holdings did not agree on one (or none of them stated one). ⚠ NULL therefore means "do not '
    'assume": it covers both "mixed, so this total is a currency-blind sum" and "unknown". A '
    'reader that needs certainty must check for NULL rather than assume USD.';
