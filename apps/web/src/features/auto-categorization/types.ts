// Categorization rule types matching backend domain models and API schemas
// Issue #346: Custom Transaction Categorization Rules
// Issue #1073: PFC v2 canonical — discriminated target shape, removed legacy existing_category

/**
 * Condition types supported by the rule engine
 */
export type ConditionType =
  | 'merchant_contains'
  | 'merchant_equals'
  | 'merchant_starts_with'
  | 'merchant_ends_with'
  | 'amount_greater_than'
  | 'amount_less_than'
  | 'amount_equals'
  | 'amount_range'
  | 'account_id'
  | 'account_type'
  | 'payment_channel'
  | 'existing_pfc_primary'
  | 'existing_pfc_detailed'
  | 'date_range'
  // #1625: emitted by the server for a {field, operator} pair with no legacy fused ancestor
  // (name, merchant_id, signed_amount, direction, open-ended dates). RESPONSE-ONLY — never send it.
  | 'expression'

/**
 * Human-readable labels for condition types
 */
export const CONDITION_TYPE_LABELS: Record<ConditionType, string> = {
  merchant_contains: 'Merchant contains',
  merchant_equals: 'Merchant equals',
  merchant_starts_with: 'Merchant starts with',
  merchant_ends_with: 'Merchant ends with',
  amount_greater_than: 'Amount greater than',
  amount_less_than: 'Amount less than',
  amount_equals: 'Amount equals',
  amount_range: 'Amount between',
  account_id: 'Account is',
  account_type: 'Account type is',
  payment_channel: 'Payment channel is',
  existing_pfc_primary: 'Existing PFC Primary (as the bank originally reported it)',
  existing_pfc_detailed: 'Existing PFC Detailed (as the bank originally reported it)',
  date_range: 'Date between',
  expression: 'Condition',
}

/**
 * #1625: the {field, operator, value} vocabulary. Mirrors backend
 * `domain/condition_schema.go` — keep FIELD_OPERATORS in step with `FieldSpecs` there.
 *
 * The legacy fused `ConditionType` names above remain valid on the wire (the server still emits
 * one whenever a pair has a fused equivalent), which is why both vocabularies live here.
 */
export type ConditionField =
  | 'merchant'
  | 'effective_merchant'
  | 'name'
  | 'merchant_id'
  | 'amount'
  | 'signed_amount'
  | 'direction'
  | 'account_id'
  | 'account_type'
  | 'payment_channel'
  | 'date'
  | 'pfc_primary'
  | 'pfc_detailed'

export type ConditionOperator =
  | 'eq'
  | 'neq'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'between'
  | 'contains'
  | 'starts_with'
  | 'ends_with'
  | 'in'

export const CONDITION_FIELD_LABELS: Record<ConditionField, string> = {
  merchant: 'Merchant name (as the bank sends it)',
  effective_merchant: 'Merchant name (as you see it)',
  name: 'Description',
  merchant_id: 'Merchant ID',
  amount: 'Amount',
  signed_amount: 'Amount (signed)',
  direction: 'Money in / out',
  account_id: 'Account',
  account_type: 'Account type',
  payment_channel: 'Payment channel',
  date: 'Date',
  pfc_primary: 'PFC Primary (as the bank originally reported it)',
  pfc_detailed: 'PFC Detailed (as the bank originally reported it)',
}

export const CONDITION_OPERATOR_LABELS: Record<ConditionOperator, string> = {
  eq: 'is',
  neq: 'is not',
  gt: 'is greater than',
  gte: 'is at least',
  lt: 'is less than',
  lte: 'is at most',
  between: 'is between',
  contains: 'contains',
  starts_with: 'starts with',
  ends_with: 'ends with',
  in: 'is any of',
}

const TEXT_OPERATORS: ConditionOperator[] = ['eq', 'neq', 'in', 'contains', 'starts_with', 'ends_with']
const ID_OPERATORS: ConditionOperator[] = ['eq', 'neq', 'in']
const NUMBER_OPERATORS: ConditionOperator[] = ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between']

