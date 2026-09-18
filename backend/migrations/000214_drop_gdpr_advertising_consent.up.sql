-- Issue #1683: the server-side advertising consent concept is removed; the
-- cookie banner keeps its advertising key but nothing server-side reads it.
ALTER TABLE privacy_settings DROP COLUMN IF EXISTS gdpr_advertising_consent;
