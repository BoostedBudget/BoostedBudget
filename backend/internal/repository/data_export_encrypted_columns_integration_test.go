//go:build integration

// data_export_encrypted_columns_integration_test.go — the export must contain the user's data, not
// this application's at-rest ciphertext (#1587).
//
// WHY THIS FILE EXISTS SEPARATELY FROM data_export_repository_integration_test.go. That file proved
// the point for ONE column, users.email, with a per-column assertion. A per-column assertion can
// only find the columns its author thought of, and the author thought of one: `accounts.mask`,
// `accounts.official_name`, `transactions.external_transaction_id`,
// `transactions.external_metadata`, every mortgage and student-loan address field,
// `paddle_transactions.payload`/`subscription_id`/`customer_id`,
// `user_identities.provider_user_id` and `notifications.action_url` all shipped as unreadable
// "ENC$…" strings while that test stayed green. The secret-shaped guard could not see them either:
// its column pattern is token|secret|password|clerk|encrypt|cipher|blind_idx|hash, and not one of
// those column names matches it.
//
// So the assertion here is not per column. It walks the WHOLE export document and fails on ANY
// string still carrying the "ENC$" envelope, in any section, from any table — the same shape of
// derived, universe-wide check the other #1587 guards use. A newly encrypted column added by a
// later migration is covered on the day it lands, with no list to widen.
//
// ⚠ EVERY FIXTURE BELOW IS WRITTEN THROUGH ITS PRODUCTION REPOSITORY WITH THE REAL AES ENCRYPTOR.
// A raw INSERT of plaintext into an encrypted column produces a row production cannot produce, the
// decrypt step is then never exercised, and the assertion passes vacuously. That exact defect has
// already shipped once on this branch.
//
// Run:
//
//	DATABASE_URL="postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable" \
//	  go test -tags integration -p 1 ./internal/repository/ -run EncryptedColumns -v
package repository_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// encryptedFixture records the plaintext each production write path was handed, so the assertions
// can demand the export gives that exact value back rather than merely "something without ENC$".
type encryptedFixture struct {
	userID string

	institutionName       string
	userAccountHint       string
	accountName           string
	officialName          string
	accountMask           string
	externalTransactionID string
	externalMetadata      string
	mortgageStreet        string
	studentLoanGuarantor  string
	paddleTransactionID   string
	paddleSubscriptionID  string
	paddleCustomerID      string
	providerUserID        string
	notificationActionURL string
}

func strp(s string) *string { return &s }

