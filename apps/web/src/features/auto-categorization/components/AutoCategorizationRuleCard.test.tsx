import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react'
import { AutoCategorizationRuleCard } from './AutoCategorizationRuleCard'
import { QUARANTINE_ISSUES } from '../utils/quarantineSentence'
import { readFileSync } from 'node:fs'
import type { CategorizationRule } from '../types'

// Global mocks for assertions
const mockOnToggle = mock(() => Promise.resolve())
const mockOnEdit = mock(() => {})
const mockOnDelete = mock(() => Promise.resolve())

describe('AutoCategorizationRuleCard', () => {
  const mockRule: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [
      { type: 'merchant_contains', value: 'starbucks' },
      { type: 'amount_greater_than', value: 5 },
    ],
    condition_logic: 'AND',
    target_kind: 'pfc',
    target_pfc_primary: 'FOOD_AND_DRINK',
    target_pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    target_tags: ['morning', 'caffeine'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 15,
    override_count: 2,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  beforeEach(() => {
    mockOnToggle.mockClear()
    mockOnEdit.mockClear()
    mockOnDelete.mockClear()
  })

  const renderCard = (rule = mockRule, handlers = {}) =>
    render(
      <AutoCategorizationRuleCard
        rule={rule}
        onToggle={handlers.onToggle ?? mockOnToggle}
        onEdit={handlers.onEdit ?? mockOnEdit}
        onDelete={handlers.onDelete ?? mockOnDelete}
      />
    )

  describe('Rendering', () => {
    it('should render rule name and description', () => {
      renderCard()

      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Categorize coffee purchases')).toBeInTheDocument()
    })

    it('should show Active status for enabled rule', () => {
      renderCard()

      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('should show Disabled status for disabled rule', () => {
      renderCard({ ...mockRule, enabled: false })

      expect(screen.getByText('Disabled')).toBeInTheDocument()
    })

    it('should render conditions with labels', () => {
      renderCard()

      expect(screen.getByText('Merchant contains')).toBeInTheDocument()
      expect(screen.getByText('starbucks')).toBeInTheDocument()
      expect(screen.getByText('Amount greater than')).toBeInTheDocument()
      expect(screen.getByText('5')).toBeInTheDocument()
    })

    it('should show condition logic', () => {
      renderCard()

      expect(screen.getByText('Conditions (AND)')).toBeInTheDocument()
    })

    it('should render target category using display fields when present', () => {
      // When display fields are absent, formatPFCEnum fallback is used
      renderCard()

      expect(screen.getByText('Food And Drink › Coffee')).toBeInTheDocument()
    })

    it('should render target tags', () => {
      renderCard()

      expect(screen.getByText('morning')).toBeInTheDocument()
      expect(screen.getByText('caffeine')).toBeInTheDocument()
    })

    it('should show tag behavior label', () => {
      renderCard()

      expect(screen.getByText('Add tags:')).toBeInTheDocument()
    })

    it('should show Replace tags label for replace behavior', () => {
      renderCard({ ...mockRule, tag_behavior: 'replace' as const })

      expect(screen.getByText('Replace tags:')).toBeInTheDocument()
    })

    it('should render stats', () => {
      renderCard()

      expect(screen.getByText('Matched: 15')).toBeInTheDocument()
      expect(screen.getByText('Overridden: 2')).toBeInTheDocument()
    })

    it('should render priority', () => {
      renderCard()

      expect(screen.getByText('Priority: 10')).toBeInTheDocument()
    })

    it('should not render description if not provided', () => {
      renderCard({ ...mockRule, description: undefined })

      expect(screen.queryByText('Categorize coffee purchases')).not.toBeInTheDocument()
    })
  })

  describe('Toggle', () => {
    it('should call onToggle with false when disabling', async () => {
      mockOnToggle.mockResolvedValue(undefined)
      renderCard()

      const disableButton = screen.getByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      await waitFor(() => {
        expect(mockOnToggle).toHaveBeenCalledWith(false)
      })
    })

    it('should call onToggle with true when enabling', async () => {
      mockOnToggle.mockResolvedValue(undefined)
      renderCard({ ...mockRule, enabled: false })

      const enableButton = screen.getByRole('button', { name: 'Enable' })
      fireEvent.click(enableButton)

      await waitFor(() => {
        expect(mockOnToggle).toHaveBeenCalledWith(true)
      })
    })

    it('should show Saving... while toggling', async () => {
      let resolveToggle: (value?: unknown) => void
      mockOnToggle.mockImplementation(() => new Promise((resolve) => {
        resolveToggle = resolve
      }))

      renderCard()

      // Use findByRole to wait for button to appear in CI
      const disableButton = await screen.findByRole('button', { name: 'Disable' })
      fireEvent.click(disableButton)

      expect(screen.getByRole('button', { name: 'Saving...' })).toBeInTheDocument()

      // Wrap promise resolution in act() to ensure React processes the state update
      await act(async () => {
        resolveToggle!()
      })
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
      })
    })
  })

  describe('Edit', () => {
    it('should call onEdit when Edit button is clicked', () => {
      renderCard()

      const editButton = screen.getByRole('button', { name: 'Edit' })
      fireEvent.click(editButton)

      expect(mockOnEdit).toHaveBeenCalledTimes(1)
    })
  })

  describe('Delete', () => {
    it('should show confirmation dialog when Delete is clicked', () => {
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      expect(screen.getByText(/Are you sure you want to delete/)).toBeInTheDocument()
    })

    it('should hide confirmation dialog when Cancel is clicked', () => {
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const cancelButton = screen.getByRole('button', { name: 'Cancel' })
      fireEvent.click(cancelButton)

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    it('should call onDelete when confirmed', async () => {
      mockOnDelete.mockResolvedValue(undefined)
      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      const confirmButton = within(dialog).getByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButton)

      await waitFor(() => {
        expect(mockOnDelete).toHaveBeenCalledTimes(1)
      })
    })

    it('should disable confirm button while deleting', async () => {
      let resolveDelete: (value?: unknown) => void
      mockOnDelete.mockImplementation(() => new Promise((resolve) => {
        resolveDelete = resolve
      }))

      renderCard()

      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      const confirmButton = within(dialog).getByRole('button', { name: 'Delete' })
      fireEvent.click(confirmButton)

      // While deleting, the confirm button should be disabled
      expect(confirmButton).toBeDisabled()

      // Wrap promise resolution in act() to ensure React processes the state update
      await act(async () => {
        resolveDelete!()
      })
      await waitFor(() => {
        // Dialog closes after delete completes
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      })
    })
  })

  describe('Format helpers', () => {
    it('should format array values as comma-separated', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_type' as const, value: ['checking', 'savings'] }],
      })

      expect(screen.getByText('checking, savings')).toBeInTheDocument()
    })

    it('should format range objects as min - max', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { min: 10, max: 100 } }],
      })

      expect(screen.getByText('10 - 100')).toBeInTheDocument()
    })

    it('should format date range objects', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { start: '2025-01-01', end: '2025-12-31' } }],
      })

      expect(screen.getByText('from 2025-01-01 to 2025-12-31')).toBeInTheDocument()
    })
  })

  describe('All condition types display', () => {
    // Tests for all 13 condition types to ensure they display correctly

    it('should display merchant_equals condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_equals' as const, value: 'Amazon.com' }],
      })

      expect(screen.getByText('Merchant equals')).toBeInTheDocument()
      expect(screen.getByText('Amazon.com')).toBeInTheDocument()
    })

    it('should display merchant_starts_with condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_starts_with' as const, value: 'AMAZON' }],
      })

      expect(screen.getByText('Merchant starts with')).toBeInTheDocument()
      expect(screen.getByText('AMAZON')).toBeInTheDocument()
    })

    it('should display merchant_ends_with condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_ends_with' as const, value: 'CAFE' }],
      })

      expect(screen.getByText('Merchant ends with')).toBeInTheDocument()
      expect(screen.getByText('CAFE')).toBeInTheDocument()
    })

    it('should display amount_less_than condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_less_than' as const, value: 50 }],
      })

      expect(screen.getByText('Amount less than')).toBeInTheDocument()
      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should display amount_equals condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_equals' as const, value: 99.99 }],
      })

      expect(screen.getByText('Amount equals')).toBeInTheDocument()
      expect(screen.getByText('99.99')).toBeInTheDocument()
    })

    // #1626 (Task K): this assertion USED to expect `acc-12345` on screen — it pinned the very
    // defect the user reported (an opaque account id rendered where a name belongs). An
    // unresolvable id now renders the neutral label; the resolved-name case is covered in
    // "identifiers never reach the screen (#1626)" below.
    it('should display account_id condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_id' as const, value: 'acc-12345' }],
      })

      expect(screen.getByText('Account is')).toBeInTheDocument()
      expect(screen.getByText('Unknown account')).toBeInTheDocument()
      expect(screen.queryByText('acc-12345')).toBeNull()
    })

    it('should display payment_channel condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'payment_channel' as const, value: 'online' }],
      })

      expect(screen.getByText('Payment channel is')).toBeInTheDocument()
      expect(screen.getByText('online')).toBeInTheDocument()
    })

    it('should display existing_pfc_primary condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_primary' as const, value: 'FOOD_AND_DRINK' }],
      })

      expect(screen.getByText('PFC Primary')).toBeInTheDocument()
      // formatPFCPrimary converts enum → "Food And Drink" display string
      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('formatConditionValue_renders_pfc_primary_with_curated_display', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_primary' as const, value: 'FOOD_AND_DRINK' }],
      })

      expect(screen.getByText('Food And Drink')).toBeInTheDocument()
    })

    it('prefers server-supplied value_display over local formatter for pfc_primary condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_primary' as const, value: 'FOOD_AND_DRINK', value_display: 'Food & Drink (server)' }],
      })

      expect(screen.getByText('Food & Drink (server)')).toBeInTheDocument()
      expect(screen.queryByText('Food And Drink')).not.toBeInTheDocument()
    })

    it('prefers server-supplied value_display over local formatter for pfc_detailed condition', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_detailed' as const, value: 'FOOD_AND_DRINK_GROCERIES', value_display: 'Groceries (server)' }],
      })

      expect(screen.getByText('Groceries (server)')).toBeInTheDocument()
      expect(screen.queryByText('Food And Drink › Groceries')).not.toBeInTheDocument()
    })

    it('formatConditionValue_renders_pfc_detailed_with_breadcrumb', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'existing_pfc_detailed' as const, value: 'FOOD_AND_DRINK_GROCERIES' }],
      })

      expect(screen.getByText('Food And Drink › Groceries')).toBeInTheDocument()
    })

    it('should display date_range with only start date', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { start: '2025-01-01' } }],
      })

      expect(screen.getByText('Date between')).toBeInTheDocument()
      expect(screen.getByText('from 2025-01-01')).toBeInTheDocument()
    })

    it('should display date_range with only end date', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'date_range' as const, value: { end: '2025-12-31' } }],
      })

      expect(screen.getByText('Date between')).toBeInTheDocument()
      expect(screen.getByText('to 2025-12-31')).toBeInTheDocument()
    })
  })

  describe('Value formatting edge cases', () => {
    it('should handle empty string condition value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'merchant_contains' as const, value: '' }],
      })

      expect(screen.getByText('Merchant contains')).toBeInTheDocument()
      // Value container should exist but be empty or show placeholder
      const conditions = screen.getByText('Conditions (AND)').parentElement?.parentElement
      expect(conditions).toBeInTheDocument()
    })

    it('should handle empty array for account_type', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'account_type' as const, value: [] }],
      })

      expect(screen.getByText('Account type is')).toBeInTheDocument()
    })

    it('should handle amount_range with only min', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { min: 10 } }],
      })

      expect(screen.getByText('Amount between')).toBeInTheDocument()
    })

    it('should handle amount_range with only max', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_range' as const, value: { max: 100 } }],
      })

      expect(screen.getByText('Amount between')).toBeInTheDocument()
    })

    it('should handle zero amount value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_equals' as const, value: 0 }],
      })

      expect(screen.getByText('Amount equals')).toBeInTheDocument()
      expect(screen.getByText('0')).toBeInTheDocument()
    })

    it('should handle negative amount value', () => {
      renderCard({
        ...mockRule,
        conditions: [{ type: 'amount_greater_than' as const, value: -50 }],
      })

      expect(screen.getByText('Amount greater than')).toBeInTheDocument()
      expect(screen.getByText('-50')).toBeInTheDocument()
    })

    it('should handle multiple conditions with OR logic', () => {
      renderCard({
        ...mockRule,
        condition_logic: 'OR' as const,
        conditions: [
          { type: 'merchant_contains' as const, value: 'coffee' },
          { type: 'merchant_contains' as const, value: 'cafe' },
        ],
      })

      expect(screen.getByText('Conditions (OR)')).toBeInTheDocument()
    })
  })

  /**
   * Issue #1626 (Task K). The user reported this card rendering
   * `Account is / 32a0e8da-8aa6-44ac-9be7-3a321e1402c7` — an opaque id where a name belongs.
   */
  describe('identifiers never reach the screen (#1626)', () => {
    const ACCOUNT_UUID = '32a0e8da-8aa6-44ac-9be7-3a321e1402c7'
    const CUSTOM_CATEGORY_UUID = '5c5c5c5c-3333-4333-8333-333333333333'

    const renderWithLabels = (rule: CategorizationRule, accountLabels?: Record<string, string>) =>
      render(
        <AutoCategorizationRuleCard
          rule={rule}
          accountLabels={accountLabels}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

    const accountRule: CategorizationRule = {
      ...mockRule,
      conditions: [{ type: 'account_id' as const, value: ACCOUNT_UUID }],
    }

    it('renders the account name for an account_id condition', () => {
      renderWithLabels(accountRule, { [ACCOUNT_UUID]: 'Everyday Spending' })

      expect(screen.getByText('Account is')).toBeInTheDocument()
      expect(screen.getByText('Everyday Spending')).toBeInTheDocument()
      expect(screen.queryByText(ACCOUNT_UUID)).toBeNull()
    })

    it('renders a neutral label instead of the UUID when the account cannot be named', () => {
      renderWithLabels(accountRule)

      expect(screen.getByText('Unknown account')).toBeInTheDocument()
      expect(screen.queryByText(ACCOUNT_UUID)).toBeNull()
    })

    it('keeps the resolved account name inside data-ph-mask and out of every attribute', () => {
      const { container } = renderWithLabels(accountRule, { [ACCOUNT_UUID]: 'Everyday Spending' })

      const named = screen.getByText('Everyday Spending')
      expect(named.closest('[data-ph-mask]')).not.toBeNull()

      // Session replay records attribute values verbatim and data-ph-mask does not cover them.
      for (const el of container.querySelectorAll('*')) {
        for (const attr of el.attributes) {
          expect(attr.value).not.toContain('Everyday Spending')
        }
      }
    })

    it('renders the shared neutral label when a custom-category target has no display name', () => {
      renderWithLabels({
        ...mockRule,
        target_kind: 'custom' as const,
        target_pfc_primary: undefined,
        target_pfc_detailed: undefined,
        target_custom_category_id: CUSTOM_CATEGORY_UUID,
        target_custom_category_display: undefined,
      })

      expect(screen.getByText('Custom category')).toBeInTheDocument()
      expect(screen.queryByText(CUSTOM_CATEGORY_UUID)).toBeNull()
    })

    it('still prefers the enriched custom-category name when it arrives', () => {
      renderWithLabels({
        ...mockRule,
        target_kind: 'custom' as const,
        target_pfc_primary: undefined,
        target_pfc_detailed: undefined,
        target_custom_category_id: CUSTOM_CATEGORY_UUID,
        target_custom_category_display: 'Bike Maintenance',
      })

      expect(screen.getByText('Bike Maintenance')).toBeInTheDocument()
      expect(screen.queryByText('Custom category')).toBeNull()
    })
  })

  // #1625 Task 7: a rule that cannot be evaluated must SAY so.
  //
  // Before this, such a rule rendered as "Active" and simply matched nothing; the reason existed
  // only in a Prometheus label the user cannot see. Surfacing it is the precondition for Task 8
  // enforcing it -- enforcing invisibly is the failure mode this ticket exists to close.
  describe('quarantine status', () => {
    it('renders "Not running" and the reason instead of "Active"', () => {
      render(
        <AutoCategorizationRuleCard
          rule={{
            ...mockRule,
            quarantined: true,
            quarantine_reason: 'no_conditions_matches_everything',
          }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Not running')).toBeDefined()
      expect(screen.queryByText('Active')).toBeNull()
      expect(screen.getByText(/no conditions, so it matches every transaction/i)).toBeDefined()
    })

    // ⚠ The distinction Codex caught, with #1625 Task 2's membership. A rule with a reason but
    // quarantined=false is STILL MATCHING transactions, and the card must not say otherwise:
    // rendering "Not running" for it would tell the user their rule was off while it quietly
    // recategorised their ledger.
    //
    // ⚠ #1625 third fix wave F2: this said only ONE reason can be in that state — a wrong-typed
    // value the backend coerces at read time. That is the only reason in it while quarantine
    // enforcement is ON. With RULE_QUARANTINE_ENFORCE off, services.quarantineStopsMatching
    // returns false for EVERY enforcement-class reason, so the card must render this branch
    // correctly for any of the five. The fixture below stays on the coercible reason because it
    // is the one a default-configured server actually emits.
    it('says "Needs attention", NOT "Not running", for a rule that still runs', () => {
      render(
        <AutoCategorizationRuleCard
          rule={{
            ...mockRule,
            quarantined: false,
            quarantine_reason: 'invalid_condition_value_shape',
          }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Needs attention')).toBeDefined()
      expect(screen.queryByText('Not running')).toBeNull()
      // #1625 second fix wave H2/M3: "may still be matching", not a flat "is running". With
      // enforcement off an uncoercible value still makes the evaluator error, so this branch is
      // reachable for a rule that contributes nothing; a flat claim would be false there.
      expect(screen.getByText(/may still be matching transactions/i)).toBeDefined()
    })

    // The other half of Task 2: a value with no honest reading is not guessed at, so the rule is
    // refused and the card must say so rather than "Needs attention".
    it('renders "Not running" for a value the backend could not repair', () => {
      render(
        <AutoCategorizationRuleCard
          rule={{
            ...mockRule,
            quarantined: true,
            quarantine_reason: 'uncoercible_condition_value',
          }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Not running')).toBeDefined()
      expect(screen.queryByText('Needs attention')).toBeNull()
      expect(screen.getByText(/cannot be read as the kind of value/i)).toBeDefined()
    })

    // Copy drift guard. Every enforced reason renders with quarantined=true, and its sentence
    // must not claim the rule is still running — the sentence and the badge disagreeing is the
    // exact failure this feature exists to remove, and the two are written in different places.
    it.each([
      'no_conditions_matches_everything',
      'invalid_condition_logic_defaults_to_and',
      'unnormalizable_condition',
      'uncoercible_condition_value',
    ])('the copy for enforced reason %s never claims the rule still runs', (reason) => {
      render(
        <AutoCategorizationRuleCard
          rule={{ ...mockRule, quarantined: true, quarantine_reason: reason }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Not running')).toBeDefined()
      expect(screen.queryByText(/still running/i)).toBeNull()
      expect(screen.getByText(/not being run|not matching any transactions/i)).toBeDefined()
    })

    // ── #1625 second fix wave L7: the sync check the docstring above used to merely claim. ──
    //
    // The Go constants are the single source of the vocabulary
    // (observability.AllQuarantineReasons is derived from them). Parsing the file is crude but it
    // is the only thing available across the language boundary: quarantine_reason is a free-form
    // string on the wire, so Orval generates no enum for the frontend to import. A regex over
    // `Quarantine... QuarantineReason = "..."` is exact for the shape those declarations have
    // had since they were introduced, and the falsifiability assertion below fails loudly if the
    // parse ever silently returns nothing instead of drifting quietly.
    const BACKEND_QUARANTINE_REASONS = (() => {
      const go = readFileSync('../../backend/internal/observability/rule_quarantine_metrics.go', 'utf8')
      return [...go.matchAll(/Quarantine\w+\s+QuarantineReason\s*=\s*"([a-z_]+)"/g)].map((m) => m[1])
    })()

    it('stays in sync with the Go quarantine vocabulary', () => {
      // Falsifiability: a regex that matched nothing would make every assertion below vacuous.
      expect(BACKEND_QUARANTINE_REASONS.length).toBeGreaterThanOrEqual(5)

      const rendered = Object.keys(QUARANTINE_ISSUES).sort()
      expect(rendered).toEqual([...BACKEND_QUARANTINE_REASONS].sort())

      // And the disabled/enabled matrix below must drive every one of them.
      for (const reason of BACKEND_QUARANTINE_REASONS) {
        expect(ALL_QUARANTINE_REASONS as readonly (string | undefined)[]).toContain(reason)
      }
    })

    // ── #1625 second fix wave H2/M3: the SAME reason in the other kill-switch state. ──
    //
    // The semantics this branch settled on: `quarantined` means "the server is currently
    // preventing this rule from matching anything". That is switch-dependent by design. With
    // RULE_QUARANTINE_ENFORCE off, the backend reports quarantined=false for an ENFORCEMENT-CLASS
    // reason because the engine really is running the rule -- a zero-condition rule then matches
    // EVERY transaction (rules/engine.go's `len(rule.Conditions) == 0 -> true` branch).
    //
    // So the card must read its run status off `quarantined`, never off the reason. Keying the
    // sentence to the reason rendered "It is not being run" over a rule that was recategorising
    // the entire ledger -- the ticket's own defect, published to the user.
    it.each([
      'no_conditions_matches_everything',
      'invalid_condition_logic_defaults_to_and',
      'unnormalizable_condition',
      'uncoercible_condition_value',
    ])('never claims %s is stopped while the server reports quarantined=false', (reason) => {
      render(
        <AutoCategorizationRuleCard
          rule={{ ...mockRule, quarantined: false, quarantine_reason: reason }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Needs attention')).toBeDefined()
      expect(screen.queryByText('Not running')).toBeNull()
      expect(screen.queryByText(/not being run|is not running|not matching any transactions/i)).toBeNull()
    })

    it('falls back to a generic sentence for a reason it does not recognise', () => {
      render(
        <AutoCategorizationRuleCard
          rule={{ ...mockRule, quarantined: true, quarantine_reason: 'invented_later' }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      // Never render a raw enum at the user.
      expect(screen.queryByText(/invented_later/)).toBeNull()
      expect(screen.getByText(/a problem with its conditions/i)).toBeDefined()
    })

    // #1625 Task 10: RuleMatches returns false immediately for !rule.Enabled, so a disabled
    // rule is not running no matter what quarantine_reason or quarantined says. The badge must
    // read "Disabled" -- not "Not running" or "Needs attention" -- for a disabled rule, in
    // every quarantine combination.
    //
    // #1625 final review I5: this docstring said "every quarantine combination" while the test
    // below covered ONE (no_conditions x quarantined=true) and its sibling covered a second.
    // It is now the product it always claimed to be: the whole reason vocabulary (plus absent,
    // plus an unrecognised one) x both values of `quarantined`.
    //
    // ⚠ #1625 second fix wave L7: this list is HAND-MAINTAINED TypeScript. The previous version
    // of this comment said it was "enumerated from observability.AllQuarantineReasons()", which
    // it never was — nothing connected the two, so a reason added in Go would have been a silent
    // gap here, which is the failure the claim promised was impossible. The claim is now TRUE
    // because BACKEND_QUARANTINE_REASONS below parses the Go constants and
    // 'stays in sync with the Go vocabulary' asserts the two agree in both directions.
    const ALL_QUARANTINE_REASONS = [
      undefined,
      'no_conditions_matches_everything',
      'invalid_condition_logic_defaults_to_and',
      'unnormalizable_condition',
      'invalid_condition_value_shape',
      'uncoercible_condition_value',
      'invented_later',
    ] as const

    for (const reason of ALL_QUARANTINE_REASONS) {
      for (const quarantined of [true, false]) {
        it(`reads "Disabled" for reason=${reason ?? 'none'} quarantined=${quarantined}, and never says Not running/Needs attention/Active`, () => {
          render(
            <AutoCategorizationRuleCard
              rule={{
                ...mockRule,
                enabled: false,
                quarantined,
                quarantine_reason: reason,
              }}
              onToggle={mockOnToggle}
              onEdit={mockOnEdit}
              onDelete={mockOnDelete}
            />
          )

          expect(screen.getByText('Disabled')).toBeDefined()
          expect(screen.queryByText('Not running')).toBeNull()
          expect(screen.queryByText('Needs attention')).toBeNull()
          expect(screen.queryByText('Active')).toBeNull()
          // Never render a raw enum at the user, in any combination.
          if (reason) {
            expect(screen.queryByText(new RegExp(reason))).toBeNull()
          }
        })
      }
    }

    // A disabled rule with a repaired (still-would-run-if-enabled) reason must not claim it is
    // "STILL RUNNING" -- that claim is only true while the rule is enabled.
    it('does not claim a disabled+repaired rule is still running', () => {
      render(
        <AutoCategorizationRuleCard
          rule={{
            ...mockRule,
            enabled: false,
            quarantined: false,
            quarantine_reason: 'invalid_condition_value_shape',
          }}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Disabled')).toBeDefined()
      expect(screen.queryByText(/still running/i)).toBeNull()
      expect(screen.getByText(/is disabled, so it is not running/i)).toBeDefined()
    })

    it('leaves a healthy rule exactly as it was', () => {
      render(
        <AutoCategorizationRuleCard
          rule={mockRule}
          onToggle={mockOnToggle}
          onEdit={mockOnEdit}
          onDelete={mockOnDelete}
        />
      )

      expect(screen.getByText('Active')).toBeDefined()
      expect(screen.queryByText('Not running')).toBeNull()
    })
  })

})