/** Which operators each field accepts. Mirrors `FieldSpecs` in condition_schema.go. */
export const FIELD_OPERATORS: Record<ConditionField, ConditionOperator[]> = {
  merchant: TEXT_OPERATORS,
  effective_merchant: TEXT_OPERATORS,
  name: TEXT_OPERATORS,
  account_type: TEXT_OPERATORS,
  payment_channel: TEXT_OPERATORS,
  merchant_id: ID_OPERATORS,
  account_id: ID_OPERATORS,
  pfc_primary: ID_OPERATORS,
  pfc_detailed: ID_OPERATORS,
  amount: NUMBER_OPERATORS,
  signed_amount: NUMBER_OPERATORS,
  direction: ['eq', 'neq', 'in'],
  date: ['eq', 'gte', 'lte', 'between'],
}

/** Legal values for the `direction` field. */
export const DIRECTION_VALUES = ['inflow', 'outflow'] as const

/**
 * Target dimension discriminator for a rule's category action.
 * - 'pfc': set transaction's PFC override to target_pfc_primary[/target_pfc_detailed]
 * - 'custom': set transaction's custom override to target_custom_category_id
 * - 'clear': clear any existing override on the transaction
 * - undefined/null: rule has no category action (tags-only)
 */
export type RuleTargetKind = 'pfc' | 'custom' | 'clear'

/**
 * A single condition within a categorization rule
 */
export interface RuleCondition {
  /**
   * Legacy fused discriminator. Still emitted by the server for every pair that has a fused
   * equivalent, and still accepted on input, so frozen `definition_snapshot` rows keep rendering.
   */
  type: ConditionType
  /** #1625: the transaction attribute being matched. Absent on pre-#1625 snapshots. */
  field?: ConditionField
  /**
   * #1625: the comparison. Before then this field was declared and serialized but never
   * validated or read by any evaluator — which is why callers guessed at it.
   */
  operator?: ConditionOperator
  value: string | number | string[] | AmountRangeValue | DateRangeValue
  value_display?: string // populated by backend for PFC-typed conditions
}

/**
 * Value type for amount_range condition
 * Both min and max are optional to support open-ended ranges (e.g., "above $100" or "below $50")
 */
export interface AmountRangeValue {
  min?: number
  max?: number
}

/**
 * Value type for date_range condition
 */
export interface DateRangeValue {
  start?: string // ISO date string
  end?: string // ISO date string
}

/**
 * Condition logic options
 */
export type ConditionLogic = 'AND' | 'OR'

/**
 * Tag behavior options
 */
export type TagBehavior = 'add' | 'replace'

/**
 * Rule source options
 */
export type RuleSource = 'manual' | 'ai' | 'template'

/**
 * User-defined categorization rule
 * Matches backend domain.CategorizationRule schema (PFC v2 discriminated target)
 */
export interface CategorizationRule {
  id: string
  user_id: string
  name: string
  description?: string | null
  enabled: boolean
  priority: number
  conditions: RuleCondition[]
  condition_logic: ConditionLogic
  // Discriminated target (PFC v2)
  target_kind?: RuleTargetKind | null
  target_pfc_primary?: string | null
  target_pfc_detailed?: string | null
  target_pfc_primary_display?: string | null
  target_pfc_detailed_display?: string | null
  target_custom_category_id?: string | null
  target_custom_category_display?: string | null
  target_tags?: string[] | null
  tag_behavior: TagBehavior
  source: RuleSource
  match_count: number
  override_count: number
  last_matched_at?: string | null // ISO 8601 date-time
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
  // #1625: response-only, recomputed by the server on every read. True when the rule cannot be
  // evaluated as written and is therefore matching nothing. `quarantine_reason` is a fixed
  // backend vocabulary, never user content -- see QUARANTINE_EXPLANATIONS in
  // AutoCategorizationRuleCard for the rendering.
  quarantined?: boolean
  quarantine_reason?: string
}

/**
 * Request to create a new categorization rule
 */