// seedEncryptedColumnsThroughProductionWritePaths builds one user with a row in every exported
// table that holds an encrypted column, using the repositories the application itself uses.
func seedEncryptedColumnsThroughProductionWritePaths(
	t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor,
) encryptedFixture {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("enc%d", time.Now().UnixNano())
	user := exportUserThroughProductionWritePath(t, enc, suffix)

	fx := encryptedFixture{
		userID:                user.ID,
		institutionName:       "First Bank of " + suffix,
		userAccountHint:       "Joint chequing " + suffix,
		accountName:           "Everyday " + suffix,
		officialName:          "Premier Interest Checking " + suffix,
		accountMask:           "4417",
		externalTransactionID: "plaid-txn-" + suffix,
		externalMetadata:      `{"category_id":"13005000","note":"` + suffix + `"}`,
		mortgageStreet:        "42 Acacia Avenue, " + suffix,
		studentLoanGuarantor:  "Guarantor " + suffix,
		paddleTransactionID:   "txn_" + suffix,
		paddleSubscriptionID:  "sub_" + suffix,
		paddleCustomerID:      "ctm_" + suffix,
		providerUserID:        "google-oauth2|" + suffix,
		notificationActionURL: "https://app.boostedbudget.com/accounts/reconnect?item=" + suffix,
	}

	// --- user_identities.provider_user_id / email -----------------------------------------------
	identityRepo := repository.NewUserIdentityRepository(pool, enc)
	require.NoError(t, identityRepo.Create(ctx, &domain.UserIdentity{
		UserID:         user.ID,
		ClerkUserID:    "clerk_identity_" + suffix,
		Provider:       "google-oauth2",
		ProviderUserID: fx.providerUserID,
		Email:          user.Email,
		EmailVerified:  true,
		IsPrimary:      true,
	}), "seed user_identities through UserIdentityRepository.Create")

	// --- linked_items.institution_name / user_accounts.account_name_hint ------------------------
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	item := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          "item-" + suffix,
		AccessToken:     "access-sandbox-" + suffix,
		InstitutionID:   "ins_" + suffix,
		InstitutionName: fx.institutionName,
		Provider:        "plaid",
		Status:          domain.LinkedItemStatusActive,
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item), "seed linked_items through LinkedItemRepository.Create")

	userAccountRepo := repository.NewUserAccountRepository(pool, enc)
	userAccount := &domain.UserAccount{
		UserID:          user.ID,
		InstitutionID:   "ins_" + suffix,
		Mask:            strp(fx.accountMask),
		AccountNameHint: strp(fx.userAccountHint),
	}
	require.NoError(t, userAccountRepo.Create(ctx, userAccount), "seed user_accounts through UserAccountRepository.Create")

	// --- accounts.official_name / mask ----------------------------------------------------------
	accountRepo := repository.NewAccountRepository(pool, enc)
	account := domain.Account{
		LinkedItemID:   item.ID,
		UserID:         user.ID,
		UserAccountID:  &userAccount.ID,
		PlaidAccountID: "plaid-acct-" + suffix,
		Name:           fx.accountName,
		OfficialName:   strp(fx.officialName),
		Type:           "depository",
		Subtype:        strp("checking"),
		Mask:           strp(fx.accountMask),
		CurrencyCode:   "USD",
	}
	require.NoError(t, accountRepo.CreateBatch(ctx, user.ID, []domain.Account{account}),
		"seed accounts through AccountRepository.CreateBatch")
	var accountID string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT id::text FROM accounts WHERE user_id = $1", user.ID).Scan(&accountID))

	// --- transactions.external_transaction_id / external_metadata -------------------------------
	transactionRepo := repository.NewTransactionRepository(pool, enc)
	_, err := transactionRepo.CreateBatch(ctx, []domain.Transaction{{
		UserID:                user.ID,
		AccountID:             accountID,
		LinkedItemID:          item.ID,
		UserAccountID:         &userAccount.ID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: strp(fx.externalTransactionID),
		ExternalMetadata:      strp(fx.externalMetadata),
		Amount:                12.34,
		Date:                  time.Now().UTC().AddDate(0, 0, -3),
		Name:                  "Coffee",
	}})
	require.NoError(t, err, "seed transactions through TransactionRepository.CreateBatch")

	// --- mortgage / student-loan address and account fields -------------------------------------
	mortgageRepo := repository.NewMortgageLiabilityRepository(pool, enc)
	require.NoError(t, mortgageRepo.ReplaceForAccount(ctx, accountID, user.ID, &domain.MortgageLiability{
		UserID:                user.ID,
		AccountID:             accountID,
		AccountNumber:         strp("mortgage-acct-" + suffix),
		PropertyAddressStreet: strp(fx.mortgageStreet),
		PropertyAddressCity:   strp("Springfield"),
	}), "seed mortgage_liabilities through MortgageLiabilityRepository.ReplaceForAccount")

	studentLoanRepo := repository.NewStudentLoanLiabilityRepository(pool, enc)
	require.NoError(t, studentLoanRepo.ReplaceForAccount(ctx, accountID, user.ID, &domain.StudentLoanLiability{
		UserID:        user.ID,
		AccountID:     accountID,
		AccountNumber: strp("loan-acct-" + suffix),
		Guarantor:     strp(fx.studentLoanGuarantor),
		LoanName:      strp("Undergraduate " + suffix),
	}), "seed student_loan_liabilities through StudentLoanLiabilityRepository.ReplaceForAccount")

	// --- paddle_transactions.payload / subscription_id / customer_id ----------------------------
	paddleRepo := repository.NewPaddleTransactionRepository(pool, enc)
	require.NoError(t, paddleRepo.Create(ctx, &domain.PaddleTransaction{
		UserID:         user.ID,
		TransactionID:  fx.paddleTransactionID,
		SubscriptionID: strp(fx.paddleSubscriptionID),
		CustomerID:     strp(fx.paddleCustomerID),
		Amount:         8.99,
		Currency:       "USD",
		Status:         "completed",
		Payload:        json.RawMessage(`{"event_id":"` + suffix + `","amount":"8.99"}`),
	}), "seed paddle_transactions through PaddleTransactionRepository.Create")

	// --- notifications.action_url ---------------------------------------------------------------
	notificationRepo := repository.NewNotificationRepository(pool, enc)
	require.NoError(t, notificationRepo.Create(ctx, &domain.Notification{
		UserID:             user.ID,
		Type:               "item_error",
		Title:              "Reconnect your bank",
		Message:            "We could not reach your bank.",
		ActionURL:          strp(fx.notificationActionURL),
		ActionLabel:        strp("Reconnect"),
		InteractionPattern: "action_required",
	}), "seed notifications through NotificationRepository.Create")

	return fx
}

