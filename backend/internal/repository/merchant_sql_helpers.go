package repository

// merchantSortJoins is the LEFT JOIN block added when sort_by=merchant OR search is active.
// It resolves the effective merchant via the per-user merge+override chain.
// $1 is always user_id (the first bound parameter in all transaction queries).
// NOTE: mno.user_id and mno_t.user_id are TEXT columns, while transactions.user_id is UUID.
// Explicit ::text cast makes $1 resolve consistently when paired with t.user_id = $1::uuid in WHERE.
const merchantSortJoins = `LEFT JOIN merchants m              ON m.id = t.merchant_id
LEFT JOIN merchant_name_overrides mno   ON mno.merchant_id = m.id  AND mno.user_id = $1::text
LEFT JOIN merchants m2             ON m2.id = COALESCE(mno.merged_into_merchant_id, m.id)
LEFT JOIN merchant_name_overrides mno_t ON mno_t.merchant_id = m2.id AND mno_t.user_id = $1::text`

// effectiveMerchantNameExpr resolves the display name for a transaction's merchant,
// following the per-user override and merge chain. Never NULL (t.name is the final fallback).
const effectiveMerchantNameExpr = `COALESCE(mno.display_name, mno_t.display_name, NULLIF(m2.normalized_name,''), m2.name, NULLIF(t.merchant_name,''), t.name)`

// effectiveMerchantIdExpr resolves the canonical merchant id for grouping/filtering.
const effectiveMerchantIdExpr = `COALESCE(mno.merged_into_merchant_id, t.merchant_id)`
