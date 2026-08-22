package database

// Unit tests for BackfillUserAccountIDs multiple-match path (Issue #1334).
//
// Uses pgxmock to simulate a database returning two rows for the same
// (user_id, institution_id_blind_idx, mask_blind_idx) triple — possible before
// migration 000100 enforces uniqueness. Verifies that:
//   - The backfill does not return an error (does not abort)
//   - A WARN is emitted via slog when count > 1
//   - The first row (deterministic ORDER BY created_at ASC, id ASC) is used

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// newBackfillTestEnc2 creates a deterministic FieldEncryptor for unit tests in this file.
// Named to avoid conflict with newBackfillTestEnc in the integration test file (build-tag guarded).
func newBackfillTestEnc2(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	svc := &backfillTestEncryptionSvc2{}
	key := []byte("12345678901234567890123456789012")
	enc, err := appcrypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	return enc
}

type backfillTestEncryptionSvc2 struct{}

func (s *backfillTestEncryptionSvc2) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *backfillTestEncryptionSvc2) Decrypt(ciphertext string) (string, error) {
	if len(ciphertext) > 5 && ciphertext[:5] == "TENC[" && ciphertext[len(ciphertext)-1] == ']' {
		return ciphertext[5 : len(ciphertext)-1], nil
	}
	return ciphertext, nil
}

// TestBackfillUserAccountIDs_MultipleMatch_Warns verifies that when multiple
// user_accounts rows match the same blind-index triple, the backfill:
//   - does NOT return an error
//   - emits a slog WARN
//   - uses the first row's ID (ORDER BY created_at ASC, id ASC picks the winner)
func TestBackfillUserAccountIDs_MultipleMatch_Warns(t *testing.T) {
	enc := newBackfillTestEnc2(t)
	ctx := context.Background()

	// Capture slog output by replacing the default logger temporarily.
	var logBuf bytes.Buffer
	origLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(testLogger)
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	// Build a pgxmock that returns:
	// 1. First Query (accounts scan): one account row with null user_account_id.
	// 2. Second Query (user_account lookup): two rows — mock LIMIT 2 path.
	// 3. Exec (UPDATE accounts): success.
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Encrypt institution_id and mask as the backfill code will decrypt them.
	plainInstID := "ins-test-multi"
	plainMask := "1234"
	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	// Compute expected blind indexes (same as backfill code will compute).
	instBlind := enc.BlindIndex(plainInstID)
	maskBlind := enc.BlindIndex(plainMask)

	// First mock: the accounts scan query.
	// mask and institution_id are *string (nullable) — pass pointers so pgxmock scans them correctly.
	encMaskPtr := &encMask
	encInstIDPtr := &encInstID
	// #1569 widened this SELECT with a.type/a.subtype so a synthesised identity is not typeless.
	acctType, acctSubtype := "depository", "checking"
	accountRows := pgxmock.NewRows([]string{"id", "user_id", "mask", "institution_id", "type", "subtype"}).
		AddRow(accountID, userID, encMaskPtr, encInstIDPtr, &acctType, &acctSubtype)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT a.id, a.user_id, a.mask, li.institution_id`)).
		WillReturnRows(accountRows)

	// Second mock: the user_accounts lookup — returns TWO rows (the multiple-match scenario).
	uaID1 := uuid.New().String() // this is the "first" row (ORDER BY created_at ASC, id ASC winner)
	uaID2 := uuid.New().String() // this is the "second" row (discarded)
	uaRows := pgxmock.NewRows([]string{"id"}).
		AddRow(uaID1).
		AddRow(uaID2)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM user_accounts`)).
		WithArgs(userID, instBlind, maskBlind).
		WillReturnRows(uaRows)

	// Third mock: the UPDATE accounts exec — must use uaID1 (the first row).
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE accounts SET user_account_id`)).
		WithArgs(uaID1, accountID).
		WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))

	// Run backfillAccountsUserAccountIDsWithDB — the refactored helper that accepts the interface.
	err = backfillAccountsUserAccountIDsWithDB(ctx, mock, enc)

	// Should NOT error.
	require.NoError(t, err, "backfill must not return an error on multiple matches")

	// Should have logged a WARN about multiple matches.
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "WARN", "slog must emit a WARN for multiple matches")
	assert.Contains(t, logOutput, "multiple user_accounts match", "WARN message must mention multiple matches")

	// Verify all mock expectations were met (confirms uaID1 was used, not uaID2).
	require.NoError(t, mock.ExpectationsWereMet())
}