// userTypedEnvelopeCollision is a string a user is entitled to type, paste or import that the
// structural envelope check cannot tell from a real one: "ENC$" followed by well-formed standard
// base64 of 40 bytes — comfortably over the 12-byte nonce + 16-byte tag floor isCiphertextEnvelope
// requires. Only the GCM tag can discriminate, so this value reaches the decryptor and fails
// authentication, which is exactly the path #1587's fix round exists to keep non-fatal.
//
// One definition, two tests: the dedicated collision test below, and the whole-document sweep,
// which plants it beside every real ciphertext so both directions are proved in one run.
func userTypedEnvelopeCollision() string {
	return "ENC$" + base64.StdEncoding.EncodeToString([]byte("this is forty bytes of ordinary user text"[:40]))
}

// ciphertextInExport returns "section.column" for every exported value still wearing the at-rest
// envelope. Walking the whole document is the point: a per-column assertion finds only the columns
// its author remembered, and the columns nobody remembered are the ones that shipped broken.
//
// `userTyped` lists values the FIXTURE itself wrote as user text — a string the user is entitled to
// type that happens to look like an envelope. They are matched by exact value, not by column, so
// the exemption cannot widen: real ciphertext in the same column is still reported, and a build
// that stopped decrypting is still caught by every genuinely encrypted column in the document.
func ciphertextInExport(export *domain.DataExport, userTyped ...string) []string {
	typed := make(map[string]struct{}, len(userTyped))
	for _, v := range userTyped {
		typed[v] = struct{}{}
	}
	var found []string
	for section, rows := range export.Sections {
		for _, row := range rows {
			for column, value := range row {
				text, ok := value.(string)
				if !ok || !strings.HasPrefix(text, "ENC$") {
					continue
				}
				if _, expected := typed[text]; expected {
					continue
				}
				found = append(found, section+"."+column)
			}
		}
	}
	sort.Strings(found)
	return found
}

