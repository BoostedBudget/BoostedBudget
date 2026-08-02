-- #1413 P5: revert work_claims.resource_id from TEXT back to UUID.
--
-- Only safe if every row currently in the table holds a valid UUID string -- i.e. no recurring
-- action has ever been claimed with a composite "{userID}:{discriminator}" key. work_claims had
-- no production rows as of migration 000115 and this feature had not shipped as of this
-- migration being written, so this holds at rollback time in practice; a composite-keyed row
-- surviving to a real rollback fails this ALTER loudly ("invalid input syntax for type uuid"),
-- which is preferable to silently truncating or corrupting data.
--
-- Idempotent: only runs the ALTER when the column is still TEXT.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'work_claims'
          AND column_name = 'resource_id'
          AND data_type = 'text'
    ) THEN
        ALTER TABLE work_claims ALTER COLUMN resource_id TYPE UUID USING resource_id::UUID;
    END IF;
END $$;

COMMENT ON COLUMN work_claims.resource_id IS
    'The resource''s primary key (e.g. users.id). Not a foreign key: work_claims must outlive '
    'the resource it references (a claim can legitimately be the last thing written before a '
    'user row is deleted) and may in future cover non-user resources.';
