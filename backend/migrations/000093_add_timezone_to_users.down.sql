-- #972: Revert — drop timezone and timezone_source from users.

ALTER TABLE users DROP COLUMN IF EXISTS timezone_source;
ALTER TABLE users DROP COLUMN IF EXISTS timezone;