// TestExportUserData_Integration_EncryptedColumnsAreReadable is the whole-document proof.
//
// #1587 exists because retained history could not leave the building. An export full of "ENC$…"
// leaves the building and is still unreadable — the same failure wearing a success message.
func TestExportUserData_Integration_EncryptedColumnsAreReadable(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	fx := seedEncryptedColumnsThroughProductionWritePaths(t, pool, enc)

	// The fixtures really are encrypted at rest. Without this the assertion below could pass on a
	// database that stores plaintext, proving nothing at all.
	encryptedAtRest := map[string]string{
		"SELECT official_name FROM accounts WHERE user_id = $1":                       "accounts.official_name",
		"SELECT external_metadata FROM transactions WHERE user_id = $1":               "transactions.external_metadata",
		"SELECT property_address_street FROM mortgage_liabilities WHERE user_id = $1": "mortgage_liabilities.property_address_street",
		"SELECT payload FROM paddle_transactions WHERE user_id = $1":                  "paddle_transactions.payload",
		"SELECT provider_user_id FROM user_identities WHERE user_id = $1":             "user_identities.provider_user_id",
		"SELECT action_url FROM notifications WHERE user_id = $1":                     "notifications.action_url",
	}
	for query, what := range encryptedAtRest {
		var stored string
		require.NoError(t, pool.QueryRow(ctx, query, fx.userID).Scan(&stored), "read %s at rest", what)
		require.True(t, strings.HasPrefix(stored, "ENC$"),
			"the fixture wrote plaintext into %s, so this test would prove nothing", what)
	}

	// A colliding user string lives in the SAME document as every real ciphertext. Both directions
	// of the sweep are therefore proved by one run: it must tolerate this value (a heuristic
	// collision is not a leak) and it must still report every genuinely encrypted column if the
	// decrypt step were dropped.
	collision := userTypedEnvelopeCollision()
	_, err := pool.Exec(ctx, "UPDATE transactions SET name = $2 WHERE user_id = $1", fx.userID, collision)
	require.NoError(t, err)

	repo := repository.NewDataExportRepository(pool, enc)
	export, err := repo.ExportUserData(ctx, fx.userID)
	require.NoError(t, err)

	leaks := ciphertextInExport(export, collision)
	assert.Empty(t, leaks,
		"%d exported column(s) hand the user this application's at-rest ciphertext instead of "+
			"their own data (#1587). An export of \"ENC$…\" is unusable to the data subject and "+
			"is still a copy of a secret-adjacent value:\n    %s",
		len(leaks), strings.Join(leaks, "\n    "))

	// ...and not vacuously: the plaintext that comes back is the plaintext that went in. An export
	// that dropped the encrypted columns entirely would satisfy the sweep above.
	requireExportedValue := func(section, column, want string) {
		t.Helper()
		require.NotEmpty(t, export.Sections[section], "section %q is empty, so its columns prove nothing", section)
		require.Equal(t, want, fmt.Sprint(export.Sections[section][0][column]),
			"%s.%s must be the user's own value", section, column)
	}
	requireExportedValue("accounts", "official_name", fx.officialName)
	requireExportedValue("accounts", "mask", fx.accountMask)
	requireExportedValue("accounts", "name", fx.accountName)
	requireExportedValue("user_accounts", "account_name_hint", fx.userAccountHint)
	requireExportedValue("linked_items", "institution_name", fx.institutionName)
	requireExportedValue("transactions", "external_transaction_id", fx.externalTransactionID)
	requireExportedValue("transactions", "external_metadata", fx.externalMetadata)
	requireExportedValue("mortgage_liabilities", "property_address_street", fx.mortgageStreet)
	requireExportedValue("student_loan_liabilities", "guarantor", fx.studentLoanGuarantor)
	requireExportedValue("paddle_transactions", "transaction_id", fx.paddleTransactionID)
	requireExportedValue("paddle_transactions", "subscription_id", fx.paddleSubscriptionID)
	requireExportedValue("paddle_transactions", "customer_id", fx.paddleCustomerID)
	requireExportedValue("user_identities", "provider_user_id", fx.providerUserID)
	requireExportedValue("notifications", "action_url", fx.notificationActionURL)

	// The Plaid access token is the one encrypted column that must NOT be decrypted: it is a live
	// bearer credential, and it is withheld outright rather than handed over in the clear.
	for _, row := range export.Sections["linked_items"] {
		_, present := row["access_token_encrypted"]
		assert.False(t, present, "the Plaid access token must never be exported, decrypted or not")
	}
}

// TestExportUserData_Integration_UserTypedEnvelopeMarkerSurvives.
//
// Decrypting by envelope rather than by column name asks a question of the VALUE, and free-text
// columns hold whatever the user typed. If "does it start with ENC$" were the whole test, a user
// whose transaction is named "ENC$ rent" would get a hard failure instead of an export — a denial
// of service against their own Article 20 request, caused by a string they were entitled to type.
// The envelope check is therefore structural: prefix AND standard base64 AND at least a nonce and a
// GCM tag of payload. `transactions.name` is not an encrypted column, so nothing here relies on the
// decrypt path; it relies on the decrypt path knowing to keep its hands off.
func TestExportUserData_Integration_UserTypedEnvelopeMarkerSurvives(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	fx := seedEncryptedColumnsThroughProductionWritePaths(t, pool, enc)

	const typed = "ENC$ rent, not a ciphertext"
	_, err := pool.Exec(ctx,
		"UPDATE transactions SET name = $2 WHERE user_id = $1", fx.userID, typed)
	require.NoError(t, err)

	repo := repository.NewDataExportRepository(pool, enc)
	export, err := repo.ExportUserData(ctx, fx.userID)
	require.NoError(t, err,
		"a user-typed string that merely starts with the envelope marker must not fail the export")
	require.NotEmpty(t, export.Sections["transactions"])
	assert.Equal(t, typed, fmt.Sprint(export.Sections["transactions"][0]["name"]),
		"the user's own text must come back exactly as they typed it")
}

