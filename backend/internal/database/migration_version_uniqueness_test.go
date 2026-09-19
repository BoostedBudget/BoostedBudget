package database

// Issue #1652 Todo 13 (v) — migration version uniqueness guard.
//
// WHY THIS EXISTS. A branch once carried four migrations numbered 000208-000211 that collided with
// four already on staging (000208-000213). Git merges by path, so the filenames differed and the
// merge reported no SQL conflict; the result was two migrations sharing each version number.
// golang-migrate keys its ledger by version, so it applies them in undefined order (or fails), and
// nothing else in the suite notices. This guard enumerates backend/migrations/*.sql and fails on any
// version used twice in the same direction.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var migrationVersionPattern = regexp.MustCompile(`^(\d{6})_.*\.(up|down)\.sql$`)

// duplicateMigrationVersions returns version+direction -> colliding filenames for every version
// number used by more than one migration in the same direction. A version with one .up.sql and one
// .down.sql is the normal pair, not a collision.
func duplicateMigrationVersions(dir string) (map[string][]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	grouped := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationVersionPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		key := match[1] + " " + match[2]
		grouped[key] = append(grouped[key], entry.Name())
	}
	duplicates := map[string][]string{}
	for key, files := range grouped {
		if len(files) > 1 {
			sort.Strings(files)
			duplicates[key] = files
		}
	}
	return duplicates, nil
}

func TestNoDuplicateMigrationVersionNumbers(t *testing.T) {
	duplicates, err := duplicateMigrationVersions(migrationsDir(t))
	require.NoError(t, err)
	assert.Empty(t, duplicates,
		"two migrations share a version number in the same direction; golang-migrate applies them in undefined order")
}

// TestDuplicateMigrationVersionDetection is the meta-test: it constructs the exact collision this
// guard exists to catch and proves the detection helper reports it.
func TestDuplicateMigrationVersionDetection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"000207_unique.up.sql",
		"000207_unique.down.sql",
		"000208_first.up.sql",
		"000208_first.down.sql",
		"000208_collision.up.sql",
		"000208_collision.down.sql",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;\n"), 0o644))
	}

	duplicates, err := duplicateMigrationVersions(dir)
	require.NoError(t, err)
	require.Len(t, duplicates, 2, "only the colliding 000208 pair directions should be reported")
	assert.Equal(t, []string{"000208_collision.up.sql", "000208_first.up.sql"}, duplicates["000208 up"])
	assert.Equal(t, []string{"000208_collision.down.sql", "000208_first.down.sql"}, duplicates["000208 down"])
	assert.NotContains(t, duplicates, "000207 up", "a single up/down pair is not a collision")
}

// TestDuplicateMigrationVersionDetectionIgnoresNonMigrations proves the scanner does not invent a
// collision from a file that is not a versioned migration.
func TestDuplicateMigrationVersionDetectionIgnoresNonMigrations(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"README.md", "000208_ok.up.sql", "000208_ok.down.sql", "notes.sql"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;\n"), 0o644))
	}

	duplicates, err := duplicateMigrationVersions(dir)
	require.NoError(t, err)
	assert.Empty(t, duplicates)
}
