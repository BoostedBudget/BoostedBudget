package repository

import (
	"context"
)

// MerchantRepository handles data access for merchants.
type MerchantRepository struct {
	db DB
}

// NewMerchantRepository creates a new MerchantRepository.
func NewMerchantRepository(db DB) *MerchantRepository {
	return &MerchantRepository{db: db}
}

// GetOrCreateMerchants bulk-upserts merchants by name and returns a map of name→id.
// Uses a single DB round-trip via UNNEST.
// ON CONFLICT DO UPDATE is required (not DO NOTHING) to always RETURNING the id.
func (r *MerchantRepository) GetOrCreateMerchants(ctx context.Context, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	const q = `
        INSERT INTO merchants (id, name, created_at)
        SELECT gen_random_uuid(), u.name, NOW()
        FROM   unnest($1::text[]) AS u(name)
        ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
        RETURNING name, id`
	rows, err := r.db.Query(ctx, q, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string, len(names))
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		result[name] = id
	}
	return result, rows.Err()
}