// TestExportUserData_Integration_UserTypedEnvelopeCollisionSurvives.
//
// The test above exercises the EASY end of the property: "ENC$ rent, not a ciphertext" is not valid
// base64 and is far too short, so the structural check rejects it before decryption is attempted.
// That check is a HEURISTIC, and a heuristic has a collision set. `ENC$` followed by well-formed
// standard base64 decoding to at least nonce+tag bytes is a string a user can type, paste or
// import — a note, a merchant name, a description copied out of some other system's export — and
// for such a value the structural check says "envelope", AES-GCM authentication says "not mine",
// and #1587's original code turned that disagreement into a 500 for the WHOLE export. One
// unlucky string in one row and the data subject cannot exercise Article 20 at all.
//
// The discriminator that cannot collide is the GCM tag itself: only the holder of this
// application's key can produce a value that authenticates under it. So authentication — not the
// prefix, not the base64 shape — decides, and a value that fails it is by construction not
// something this application encrypted, and is exported exactly as the user typed it.
//
// ⚠ The fallback is NOT unconditional, and TestExportUserData_Integration_EncryptedColumnsAreReadable
// is why: if the key were wrong or rotated, EVERY envelope would fail authentication and
// pass-through would hand the user a document of unreadable ciphertext, silently — the round-4
// defect, restored. See exportKeyHealthError.
func TestExportUserData_Integration_UserTypedEnvelopeCollisionSurvives(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	fx := seedEncryptedColumnsThroughProductionWritePaths(t, pool, enc)

	// Structurally indistinguishable from a real envelope, and authenticating under no key this
	// app holds — see userTypedEnvelopeCollision.
	typed := userTypedEnvelopeCollision()
	require.True(t, strings.HasPrefix(typed, "ENC$"))

	_, err := pool.Exec(ctx,
		"UPDATE transactions SET name = $2 WHERE user_id = $1", fx.userID, typed)
	require.NoError(t, err)

	repo := repository.NewDataExportRepository(pool, enc)
	export, err := repo.ExportUserData(ctx, fx.userID)
	require.NoError(t, err,
		"a user-typed string that merely LOOKS like an envelope must not fail the whole export")
	require.NotEmpty(t, export.Sections["transactions"])
	assert.Equal(t, typed, fmt.Sprint(export.Sections["transactions"][0]["name"]),
		"the user's own text must come back exactly as they typed it")

	// And the genuinely encrypted columns in the same document are still decrypted — the fallback
	// must not have been reached by disabling decryption.
	require.NotEmpty(t, export.Sections["users"])
	assert.NotContains(t, fmt.Sprint(export.Sections["users"][0]["email"]), "ENC$",
		"real ciphertext in the same export must still be decrypted")
}

// TestExportUserData_Integration_WrongKeyStillFailsLoudly.
//
// The collision fallback above must not become "shrug at every authentication failure". If the
// encryption key is wrong or has been rotated out from under the data, EVERY envelope in the
// document fails to authenticate, and passing them all through would ship the round-4 defect back:
// a document of unreadable "ENC$…" strings handed over as if it were the user's own data, with a
// 200 on it. Nothing decrypting anywhere in a whole export is not an unlucky string; it is a broken
// key, and it is refused.
//
// This is the mutant test for exportKeyHealthError: delete the call in ExportUserData and this
// test is the one that goes red.
func TestExportUserData_Integration_WrongKeyStillFailsLoudly(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	enc := realFieldEncryptor(t)
	fx := seedEncryptedColumnsThroughProductionWritePaths(t, pool, enc)

	svc, err := services.NewEncryptionService(&config.Config{
		EncryptionKey: "abcdefghijklmnopqrstuvwxyz012345",
	})
	require.NoError(t, err)
	wrongKey, err := appcrypto.NewFieldEncryptor(svc, []byte("abcdefghijklmnopqrstuvwxyz012345"))
	require.NoError(t, err)

	repo := repository.NewDataExportRepository(pool, wrongKey)
	_, err = repo.ExportUserData(ctx, fx.userID)
	require.Error(t, err, "an export in which NOTHING decrypts must not be handed to the user")
	assert.Contains(t, err.Error(), "none succeeded",
		"the failure must name the key-health rule, not a single column: %v", err)
}
