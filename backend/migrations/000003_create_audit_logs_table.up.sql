CREATE TABLE IF NOT EXISTS audit_logs (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- User who performed the action
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    -- NULL if system action or user deleted

    -- Action details
    action VARCHAR(100) NOT NULL,
    -- Examples: 'user.created', 'user.login', 'user.updated',
    --           'account.linked', 'rule.created', 'data.exported', 'user.deleted'

    resource_type VARCHAR(50) NOT NULL,
    -- Examples: 'user', 'account', 'budget', 'rule', 'transaction'

    resource_id UUID,
    -- ID of the resource affected (can be NULL for list operations)

    -- Request metadata
    ip_address INET,
    user_agent TEXT,
    request_id UUID,

    -- Change details (for update operations)
    changes JSONB,
    -- Store before/after state for updates

    -- Result
    status VARCHAR(20) NOT NULL DEFAULT 'success',
    -- Possible values: 'success', 'failure', 'error'

    error_message TEXT,
    -- If status is 'failure' or 'error'

    -- Timestamp
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT valid_status CHECK (status IN ('success', 'failure', 'error'))
);
-- Indexes for common queries
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX idx_audit_logs_resource_id ON audit_logs(resource_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_logs_status ON audit_logs(status) WHERE status != 'success';
-- Composite index for user activity timeline
CREATE INDEX idx_audit_logs_user_timeline ON audit_logs(user_id, created_at DESC);
-- Composite index for resource history
CREATE INDEX idx_audit_logs_resource_history ON audit_logs(resource_type, resource_id, created_at DESC);
-- Comments
COMMENT ON TABLE audit_logs IS 'GDPR-compliant audit trail of all data access and modifications';
COMMENT ON COLUMN audit_logs.action IS 'Action performed (e.g., user.created, data.exported)';
COMMENT ON COLUMN audit_logs.changes IS 'JSONB of before/after state for updates';
COMMENT ON COLUMN audit_logs.ip_address IS 'IP address of the request (for security)';
