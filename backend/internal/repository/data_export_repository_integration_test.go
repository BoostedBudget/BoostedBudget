//go:build integration

// data_export_repository_integration_test.go — DB-backed behaviour for the GDPR Article 20 export
// (#1587).
//
// Every property this file pins is one pgxmock structurally cannot see. pgxmock matches the query
// TEXT, so it would happily approve an ownership predicate that reaches a household peer's rows, an
// ORDER BY on a column that no longer exists, and an export that hands the user a base64 blob where
// their email should be. Two of those were real defects in this registry's first draft.
//
// Run:
//
//	DATABASE_URL="postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable" \
//	  go test -tags integration -p 1 ./internal/repository/ -run TestExportUserData_Integration -v
package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realFieldEncryptor builds the PRODUCTION AES-256-GCM encryptor, not a passthrough stub.
//
// ⚠ THIS MATTERS AND IS NOT INTERCHANGEABLE WITH newTestFieldEncryptor. A fixture written through a
// passthrough encryptor stores plaintext in an encrypted column, so the export's decrypt step is
// never exercised: the assertion "email is readable" passes on a row production cannot produce, and
// the predicate under test is dead. Exporting ciphertext to the user is the exact defect this file
// exists to rule out, so the ciphertext has to be real.
func realFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	svc, err := services.NewEncryptionService(&config.Config{
		EncryptionKey: "12345678901234567890123456789012",
	})
	require.NoError(t, err, "build the production AES-256-GCM encryption service")
	enc, err := appcrypto.NewFieldEncryptor(svc, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return enc
}

// exportUserThroughProductionWritePath creates a user with ProfileRepository.CreateUserTx — the
// same call the registration flow makes — so email and clerk_user_id land in the database as
// "ENC$"+ciphertext exactly as they do in production.
func exportUserThroughProductionWritePath(t *testing.T, enc *appcrypto.FieldEncryptor, suffix string) *domain.User {
	t.Helper()
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewProfileRepository(pool, enc)

	user := &domain.User{
		ClerkUserID:        "clerk_export_" + suffix,
		Email:              "export-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user
}

// everyExportedValue flattens the whole export into strings, so a leak assertion can be made across
// ALL ~49 sections at once rather than against the handful a test author happened to think of. A
// per-section assertion is precisely how a leak through a section nobody considered survives.
func everyExportedValue(t *testing.T, export *domain.DataExport) []string {
	t.Helper()
	raw, err := json.Marshal(export.Sections)
	require.NoError(t, err)
	var values []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for _, inner := range typed {
				walk(inner)
			}
		case []any:
			for _, inner := range typed {
				walk(inner)
			}
		case string:
			values = append(values, typed)
		case json.Number:
			values = append(values, typed.String())
		}
	}
	var decoded any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&decoded))
	walk(decoded)
	return values
}

// TestExportUserData_Integration_DoesNotLeakAnotherHouseholdMembersData is THE tenancy proof.
//
// Household sharing makes a peer's accounts and transactions VISIBLE to this user inside the
// product. Visibility is not ownership: GDPR Article 20(4) says portability must not adversely
// affect the rights and freedoms of others, so the export is the user's OWN rows and nothing else.
// The rule applied throughout the registry is that every ownership predicate is rooted at this
// user's id and never traverses household_members or household_account_shares to reach a row.
//
// Both members are set up with a full account chain and transactions, and both accounts are SHARED
// into the household, so a predicate that scoped by household_id instead of by user would pass a
// weaker test and fail this one.
func TestExportUserData_Integration_DoesNotLeakAnotherHouseholdMembersData(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewDataExportRepository(pool, newTestFieldEncryptor(t))

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	insertTestHouseholdMember(t, pool, hh, owner, "owner")
	insertTestHouseholdMember(t, pool, hh, member, "member")

	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 50, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	export, err := repo.ExportUserData(ctx, owner)
	require.NoError(t, err)

	values := everyExportedValue(t, export)
	forbidden := map[string]string{
		member:                    "the peer's user id",
		memberAcct.AccountID:      "the peer's account id",
		memberAcct.UserAccountID:  "the peer's user_account id",
		memberAcct.LinkedItemID:   "the peer's linked_item id",
	}
	for value, what := range forbidden {
		for _, exported := range values {
			assert.NotEqual(t, value, exported,
				"the owner's export contains %s (%s) — household VISIBILITY was mistaken for "+
					"ownership somewhere in the registry (GDPR Article 20(4))", what, value)
		}
	}

	// ...and the export is not vacuously clean: the owner's OWN rows are present, so an
	// export that returned nothing at all could not pass this test.
	require.Contains(t, values, ownerAcct.AccountID, "the owner's own account must be exported")
	require.NotEmpty(t, export.Sections["transactions"], "the owner's own transactions must be exported")

	// The household record itself IS the owner's data (they belong to it), but the peer who owns it
	// with them is not: owner_user_id is excluded, and the peer's membership row is not exported.
	require.NotEmpty(t, export.Sections["households"], "the household the user belongs to must be exported")
	for _, row := range export.Sections["households"] {
		_, present := row["owner_user_id"]
		assert.False(t, present, "households.owner_user_id identifies a data subject who may not be this user")
	}
	require.Len(t, export.Sections["household_members"], 1,
		"only the exporting user's OWN membership row belongs in their export")
}

