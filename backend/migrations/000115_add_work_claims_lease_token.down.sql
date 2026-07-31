-- #1413: Revert -- drop lease_token from work_claims.

ALTER TABLE work_claims DROP COLUMN IF EXISTS lease_token;
