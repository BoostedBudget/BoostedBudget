-- PostgreSQL cannot mark a validated CHECK constraint NOT VALID again. Validation does not change
-- which new rows the constraint accepts, so rollback leaves enforcement in place; migration 000209
-- removes both constraints when the schema change itself is rolled back.
SELECT 1;