// TestExportUserData_Integration_DecryptsAtRestColumns.
//
// users.email is stored as "ENC$"+ciphertext. Handing the user that string would satisfy every
// structural check — the field is present, non-empty, a string — and be completely useless to them:
// unreadable, and a copy of a value they cannot decrypt. The fixture is written through
// ProfileRepository.CreateUserTx with the real AES encryptor, so the row under test is one
// production actually produces.
func TestExportUserData_Integration_DecryptsAtRestColumns(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	user := exportUserThroughProductionWritePath(t, enc, fmt.Sprintf("dec%d", time.Now().UnixNano()))

	// The fixture really is encrypted at rest — otherwise the decrypt step below is untested.
	var stored string
	require.NoError(t, pool.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", user.ID).Scan(&stored))
	require.True(t, strings.HasPrefix(stored, "ENC$"),
		"the fixture wrote plaintext into an encrypted column, so this test would prove nothing")

	repo := repository.NewDataExportRepository(pool, enc)
	export, err := repo.ExportUserData(ctx, user.ID)
	require.NoError(t, err)

	require.Len(t, export.Sections["users"], 1)
	row := export.Sections["users"][0]
	assert.Equal(t, user.Email, row["email"], "the user must receive their email, not its ciphertext")
	assert.NotContains(t, fmt.Sprint(row["email"]), "ENC$")
}

// TestExportUserData_Integration_WithholdsSecretsAndAuthIdentifiers.
//
// clerk_user_id is a third-party authentication identifier, and every *_blind_idx column is an HMAC
// derived from the master encryption key. Neither is data the user provided or can act on, and the
// blind indexes are derived from a secret. The assertion sweeps EVERY section, so a secret reaching
// the user through some other table is caught too.
func TestExportUserData_Integration_WithholdsSecretsAndAuthIdentifiers(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	user := exportUserThroughProductionWritePath(t, enc, fmt.Sprintf("sec%d", time.Now().UnixNano()))

	repo := repository.NewDataExportRepository(pool, enc)
	export, err := repo.ExportUserData(ctx, user.ID)
	require.NoError(t, err)

	require.Len(t, export.Sections["users"], 1)
	row := export.Sections["users"][0]
	_, hasClerk := row["clerk_user_id"]
	assert.False(t, hasClerk, "the Clerk authentication identifier must not be exported")

	for section, rows := range export.Sections {
		for _, r := range rows {
			for column := range r {
				assert.False(t, strings.HasSuffix(column, "_blind_idx"),
					"%s.%s is a blind index — an HMAC derived from the master encryption key", section, column)
				assert.NotEqual(t, "access_token_encrypted", column,
					"%s exports a Plaid access token", section)
				assert.NotEqual(t, "token_hash", column,
					"%s exports an invite bearer-token hash", section)
			}
		}
	}
}

// TestExportUserData_Integration_PreservesMonetaryPrecision.
//
// The money columns are NUMERIC. Decoding jsonb through Go's default float64 silently re-rounds
// values on the way out of the building — an export that quietly changes the user's numbers is a
// worse answer than no export. json.Number keeps the digits Postgres emitted.
func TestExportUserData_Integration_PreservesMonetaryPrecision(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewDataExportRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	acct := setupHouseholdMemberAccount(t, pool, user)
	// A value float64 cannot hold exactly.
	insertHouseholdTransaction(t, pool, user, acct.AccountID, acct.LinkedItemID, 1234567.89, time.Now().UTC().AddDate(0, 0, -1), "FOOD_AND_DRINK")

	export, err := repo.ExportUserData(ctx, user)
	require.NoError(t, err)
	require.Len(t, export.Sections["transactions"], 1)

	amount := export.Sections["transactions"][0]["amount"]
	number, ok := amount.(json.Number)
	require.True(t, ok, "amount decoded as %T, not json.Number — float64 rounding is back", amount)

	var stored string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT amount::text FROM transactions WHERE user_id = $1", user).Scan(&stored))
	assert.Equal(t, stored, number.String(),
		"the exported amount must be the digits Postgres holds, not a float64 round-trip of them")
}

// TestExportUserData_Integration_ReportsEmptySectionsAndExclusions.
//
// A brand-new user must get a document that says "we hold nothing here" for every table, rather than
// a document with the tables missing — those are different answers to a subject access request, and
// only one of them is checkable. The withheld-table list ships in the payload for the same reason:
// an exclusion the data subject cannot see is an exclusion only we can audit.
func TestExportUserData_Integration_ReportsEmptySectionsAndExclusions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewDataExportRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	export, err := repo.ExportUserData(ctx, user)
	require.NoError(t, err)

	assert.Equal(t, domain.DataExportVersion, export.ExportVersion)
	assert.Equal(t, user, export.UserID)
	assert.Len(t, export.Sections, len(repository.DataExportTables),
		"every registered table must be present, including the ones with no rows")
	for _, name := range repository.DataExportTableNames() {
		section, present := export.Sections[name]
		require.True(t, present, "section %q is missing entirely", name)
		require.NotNil(t, section, "section %q is null; an empty section must serialise as []", name)
	}
	assert.Equal(t, len(repository.DataExportWithheldTables), len(export.ExcludedTables))
	assert.Equal(t, 1, export.RowCount, "the users row itself is the only row a new account has")
}
