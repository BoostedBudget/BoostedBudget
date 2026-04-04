// Command encrypt-migrate encrypts all existing plaintext PII data in the database.
//
// Run this tool AFTER deploying migration 000053 but BEFORE deploying the
// application code that switches to encrypted reads/writes.
//
// Usage:
//
//	encrypt-migrate                    # Encrypt all plaintext data
//	encrypt-migrate --decrypt          # Decrypt all data (for rollback)
//	encrypt-migrate --dry-run          # Show what would be encrypted without changes
//	encrypt-migrate --table=users      # Encrypt only a specific table
//
// The tool is idempotent: already-encrypted values (prefixed with "ENC$") are skipped.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

func main() {
	decryptMode := flag.Bool("decrypt", false, "Decrypt all data instead of encrypting (for rollback)")
	dryRun := flag.Bool("dry-run", false, "Show what would be changed without making changes")
	tableFilter := flag.String("table", "", "Only process this table (empty = all tables)")
	batchSize := flag.Int("batch-size", 1000, "Number of rows to process per batch")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	svc, err := services.NewEncryptionService(cfg)
	if err != nil {
		log.Fatal("Failed to create encryption service:", err)
	}

	enc, err := appcrypto.NewFieldEncryptor(svc, []byte(cfg.EncryptionKey))
	if err != nil {
		log.Fatal("Failed to create field encryptor:", err)
	}

	db, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	if err := db.Ping(context.Background()); err != nil {
		log.Fatal("Database ping failed:", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	mode := "encrypt"
	if *decryptMode {
		mode = "decrypt"
	}

	logger.Info("Starting data migration", "mode", mode, "dry_run", *dryRun, "batch_size", *batchSize)

	m := &migrator{
		db:          db,
		enc:         enc,
		decrypt:     *decryptMode,
		dryRun:      *dryRun,
		batchSize:   *batchSize,
		tableFilter: *tableFilter,
		logger:      logger,
	}

	tables := allTables()
	for _, t := range tables {
		if *tableFilter != "" && t.name != *tableFilter {
			continue
		}
		if err := m.processTable(context.Background(), t); err != nil {
			logger.Error("Failed to process table", "table", t.name, "error", err)
			os.Exit(1)
		}
	}

	logger.Info("Migration complete")
}

type migrator struct {
	db          *pgxpool.Pool
	enc         *appcrypto.FieldEncryptor
	decrypt     bool
	dryRun      bool
	batchSize   int
	tableFilter string
	logger      *slog.Logger
}

// tableSpec describes which columns in a table need encryption.
type tableSpec struct {
	name        string
	idCol       string // primary key column name
	stringCols  []string // plain string columns to encrypt
	nullableCols []string // nullable string columns to encrypt
	jsonbCols   []string // JSONB columns that are now TEXT blobs to encrypt
	blindCols   []blindColSpec // columns that also need a blind index
}

// blindColSpec describes a column + its corresponding blind index column.
type blindColSpec struct {
	col      string // the data column (being encrypted)
	blindIdx string // the blind index column name
	lower    bool   // whether to lowercase before computing blind index (for emails)
}

func allTables() []tableSpec {
	return []tableSpec{
		{
			name:  "linked_items",
			idCol: "id",
			stringCols: []string{"institution_name"},
			blindCols: []blindColSpec{
				{col: "item_id", blindIdx: "item_id_blind_idx"},
				{col: "institution_id", blindIdx: "institution_id_blind_idx"},
			},
		},
		{
			name:  "users",
			idCol: "id",
			blindCols: []blindColSpec{
				{col: "email", blindIdx: "email_blind_idx", lower: true},
				{col: "clerk_user_id", blindIdx: "clerk_user_id_blind_idx"},
			},
		},
		{
			name:       "user_identities",
			idCol:      "id",
			stringCols: []string{"email", "provider_user_id"},
		},
		{
			name:  "accounts",
			idCol: "id",
			nullableCols: []string{"official_name"},
			stringCols:   []string{"name"},
			blindCols: []blindColSpec{
				{col: "plaid_account_id", blindIdx: "plaid_account_id_blind_idx"},
				{col: "mask", blindIdx: ""}, // mask on accounts has no blind index (only user_accounts does)
			},
		},
		{
			name:  "user_accounts",
			idCol: "id",
			nullableCols: []string{"account_name_hint"},
			blindCols: []blindColSpec{
				{col: "institution_id", blindIdx: "institution_id_blind_idx"},
				{col: "mask", blindIdx: "mask_blind_idx"}, // nullable; handled specially
			},
		},
		{
			name:  "transactions",
			idCol: "id",
			jsonbCols: []string{"external_metadata"},
			blindCols: []blindColSpec{
				{col: "external_transaction_id", blindIdx: "external_transaction_id_blind_idx"},
			},
		},
		{
			name:         "mortgage_liabilities",
			idCol:        "id",
			nullableCols: []string{"account_number", "property_address_street", "property_address_city", "property_address_region", "property_address_postal_code", "property_address_country"},
		},
		{
			name:         "student_loan_liabilities",
			idCol:        "id",
			nullableCols: []string{"account_number", "servicer_address_street", "servicer_address_city", "servicer_address_region", "servicer_address_postal_code", "servicer_address_country", "payment_reference_number", "guarantor", "loan_name", "sequence_number"},
		},
		{
			name:      "webhook_events",
			idCol:     "id",
			jsonbCols: []string{"payload"},
			blindCols: []blindColSpec{
				{col: "item_id", blindIdx: "item_id_blind_idx"},
			},
		},
		{
			name:      "paddle_webhook_events",
			idCol:     "id",
			jsonbCols: []string{"payload"},
		},
		{
			name:      "paddle_transactions",
			idCol:     "id",
			jsonbCols: []string{"payload"},
			nullableCols: []string{"subscription_id", "customer_id"},
			blindCols: []blindColSpec{
				{col: "transaction_id", blindIdx: "transaction_id_blind_idx"},
			},
		},
		{
			name:      "audit_logs",
			idCol:     "id",
			jsonbCols: []string{"changes"},
			nullableCols: []string{"ip_address", "user_agent"},
		},
		{
			name:         "notifications",
			idCol:        "id",
			nullableCols: []string{"action_url"},
		},
	}
}

func (m *migrator) processTable(ctx context.Context, t tableSpec) error {
	m.logger.Info("Processing table", "table", t.name)

	// Collect all columns to select
	allCols := []string{t.idCol}
	allCols = append(allCols, t.stringCols...)
	allCols = append(allCols, t.nullableCols...)
	allCols = append(allCols, t.jsonbCols...)
	for _, b := range t.blindCols {
		// Avoid duplicates
		alreadyIncluded := false
		for _, c := range allCols {
			if c == b.col {
				alreadyIncluded = true
				break
			}
		}
		if !alreadyIncluded {
			allCols = append(allCols, b.col)
		}
	}

	totalProcessed := 0
	offset := 0
	for {
		query := fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY %s LIMIT %d OFFSET %d",
			strings.Join(allCols, ", "), t.name, t.idCol, m.batchSize, offset,
		)

		rows, err := m.db.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("query batch offset=%d: %w", offset, err)
		}

		batchCount := 0
		for rows.Next() {
			// Build a map of column → value via scanning
			values := make([]any, len(allCols))
			valuePtrs := make([]any, len(allCols))
			for i := range values {
				valuePtrs[i] = &values[i]
			}
			if err := rows.Scan(valuePtrs...); err != nil {
				rows.Close()
				return fmt.Errorf("scan row: %w", err)
			}

			rowMap := make(map[string]any, len(allCols))
			for i, col := range allCols {
				rowMap[col] = values[i]
			}

			updates, err := m.computeUpdates(rowMap, t)
			if err != nil {
				rows.Close()
				return fmt.Errorf("compute updates for row %v: %w", rowMap[t.idCol], err)
			}

			if len(updates) == 0 {
				batchCount++
				continue
			}

			if m.dryRun {
				m.logger.Info("DRY RUN: would update", "table", t.name, "id", rowMap[t.idCol], "columns", len(updates))
				batchCount++
				continue
			}

			if err := m.applyUpdates(ctx, t.name, t.idCol, rowMap[t.idCol], updates); err != nil {
				rows.Close()
				return fmt.Errorf("apply updates for row %v: %w", rowMap[t.idCol], err)
			}
			batchCount++
		}
		rows.Close()

		totalProcessed += batchCount
		m.logger.Info("Batch complete", "table", t.name, "offset", offset, "batch", batchCount, "total", totalProcessed)

		if batchCount < m.batchSize {
			break
		}
		offset += m.batchSize
	}

	m.logger.Info("Table complete", "table", t.name, "total", totalProcessed)
	return nil
}

