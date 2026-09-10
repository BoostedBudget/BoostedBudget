/**
 * formatConditionValue tests — Issue #1626 (Task K).
 *
 * The RED these tests encode: the Auto-Categorization Rules page rendered an `account_id`
 * condition as the raw account UUID ("Account is 32a0e8da-…"). `formatConditionValue` resolved
 * only the two PFC condition types and returned every other string value verbatim.
 *
 * The account-name resolution lives HERE, in the single shared renderer, because the util's two
 * callers render the same condition from two different sources — the live rule and the frozen
 * `definition_snapshot` — and must read identically.
 */

import { describe, it, expect } from 'bun:test'
import type { Account } from '@boostedbudget/api-client/models'
import {
  formatConditionValue,
  buildAccountLabelIndex,
} from './formatConditionValue'
import { UNKNOWN_ACCOUNT_LABEL } from '../constants'

/**
 * Deliberately NOT a UUID-shaped nickname and NOT equal to the fallback label: if either the
 * lookup or the fallback silently won, the assertion would still be able to tell.
 */
const CHECKING_ID = '32a0e8da-8aa6-44ac-9be7-3a321e1402c7'
const CHECKING_USER_ACCOUNT_ID = 'c1d2e3f4-0000-4000-8000-000000000001'
const SAVINGS_ID = '9f9f9f9f-1111-4111-8111-111111111111'

function account(overrides: Partial<Account>): Account {
  return {
    id: CHECKING_ID,
    name: 'Plaid Checking',
    type: 'depository',
    ...overrides,
  } as Account
}

describe('buildAccountLabelIndex', () => {
  it('indexes an account under BOTH its user_account_id and its raw id', () => {
    const index = buildAccountLabelIndex([
      account({ id: CHECKING_ID, user_account_id: CHECKING_USER_ACCOUNT_ID, name: 'Everyday Spending' }),
    ])

    // Rules written by the editor store user_account_id; legacy rules store accounts.id.
    expect(index[CHECKING_USER_ACCOUNT_ID]).toBe('Everyday Spending')
    expect(index[CHECKING_ID]).toBe('Everyday Spending')
  })

  it('prefers the user-chosen nickname over the provider name', () => {
    const index = buildAccountLabelIndex([
      account({ id: SAVINGS_ID, name: 'Plaid Saving', nickname: 'Vacation Fund' }),
    ])

    expect(index[SAVINGS_ID]).toBe('Vacation Fund')
  })

  it('falls back to the provider name when the nickname is blank', () => {
    const index = buildAccountLabelIndex([
      account({ id: SAVINGS_ID, name: 'Plaid Saving', nickname: '   ' }),
    ])

    expect(index[SAVINGS_ID]).toBe('Plaid Saving')
  })

  it('returns an empty index for an empty account list', () => {
    expect(buildAccountLabelIndex([])).toEqual({})
  })
})

describe('formatConditionValue — account_id', () => {
  const labels = buildAccountLabelIndex([
    account({ id: CHECKING_ID, user_account_id: CHECKING_USER_ACCOUNT_ID, name: 'Everyday Spending' }),
  ])

  it('renders the account name for a rule that stores the user_account_id', () => {
    expect(
      formatConditionValue({ type: 'account_id', value: CHECKING_USER_ACCOUNT_ID }, { accountLabels: labels })
    ).toBe('Everyday Spending')
  })

  it('renders the account name for a legacy rule that stores the raw accounts.id', () => {
    expect(
      formatConditionValue({ type: 'account_id', value: CHECKING_ID }, { accountLabels: labels })
    ).toBe('Everyday Spending')
  })

  it('renders the neutral label — never the UUID — for an id absent from the index', () => {
    const rendered = formatConditionValue(
      { type: 'account_id', value: SAVINGS_ID },
      { accountLabels: labels }
    )

    expect(rendered).toBe(UNKNOWN_ACCOUNT_LABEL)
    expect(rendered).not.toContain(SAVINGS_ID)
  })

  it('renders the neutral label — never the UUID — when no index is supplied at all', () => {
    const rendered = formatConditionValue({ type: 'account_id', value: CHECKING_ID })

    expect(rendered).toBe(UNKNOWN_ACCOUNT_LABEL)
    expect(rendered).not.toContain(CHECKING_ID)
  })

  it('renders the neutral label for an empty account value', () => {
    expect(formatConditionValue({ type: 'account_id', value: '' }, { accountLabels: labels })).toBe(
      UNKNOWN_ACCOUNT_LABEL
    )
  })

  it('ignores a value_display the backend does not populate for account conditions', () => {
    // value_display is a PFC-only field today. An account condition must still resolve through
    // the account index rather than trusting a field nothing writes.
    expect(
      formatConditionValue(
        { type: 'account_id', value: CHECKING_USER_ACCOUNT_ID, value_display: CHECKING_ID },
        { accountLabels: labels }
      )
    ).toBe('Everyday Spending')
  })
})

describe('formatConditionValue — untouched condition shapes', () => {
  it('still returns a merchant substring verbatim', () => {
    expect(formatConditionValue({ type: 'merchant_contains', value: 'starbucks' })).toBe('starbucks')
  })

  it('still prefers the backend value_display for a PFC primary condition', () => {
    expect(
      formatConditionValue({
        type: 'existing_pfc_primary',
        value: 'FOOD_AND_DRINK',
        value_display: 'Food and Drink',
      })
    ).toBe('Food and Drink')
  })

  it('still formats a PFC primary enum when no value_display arrives', () => {
    // Title-cased by formatPFCPrimary — deliberately NOT the same string as the value_display
    // fixture above, so a mutation that stopped honouring value_display would be visible.
    expect(formatConditionValue({ type: 'existing_pfc_primary', value: 'FOOD_AND_DRINK' })).toBe(
      'Food And Drink'
    )
  })

  it('still joins an account_type array of Plaid type tokens', () => {
    expect(formatConditionValue({ type: 'account_type', value: ['depository', 'credit'] })).toBe(
      'depository, credit'
    )
  })

  it('still renders an amount range', () => {
    expect(formatConditionValue({ type: 'amount_range', value: { min: 10, max: 50 } })).toBe('10 - 50')
  })

  it('still renders a date range', () => {
    expect(
      formatConditionValue({ type: 'date_range', value: { start: '2026-01-01', end: '2026-02-01' } })
    ).toBe('from 2026-01-01 to 2026-02-01')
  })

  it('still stringifies a bare numeric amount', () => {
    expect(formatConditionValue({ type: 'amount_greater_than', value: 5 })).toBe('5')
  })
})