export interface CreateRuleRequest {
  name: string
  description?: string
  priority?: number
  conditions: RuleCondition[]
  condition_logic?: ConditionLogic
  target_kind?: RuleTargetKind
  target_pfc_primary?: string
  target_pfc_detailed?: string
  target_custom_category_id?: string
  target_tags?: string[]
  tag_behavior?: TagBehavior
}

/**
 * Request to update an existing categorization rule
 */
export interface UpdateRuleRequest {
  name?: string
  description?: string
  enabled?: boolean
  priority?: number
  conditions?: RuleCondition[]
  condition_logic?: ConditionLogic
  target_kind?: RuleTargetKind
  target_pfc_primary?: string
  target_pfc_detailed?: string
  target_custom_category_id?: string
  target_tags?: string[]
  tag_behavior?: TagBehavior
}

/**
 * Request to toggle rule enabled status
 */
export interface ToggleRuleRequest {
  enabled: boolean
}

/**
 * Hook result type for useAutoCategorizationRules
 */
export interface UseAutoCategorizationRulesResult {
  rules: CategorizationRule[]
  loading: boolean
  error: string | null
  createRule: (request: CreateRuleRequest) => Promise<CategorizationRule>
  updateRule: (ruleId: string, request: UpdateRuleRequest) => Promise<CategorizationRule>
  deleteRule: (ruleId: string) => Promise<void>
  toggleRule: (ruleId: string, enabled: boolean) => Promise<CategorizationRule>
  refresh: () => Promise<void>
}

/**
 * #1625: the flat list of choices the rule builder's condition dropdown offers.
 *
 * The pre-#1625 dropdown was keyed on `ConditionType` alone, which is why the new capabilities
 * (matching the raw description, a merchant id, or the DIRECTION of money) were unreachable from
 * the web UI even after the API gained them — there was no fused type name to select.
 *
 * A choice is EITHER a legacy fused type or a {field, operator} pair. `key` is what the <select>
 * carries; pair choices use "field:operator" so the two namespaces cannot collide.
 */
export interface ConditionChoice {
  key: string
  label: string
  type?: ConditionType
  field?: ConditionField
  operator?: ConditionOperator
}

/** The legacy fused choices, in their existing order. `expression` is response-only. */
const FUSED_CHOICES: ConditionChoice[] = (
  Object.keys(CONDITION_TYPE_LABELS) as ConditionType[]
)
  .filter((t) => t !== 'expression')
  .map((t) => ({ key: t, label: CONDITION_TYPE_LABELS[t], type: t }))

/**
 * #1625: (field, operator) pairs that already have a fused `ConditionType`. Mirrors backend
 * `fusedToTriple` in `condition_schema.go` exactly — keep the two in step, or a pair ends up
 * offered twice (once as its fused choice, once as a generated pair choice) or not at all.
 */
const FUSED_PAIR_KEYS = new Set<string>([
  'merchant:contains', 'merchant:eq', 'merchant:starts_with', 'merchant:ends_with',
  'amount:gt', 'amount:lt', 'amount:eq', 'amount:between',
  'account_id:eq', 'account_type:eq', 'payment_channel:eq',
  'date:between',
  'pfc_primary:eq', 'pfc_detailed:eq',
])

/** Deterministic iteration order — cosmetic only, keeps the generated list's ordering stable. */
const FIELD_ORDER: ConditionField[] = [
  'merchant', 'effective_merchant', 'name', 'merchant_id',
  'amount', 'signed_amount', 'direction',
  'account_id', 'account_type', 'payment_channel',
  'date', 'pfc_primary', 'pfc_detailed',
]
const OPERATOR_ORDER: ConditionOperator[] = [
  'eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between', 'contains', 'starts_with', 'ends_with', 'in',
]