func (m *migrator) computeUpdates(row map[string]any, t tableSpec) (map[string]any, error) {
	updates := make(map[string]any)

	transform := func(val any) (any, string, error) {
		var strVal string
		switch v := val.(type) {
		case string:
			strVal = v
		case nil:
			return nil, "", nil
		default:
			strVal = fmt.Sprintf("%v", v)
		}

		if m.decrypt {
			if !appcrypto.IsEncrypted(strVal) {
				return nil, strVal, nil // already plaintext, skip
			}
			decrypted, err := m.enc.DecryptString(strVal)
			if err != nil {
				return nil, "", err
			}
			return decrypted, decrypted, nil
		}

		// Encrypt
		if appcrypto.IsEncrypted(strVal) {
			return nil, strVal, nil // already encrypted, skip
		}
		encrypted, err := m.enc.EncryptString(strVal)
		if err != nil {
			return nil, "", err
		}
		return encrypted, strVal, nil
	}

	// Process plain string columns
	for _, col := range t.stringCols {
		newVal, rawVal, err := transform(row[col])
		if err != nil {
			return nil, fmt.Errorf("col %s: %w", col, err)
		}
		if newVal != nil && fmt.Sprintf("%v", newVal) != fmt.Sprintf("%v", row[col]) {
			updates[col] = newVal
			_ = rawVal
		}
	}

	// Process nullable string columns
	for _, col := range t.nullableCols {
		if row[col] == nil {
			continue
		}
		newVal, rawVal, err := transform(row[col])
		if err != nil {
			return nil, fmt.Errorf("col %s: %w", col, err)
		}
		if newVal != nil && fmt.Sprintf("%v", newVal) != fmt.Sprintf("%v", row[col]) {
			updates[col] = newVal
			_ = rawVal
		}
	}

	// Process JSONB blob columns (same transform, values are JSON strings)
	for _, col := range t.jsonbCols {
		if row[col] == nil {
			continue
		}
		newVal, rawVal, err := transform(row[col])
		if err != nil {
			return nil, fmt.Errorf("col %s: %w", col, err)
		}
		if newVal != nil && fmt.Sprintf("%v", newVal) != fmt.Sprintf("%v", row[col]) {
			updates[col] = newVal
			_ = rawVal
		}
	}

	// Process blind index columns
	for _, b := range t.blindCols {
		if b.blindIdx == "" {
			// No blind index column for this col (e.g., accounts.mask)
			// Still encrypt the column itself
			newVal, _, err := transform(row[b.col])
			if err != nil {
				return nil, fmt.Errorf("col %s: %w", b.col, err)
			}
			if newVal != nil && fmt.Sprintf("%v", newVal) != fmt.Sprintf("%v", row[b.col]) {
				updates[b.col] = newVal
			}
			continue
		}

		if row[b.col] == nil {
			// NULL value — blind index should also be NULL
			continue
		}

		strVal, ok := row[b.col].(string)
		if !ok {
			strVal = fmt.Sprintf("%v", row[b.col])
		}

		if m.decrypt {
			// Decrypt the col value; set blind index to NULL (will be re-populated on re-encrypt)
			if !appcrypto.IsEncrypted(strVal) {
				continue
			}
			decrypted, err := m.enc.DecryptString(strVal)
			if err != nil {
				return nil, fmt.Errorf("col %s decrypt: %w", b.col, err)
			}
			updates[b.col] = decrypted
			updates[b.blindIdx] = nil
			continue
		}

		// Already encrypted — check if blind index is populated
		if appcrypto.IsEncrypted(strVal) {
			// Check if blind index needs populating
			if row[b.blindIdx] == nil {
				// Extract plaintext via decrypt to compute blind index
				plaintext, err := m.enc.DecryptString(strVal)
				if err != nil {
					return nil, fmt.Errorf("col %s: re-index decrypt: %w", b.col, err)
				}
				if b.lower {
					plaintext = strings.ToLower(strings.TrimSpace(plaintext))
				}
				updates[b.blindIdx] = m.enc.BlindIndex(plaintext)
			}
			continue
		}

		// Encrypt plaintext value + compute blind index
		plaintext := strVal
		if b.lower {
			plaintext = strings.ToLower(strings.TrimSpace(plaintext))
		}
		encrypted, err := m.enc.EncryptString(strVal)
		if err != nil {
			return nil, fmt.Errorf("col %s encrypt: %w", b.col, err)
		}
		updates[b.col] = encrypted
		updates[b.blindIdx] = m.enc.BlindIndex(plaintext)
	}

	return updates, nil
}

func (m *migrator) applyUpdates(ctx context.Context, table, idCol string, id any, updates map[string]any) error {
	setClauses := make([]string, 0, len(updates))
	args := make([]any, 0, len(updates)+1)
	i := 1
	for col, val := range updates {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	args = append(args, id)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d",
		table, strings.Join(setClauses, ", "), idCol, i)

	_, err := m.db.Exec(ctx, query, args...)
	return err
}
