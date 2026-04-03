-- Create liability tables for Plaid Liabilities product
-- Issue #N: Plaid Liabilities: Credit, Mortgage, Student Loan
--
-- Tables created in dependency order:
--   1. credit_liabilities (one-to-one with accounts for credit cards)
--   2. credit_liability_aprs (child rows of credit_liabilities)
--   3. mortgage_liabilities (one-to-one with accounts for mortgages)
--   4. student_loan_liabilities (one-to-one with accounts for student loans)
-- Plus: add liabilities_last_synced_at to linked_items for incremental sync tracking

-- ============================================================
-- 1. credit_liabilities
-- ============================================================
CREATE TABLE IF NOT EXISTS credit_liabilities (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id    UUID NOT NULL,
    account_id UUID NOT NULL,

    -- Liability data
    is_overdue                  BOOLEAN,
    last_payment_amount         DECIMAL(18, 2),
    last_payment_date           DATE,
    last_statement_issue_date   DATE,
    last_statement_balance      DECIMAL(18, 2),
    minimum_payment_amount      DECIMAL(18, 2),
    next_payment_due_date       DATE,

    -- Timestamps
    synced_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- One liability record per account
    CONSTRAINT unique_credit_liability_account UNIQUE (account_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
-- Wrapped in DO blocks for idempotency (re-running after partial failure won't error)
DO $$ BEGIN
    ALTER TABLE credit_liabilities ADD CONSTRAINT fk_credit_liabilities_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE credit_liabilities VALIDATE CONSTRAINT fk_credit_liabilities_user_id;
DO $$ BEGIN
    ALTER TABLE credit_liabilities ADD CONSTRAINT fk_credit_liabilities_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE credit_liabilities VALIDATE CONSTRAINT fk_credit_liabilities_account_id;

-- Indexes for credit_liabilities
CREATE INDEX IF NOT EXISTS idx_credit_liabilities_user_id    ON credit_liabilities(user_id);
CREATE INDEX IF NOT EXISTS idx_credit_liabilities_account_id ON credit_liabilities(account_id);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_credit_liabilities_updated_at'
    ) THEN
        CREATE TRIGGER update_credit_liabilities_updated_at
            BEFORE UPDATE ON credit_liabilities
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE credit_liabilities IS 'Credit card liability details from Plaid. One row per credit account.';
COMMENT ON COLUMN credit_liabilities.synced_at IS 'Timestamp of the most recent Plaid liabilities sync for this record';
COMMENT ON COLUMN credit_liabilities.is_overdue IS 'Whether the credit account is currently overdue';
COMMENT ON COLUMN credit_liabilities.minimum_payment_amount IS 'Minimum payment due on the next statement';

-- ============================================================
-- 2. credit_liability_aprs
-- ============================================================
CREATE TABLE IF NOT EXISTS credit_liability_aprs (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign key
    credit_liability_id UUID NOT NULL,

    -- APR data
    rate_percentage            DECIMAL(8, 4) NOT NULL,
    rate_type                  VARCHAR(100)  NOT NULL,   -- provider-neutral: "purchase_apr", "balance_transfer_apr", "cash_apr", "special"
    balance_subject_to_rate    DECIMAL(18, 2),
    interest_charge_amount     DECIMAL(18, 2),

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
DO $$ BEGIN
    ALTER TABLE credit_liability_aprs ADD CONSTRAINT fk_credit_liability_aprs_credit_liability_id FOREIGN KEY (credit_liability_id) REFERENCES credit_liabilities(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE credit_liability_aprs VALIDATE CONSTRAINT fk_credit_liability_aprs_credit_liability_id;

-- Indexes for credit_liability_aprs
CREATE INDEX IF NOT EXISTS idx_credit_liability_aprs_credit_liability_id ON credit_liability_aprs(credit_liability_id);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_credit_liability_aprs_updated_at'
    ) THEN
        CREATE TRIGGER update_credit_liability_aprs_updated_at
            BEFORE UPDATE ON credit_liability_aprs
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE credit_liability_aprs IS 'APR tiers for a credit liability. Multiple rows per credit_liability (purchase, cash advance, balance transfer, etc.).';
COMMENT ON COLUMN credit_liability_aprs.rate_type IS 'Provider-neutral APR category: purchase_apr, balance_transfer_apr, cash_apr, special';
COMMENT ON COLUMN credit_liability_aprs.balance_subject_to_rate IS 'Balance amount currently accruing interest at this rate';
COMMENT ON COLUMN credit_liability_aprs.interest_charge_amount IS 'Interest charge amount for the current billing cycle at this rate';

-- ============================================================
-- 3. mortgage_liabilities
-- ============================================================
CREATE TABLE IF NOT EXISTS mortgage_liabilities (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id    UUID NOT NULL,
    account_id UUID NOT NULL,

    -- Loan identification
    account_number VARCHAR(100),

    -- Financial data
    current_late_fee              DECIMAL(18, 2),
    escrow_balance                DECIMAL(18, 2),
    has_pmi                       BOOLEAN,
    has_prepayment_penalty        BOOLEAN,
    interest_rate_percentage      DECIMAL(8, 4),
    interest_rate_type            VARCHAR(100),           -- e.g. "fixed", "variable"
    last_payment_amount           DECIMAL(18, 2),
    last_payment_date             DATE,
    loan_type_description         VARCHAR(255),
    loan_term                     VARCHAR(100),
    maturity_date                 DATE,
    next_monthly_payment          DECIMAL(18, 2),
    next_payment_due_date         DATE,
    origination_date              DATE,
    origination_principal_amount  DECIMAL(18, 2),
    past_due_amount               DECIMAL(18, 2),

    -- Property address
    property_address_street       VARCHAR(500),
    property_address_city         VARCHAR(255),
    property_address_region       VARCHAR(100),
    property_address_postal_code  VARCHAR(20),
    property_address_country      VARCHAR(10),

    -- Year-to-date totals
    ytd_interest_paid  DECIMAL(18, 2),
    ytd_principal_paid DECIMAL(18, 2),

    -- Timestamps
    synced_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- One liability record per account
    CONSTRAINT unique_mortgage_liability_account UNIQUE (account_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
DO $$ BEGIN
    ALTER TABLE mortgage_liabilities ADD CONSTRAINT fk_mortgage_liabilities_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE mortgage_liabilities VALIDATE CONSTRAINT fk_mortgage_liabilities_user_id;
DO $$ BEGIN
    ALTER TABLE mortgage_liabilities ADD CONSTRAINT fk_mortgage_liabilities_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE mortgage_liabilities VALIDATE CONSTRAINT fk_mortgage_liabilities_account_id;

-- Indexes for mortgage_liabilities
CREATE INDEX IF NOT EXISTS idx_mortgage_liabilities_user_id    ON mortgage_liabilities(user_id);
CREATE INDEX IF NOT EXISTS idx_mortgage_liabilities_account_id ON mortgage_liabilities(account_id);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_mortgage_liabilities_updated_at'
    ) THEN
        CREATE TRIGGER update_mortgage_liabilities_updated_at
            BEFORE UPDATE ON mortgage_liabilities
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE mortgage_liabilities IS 'Mortgage liability details from Plaid. One row per mortgage account.';
COMMENT ON COLUMN mortgage_liabilities.synced_at IS 'Timestamp of the most recent Plaid liabilities sync for this record';
COMMENT ON COLUMN mortgage_liabilities.interest_rate_type IS 'Mortgage rate type: fixed, variable, etc.';
COMMENT ON COLUMN mortgage_liabilities.has_pmi IS 'Whether the mortgage requires private mortgage insurance';
COMMENT ON COLUMN mortgage_liabilities.has_prepayment_penalty IS 'Whether the mortgage has a prepayment penalty clause';
COMMENT ON COLUMN mortgage_liabilities.ytd_interest_paid IS 'Year-to-date interest paid on this mortgage';
COMMENT ON COLUMN mortgage_liabilities.ytd_principal_paid IS 'Year-to-date principal paid on this mortgage';

-- ============================================================
-- 4. student_loan_liabilities
-- ============================================================
CREATE TABLE IF NOT EXISTS student_loan_liabilities (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id    UUID NOT NULL,
    account_id UUID NOT NULL,

    -- Loan identification
    account_number      VARCHAR(100),
    loan_name           VARCHAR(255),
    sequence_number     VARCHAR(100),
    guarantor           VARCHAR(255),
    payment_reference_number VARCHAR(255),

    -- Disbursement
    disbursement_dates DATE[],

    -- Loan status
    loan_status_type     VARCHAR(100),
    loan_status_end_date DATE,
    is_overdue           BOOLEAN,

    -- Financial data
    interest_rate_percentage      DECIMAL(8, 4),
    last_payment_amount           DECIMAL(18, 2),
    last_payment_date             DATE,
    last_statement_issue_date     DATE,
    last_statement_balance        DECIMAL(18, 2),
    minimum_payment_amount        DECIMAL(18, 2),
    next_payment_due_date         DATE,
    origination_date              DATE,
    origination_principal_amount  DECIMAL(18, 2),
    outstanding_interest_amount   DECIMAL(18, 2),
    expected_payoff_date          DATE,

    -- PSLF (Public Service Loan Forgiveness) tracking
    pslf_estimated_eligibility_date DATE,
    pslf_payments_made              INTEGER,
    pslf_payments_remaining         INTEGER,

    -- Repayment plan
    repayment_plan_type        VARCHAR(100),
    repayment_plan_description VARCHAR(500),

    -- Servicer address
    servicer_address_street      VARCHAR(500),
    servicer_address_city        VARCHAR(255),
    servicer_address_region      VARCHAR(100),
    servicer_address_postal_code VARCHAR(20),
    servicer_address_country     VARCHAR(10),

    -- Year-to-date totals
    ytd_interest_paid  DECIMAL(18, 2),
    ytd_principal_paid DECIMAL(18, 2),

    -- Timestamps
    synced_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- One liability record per account
    CONSTRAINT unique_student_loan_liability_account UNIQUE (account_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
DO $$ BEGIN
    ALTER TABLE student_loan_liabilities ADD CONSTRAINT fk_student_loan_liabilities_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE student_loan_liabilities VALIDATE CONSTRAINT fk_student_loan_liabilities_user_id;
DO $$ BEGIN
    ALTER TABLE student_loan_liabilities ADD CONSTRAINT fk_student_loan_liabilities_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE student_loan_liabilities VALIDATE CONSTRAINT fk_student_loan_liabilities_account_id;

-- Indexes for student_loan_liabilities
CREATE INDEX IF NOT EXISTS idx_student_loan_liabilities_user_id    ON student_loan_liabilities(user_id);
CREATE INDEX IF NOT EXISTS idx_student_loan_liabilities_account_id ON student_loan_liabilities(account_id);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_student_loan_liabilities_updated_at'
    ) THEN
        CREATE TRIGGER update_student_loan_liabilities_updated_at
            BEFORE UPDATE ON student_loan_liabilities
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE student_loan_liabilities IS 'Student loan liability details from Plaid. One row per student loan account.';
COMMENT ON COLUMN student_loan_liabilities.synced_at IS 'Timestamp of the most recent Plaid liabilities sync for this record';
COMMENT ON COLUMN student_loan_liabilities.disbursement_dates IS 'Array of dates on which loan funds were disbursed';
COMMENT ON COLUMN student_loan_liabilities.pslf_estimated_eligibility_date IS 'Estimated date of PSLF (Public Service Loan Forgiveness) eligibility';
COMMENT ON COLUMN student_loan_liabilities.pslf_payments_made IS 'Number of qualifying payments made toward PSLF';
COMMENT ON COLUMN student_loan_liabilities.pslf_payments_remaining IS 'Number of qualifying payments remaining to reach PSLF eligibility';
COMMENT ON COLUMN student_loan_liabilities.loan_status_type IS 'Current repayment status (e.g. in_repayment, deferment, forbearance, default)';
COMMENT ON COLUMN student_loan_liabilities.ytd_interest_paid IS 'Year-to-date interest paid on this student loan';
COMMENT ON COLUMN student_loan_liabilities.ytd_principal_paid IS 'Year-to-date principal paid on this student loan';

-- ============================================================
-- 5. Add liabilities_last_synced_at to linked_items
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'liabilities_last_synced_at'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN liabilities_last_synced_at TIMESTAMP WITH TIME ZONE;
        COMMENT ON COLUMN linked_items.liabilities_last_synced_at IS 'Timestamp of the most recent Plaid liabilities sync. NULL = never synced.';
    END IF;
END $$;