/**
 * #1625 PRODUCT DECISION — how wide to go, recorded here so the next reader knows it was chosen
 * rather than overlooked.
 *
 * PAIR_CHOICES offers every legal (field, operator) pair EXCEPT:
 *   - pairs with a fused equivalent (FUSED_CHOICES already offers those, under their existing
 *     fused key — offering both would put two options in the dropdown for the same condition).
 *   - the `in` operator, on every field. `in` needs a multi-value (chip-list) input widget that
 *     does not exist anywhere in this editor today; every existing value widget (text, number,
 *     date, single-select) writes exactly one scalar. Silently letting a user pick "is any of"
 *     and then type into a single text/number box would produce a condition whose displayed
 *     intent ("any of") does not match what gets sent (one value) — the exact silent-corruption
 *     shape this ticket exists to close, not something to introduce. `in` becomes reachable once
 *     a multi-value widget exists (tracked as a follow-up); until then it is a deliberately
 *     curated omission, not an oversight — the reachability test below asserts fields, not
 *     operators, so this exclusion is intentionally invisible to it.
 *
 * Every included pair gets a real input widget: `getInputTypeForCondition` in
 * AutoCategorizationRuleEditor.tsx routes by (field, operator) for amount/signed_amount/date
 * (number, amount-range, or single-date) and by field alone for account_id/pfc_primary/
 * pfc_detailed/direction (their existing lookup selects) — nothing here falls back to a
 * mismatched widget.
 *
 * ⚠ #1625 final review B4 — "gets a real widget" is NOT the whole claim, and reading it as such is
 * how `pfc_detailed:neq` shipped offered-but-unusable. A widget that needs LAZY-LOADED options is
 * only usable if the editor's fetch gate also recognises the pair form: the gate keyed on the
 * legacy fused type (`c.type === 'existing_pfc_detailed'`) while a pair choice writes
 * `type: undefined`, so the correct widget rendered with zero options and disabled. When you add a
 * pair here whose widget loads its options, check `needsPlaidDetailed`-style gates too, not just
 * `getInputTypeForCondition`. Pinned by
 * detailed_endpoint_fetched_for_the_pair_form_too_not_only_the_fused_type. account_type/payment_channel's `eq` AND `neq` operators both get their
 * curated enum picker (picking ONE known value works the same way whether the condition matches
 * or excludes it); only their substring-style operators (`contains`/`starts_with`/`ends_with`)
 * fall back to the generic text box, since a closed-set picker cannot express "match a
 * substring" — that is a deliberate, narrower widget choice for those operators, not a gap.
 */
const PAIR_CHOICES: ConditionChoice[] = FIELD_ORDER.flatMap((field) =>
  OPERATOR_ORDER
    .filter((op) => FIELD_OPERATORS[field].includes(op))
    .filter((op) => op !== 'in')
    .filter((op) => !FUSED_PAIR_KEYS.has(`${field}:${op}`))
    .map((op) => ({
      key: `${field}:${op}`,
      label: `${CONDITION_FIELD_LABELS[field]} ${CONDITION_OPERATOR_LABELS[op]}`,
      field,
      operator: op,
    })),
)

export const CONDITION_CHOICES: ConditionChoice[] = [...FUSED_CHOICES, ...PAIR_CHOICES]

/**
 * #1625 Task 5: a condition whose key matches no entry in CONDITION_CHOICES (a field the
 * frontend does not recognize yet, or a corrupted/legacy combination) must never be silently
 * displayed as the FIRST option — see AutoCategorizationRuleEditor.tsx, which renders this as an
 * extra, disabled `<option>` instead of letting the `<select>` fall back on its own.
 */
export function unmatchedConditionLabel(condition: Pick<RuleCondition, 'type' | 'field' | 'operator'>): string {
  if (condition.field && condition.operator) {
    return `Unsupported condition (${condition.field}:${condition.operator})`
  }
  return `Unsupported condition (${condition.type ?? 'unknown'})`
}

/** The choice key for a condition, whichever shape it arrived in. */
export function conditionChoiceKey(condition: Pick<RuleCondition, 'type' | 'field' | 'operator'>): string {
  if (condition.field && condition.operator) {
    const pair = `${condition.field}:${condition.operator}`
    // A pair with a fused equivalent is shown as its fused choice, so a rule saved before the
    // rework and one saved after it look identical in the builder.
    if (CONDITION_CHOICES.some((c) => c.key === pair)) return pair
    if (condition.type && condition.type !== 'expression') return condition.type
    return pair
  }
  return condition.type ?? ''
}
