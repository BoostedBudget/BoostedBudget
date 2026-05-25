package domain

import "time"

// Merchant represents a merchant entity with a stable UUID.
// Merchants are populated from the merchant_name field on transactions
// and linked via the merchant_id FK.
type Merchant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
