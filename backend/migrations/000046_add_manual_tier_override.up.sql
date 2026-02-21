-- Add manual tier override columns to users table
DO $$ 
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'users' AND column_name = 'manual_tier_override'
    ) THEN
        ALTER TABLE users ADD COLUMN manual_tier_override VARCHAR(50);
        COMMENT ON COLUMN users.manual_tier_override IS 'Manual subscription tier override (e.g., premium)';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'users' AND column_name = 'manual_tier_ends_at'
    ) THEN
        ALTER TABLE users ADD COLUMN manual_tier_ends_at TIMESTAMPTZ;
        COMMENT ON COLUMN users.manual_tier_ends_at IS 'When the manual tier override expires, NULL for permanent';
    END IF;
END $$;
