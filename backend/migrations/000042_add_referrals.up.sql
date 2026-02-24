-- Referral codes table
CREATE TABLE IF NOT EXISTS referral_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    code VARCHAR(20) NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE referral_codes ADD CONSTRAINT fk_referral_codes_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE referral_codes VALIDATE CONSTRAINT fk_referral_codes_user;
CREATE INDEX IF NOT EXISTS idx_referral_codes_user_id ON referral_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_referral_codes_code ON referral_codes(code) WHERE is_active = TRUE;

-- Referrals table (tracks successful referrals)
CREATE TABLE IF NOT EXISTS referrals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    referrer_id UUID NOT NULL,
    referred_id UUID NOT NULL,
    referral_code_id UUID,
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, completed, expired, cancelled
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT uq_referrals_referred UNIQUE (referred_id) -- Each user can only be referred once
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_referrer FOREIGN KEY (referrer_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE referrals VALIDATE CONSTRAINT fk_referrals_referrer;
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_referred FOREIGN KEY (referred_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE referrals VALIDATE CONSTRAINT fk_referrals_referred;
ALTER TABLE referrals ADD CONSTRAINT fk_referrals_referral_code_id FOREIGN KEY (referral_code_id) REFERENCES referral_codes(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE referrals VALIDATE CONSTRAINT fk_referrals_referral_code_id;
CREATE INDEX IF NOT EXISTS idx_referrals_referrer_id ON referrals(referrer_id);
CREATE INDEX IF NOT EXISTS idx_referrals_status ON referrals(status);
