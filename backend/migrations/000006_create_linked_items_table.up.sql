-- Create linked_items table for storing Plaid item connections
-- Phase 2.2: Account Linking Service
CREATE TABLE IF NOT EXISTS linked_items (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign key to users
    user_id UUID NOT NULL,

    -- Plaid item information
    item_id VARCHAR(255) NOT NULL,
    access_token_encrypted TEXT NOT NULL,  -- Encrypted Plaid access token
    institution_id VARCHAR(255),
    institution_name VARCHAR(255),

    -- Transaction sync metadata
    cursor TEXT,  -- Plaid cursor for incremental transaction sync
    last_synced_at TIMESTAMP WITH TIME ZONE,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_user_item UNIQUE (user_id, item_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE linked_items ADD CONSTRAINT fk_linked_items_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE linked_items VALIDATE CONSTRAINT fk_linked_items_user_id;
-- Indexes for performance
CREATE INDEX idx_linked_items_user_id ON linked_items(user_id);
CREATE INDEX idx_linked_items_item_id ON linked_items(item_id);
CREATE INDEX idx_linked_items_institution_id ON linked_items(institution_id);

-- Trigger to automatically update updated_at
CREATE TRIGGER update_linked_items_updated_at
    BEFORE UPDATE ON linked_items
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE linked_items IS 'Plaid items (bank connections) linked to user accounts';
COMMENT ON COLUMN linked_items.access_token_encrypted IS 'AES-256-GCM encrypted Plaid access token (base64 encoded)';
COMMENT ON COLUMN linked_items.cursor IS 'Cursor for incremental transaction sync pagination';
