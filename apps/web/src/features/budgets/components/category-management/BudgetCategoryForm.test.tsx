// API hooks are mocked via bunfig.toml preloads (categories-api.ts, savings-goals-api.ts).
// Do NOT add mock.module() for those here — it leaks across test files in bun:test.
// Issue #1323: Use centralized mockUseGetUserCategories from categories-api preload.
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import type { BudgetCategory, CreateBudgetCategoryRequest, UpdateBudgetCategoryRequest } from '../../types'
import type { UserTag } from '@/features/tags/types'
import type { CustomCategory } from '@boostedbudget/api-client/models'
import { mockUseGetSavingsGoals } from '@/test/mocks/savings-goals-api'
import { mockUseGetUserCategories } from '@/test/mocks/categories-api'
import type { UsePreviewCategoriesResult } from '../../hooks/usePreviewCategories'

const mockUsePreviewCategories = mock((): UsePreviewCategoriesResult => ({
  preview: null,
  loading: false,
  error: null,
}))

// usePreviewCategories is a local hook not preloaded — inline mock is safe (not polluting)
mock.module('../../hooks/usePreviewCategories', () => ({
  usePreviewCategories: mockUsePreviewCategories,
}))

import { render, screen, waitFor, act, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BudgetCategoryForm } from './BudgetCategoryForm'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

// Minimal CustomCategory factory
const makeCustomCategory = (
  id: string,
  name: string,
  // #1609 narrowed this field to the 18 PFCv2 primaries: it now decides transfer classification
  // in analytics, so the API advertises the legal set as an enum rather than a free string.
  parent_pfc_primary?: CustomCategory['parent_pfc_primary']
): CustomCategory => ({
  id,
  user_id: 'user-1',
  name,
  parent_pfc_primary: parent_pfc_primary ?? undefined,
  color: undefined,
  icon: undefined,
  usage_count: 0,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
})

// Default mock user categories (used across most tests)
const defaultUserCategories: CustomCategory[] = [
  makeCustomCategory('cat-uuid-1', 'Restaurants', 'FOOD_AND_DRINK'),
  makeCustomCategory('cat-uuid-2', 'Groceries'),
  makeCustomCategory('cat-uuid-3', 'Gas', 'TRANSPORTATION'),
  makeCustomCategory('cat-uuid-4', 'Clothing'),
]

describe('BudgetCategoryForm', () => {
  const mockOnClose = mock(() => {})
  const mockOnSubmit = mock((_request: CreateBudgetCategoryRequest | UpdateBudgetCategoryRequest) => Promise.resolve())

  // PFC enum-style categories (used only for the PFC tree picker path)
  const mockAvailableCategories: string[][] = [
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANT'],
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_GROCERIES'],
    ['TRANSPORTATION', 'TRANSPORTATION_GAS'],
  ]

  // PFC enum-style categories for testing the PFC path
  const mockPFCAvailableCategories: string[][] = [
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANT'],
    ['FOOD_AND_DRINK', 'FOOD_AND_DRINK_GROCERIES'],
    ['TRANSPORTATION', 'TRANSPORTATION_GAS'],
  ]

  const mockAvailableTags: UserTag[] = [
    {
      id: '1',
      user_id: 'user-1',
      name: 'Essential',
      color: '#22c55e',
      usage_count: 5,
      created_at: '',
      updated_at: '',
    },
    {
      id: '2',
      user_id: 'user-1',
      name: 'Discretionary',
      color: '#f59e0b',
      usage_count: 3,
      created_at: '',
      updated_at: '',
    },
  ]

  const createMockCategory = (overrides?: Partial<BudgetCategory>): BudgetCategory => ({
    id: 'cat-1',
    budget_id: 'budget-1',
    category_kind: 'pfc',
    pfc_primary: 'Food and Drink',
    pfc_detailed: 'Restaurants',
    allocated_amount: 500,
    spent_amount: 250,
    tags: ['Essential'],
    match_logic: 'category_only',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  })

  const defaultSavingsGoals = [
    { id: 'goal-1', name: 'House Down Payment', target_amount: 50000, current_amount: 12000 },
    { id: 'goal-2', name: 'Emergency Fund', target_amount: 10000, current_amount: 5000 },
  ]

  beforeEach(() => {
    mockOnClose.mockClear()
    mockOnSubmit.mockClear()
    mockOnSubmit.mockResolvedValue(undefined)
    mockUsePreviewCategories.mockClear()
    mockUsePreviewCategories.mockReturnValue({
      preview: null,
      loading: false,
      error: null,
    })
    // Issue #1323: mockUseGetUserCategories is the centralized mock from categories-api.ts preload.
    // Set a per-test default so tests see a non-loading list of categories.
    mockUseGetUserCategories.mockReturnValue({
      data: defaultUserCategories,
      isLoading: false,
      error: null,
    })
    mockUseGetSavingsGoals.mockReturnValue({
      data: defaultSavingsGoals,
      isLoading: false,
    })
  })

  /** Helper: switch the kind toggle to 'custom' */
  async function switchToCustom() {
    const customBtn = screen.getByRole('button', { name: /custom categories/i })
    fireEvent.click(customBtn)
  }

  describe('Create Mode', () => {
    it('shows preview amounts in their verified currency', () => {
      mockUsePreviewCategories.mockReturnValue({ preview: { transaction_count: 1, total_amount: 120, currency_code: 'CAD' }, loading: false, error: null })
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="edit" availableTags={[]} initialData={createMockCategory()} budgetStartDate="2026-09-01" budgetEndDate="2026-09-12" />)
      expect(screen.getByText('CA$120.00')).toBeInTheDocument()
    })
    it('previews edited groups against saved siblings for the actual current period', () => {
      const members = [{ category_kind: 'uncategorized' as const }]
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="edit" availableTags={[]} budgetId="budget-1" budgetStartDate="2026-09-01" budgetEndDate="2026-09-12" initialData={createMockCategory({ category_kind: 'group', group_name: 'Daily life', members, tags: [], pfc_primary: undefined, pfc_detailed: undefined })} />)
      expect(mockUsePreviewCategories).toHaveBeenLastCalledWith('group', undefined, undefined, undefined, [], 'category_only', '2026-09-01', '2026-09-12', true, expect.objectContaining({ budget_id: 'budget-1', replace_category_id: 'cat-1', group_name: 'Daily life', members }))
    })

    it('allows amount edits on a retained group emptied by category deletion', async () => {
      const user = userEvent.setup({ delay: null })
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="edit" availableTags={[]} initialData={createMockCategory({ category_kind: 'group', group_name: 'Retained group', members: [], pfc_primary: undefined, pfc_detailed: undefined, tags: [] })} />)
      expect(screen.getByText('This group has no categories and currently matches no spending.')).toBeInTheDocument()
      await user.clear(screen.getByRole('spinbutton', { name: /amount/i }))
      await user.type(screen.getByRole('spinbutton', { name: /amount/i }), '400')
      fireEvent.submit(screen.getByRole('button', { name: 'Save' }).closest('form')!)
      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ group_name: 'Retained group', allocated_amount: 400 })))
      expect(mockOnSubmit.mock.calls[0]?.[0]).not.toHaveProperty('members')
    })

    it('preserves mixed group members when editing its shared limit', async () => {
      const user = userEvent.setup({ delay: null })
      const members = [{ category_kind: 'pfc' as const, pfc_primary: 'FOOD_AND_DRINK' }, { category_kind: 'custom' as const, custom_category_id: 'cat-uuid-3' }]
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="edit" availableTags={[]} initialData={createMockCategory({ category_kind: 'group', group_name: 'Daily life', members, pfc_primary: undefined, pfc_detailed: undefined, tags: [] })} />)
      expect(screen.getByRole('button', { name: 'Shared group' })).toBeDisabled()
      await user.clear(screen.getByRole('spinbutton', { name: /amount/i }))
      await user.type(screen.getByRole('spinbutton', { name: /amount/i }), '400')
      fireEvent.submit(screen.getByRole('button', { name: 'Save' }).closest('form')!)
      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ group_name: 'Daily life', members, allocated_amount: 400 })))
    })

    it('creates a named shared limit with an Uncategorized member and no tags', async () => {
      const user = userEvent.setup({ delay: null })
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="create" availableTags={mockAvailableTags} />)
      fireEvent.click(screen.getByRole('button', { name: 'Shared group' }))
      await user.type(screen.getByLabelText('Group name'), 'Daily life')
      await user.click(screen.getByRole('checkbox', { name: 'Include Uncategorized' }))
      await user.type(screen.getByRole('spinbutton', { name: /amount/i }), '300')
      fireEvent.submit(screen.getByRole('button', { name: /^add$/i }).closest('form')!)
      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({
        category_kind: 'group', group_name: 'Daily life', members: [{ category_kind: 'uncategorized' }], allocated_amount: 300, match_logic: 'category_only',
      })))
    })

    it('requires a name and members for shared groups', async () => {
      const user = userEvent.setup({ delay: null })
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="create" availableTags={[]} />)
      fireEvent.click(screen.getByRole('button', { name: 'Shared group' }))
      await user.type(screen.getByRole('spinbutton', { name: /amount/i }), '300')
      fireEvent.submit(screen.getByRole('button', { name: /^add$/i }).closest('form')!)
      expect(screen.getByText('Enter a group name and select at least one category.')).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('creates automatic Other without a category or tag selector', async () => {
      const user = userEvent.setup({ delay: null })
      render(<BudgetCategoryForm isOpen onClose={mockOnClose} onSubmit={mockOnSubmit} mode="create" availableTags={mockAvailableTags} />)
      fireEvent.click(screen.getByRole('button', { name: 'Other' }))
      expect(screen.getByText(/not matched by individual lines or shared groups/i)).toBeInTheDocument()
      await user.type(screen.getByRole('spinbutton', { name: /amount/i }), '100')
      fireEvent.submit(screen.getByRole('button', { name: /^add$/i }).closest('form')!)
      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalledWith(expect.objectContaining({ category_kind: 'remainder', allocated_amount: 100, match_logic: 'category_only' })))
    })

    it('should render with empty fields in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      expect(amountInput).toHaveValue(null)
      expect(screen.getByText(/add.*category/i)).toBeInTheDocument()
    })

    it('should show category kind toggle buttons in create mode', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.getByRole('button', { name: /plaid categories/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /custom categories/i })).toBeInTheDocument()
    })

    it('should show HierarchicalCategoryTreeSelect when kind is pfc', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // The tree select trigger button is rendered (aria-label from the component)
      expect(screen.getByRole('button', { name: /select pfc category/i })).toBeInTheDocument()
      // The flat select should NOT be visible
      expect(screen.queryByRole('combobox', { name: /^category$/i })).not.toBeInTheDocument()
    })

    it('should show flat select when kind is custom', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect(categorySelect).not.toBeDisabled()
    })

    // Issue #1514 (fix round 4): the OLD version of this test asserted the child option
    // rendered under an <optgroup label="Food"> — i.e. it asserted the leak. <optgroup
    // label> is an ATTRIBUTE and <optgroup> has no child element, so unlike <option> text
    // there is no way to mask a parent-category-named group label; the only fix is to stop
    // putting the parent's name in an attribute at all. The child now renders flat,
    // immediately after its parent option (same relative order, no group header), and the
    // parent/child <option> TEXT is masked instead. This is a visible UX change — the bold
    // "Food" group-header row is gone — called out in the #1514 fix-round-4 report.
    it('renders a custom sub-category flat, immediately after its parent, with no optgroup carrying the parent name', async () => {
      const parent = makeCustomCategory('cat-parent', 'Food')
      const child: CustomCategory = {
        ...makeCustomCategory('cat-child', 'Fast Food'),
        parent_id: 'cat-parent',
      }
      mockUseGetUserCategories.mockReturnValue({
        data: [parent, child],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const parentOption = screen.getByRole('option', { name: /^Food$/i }) as HTMLOptionElement
      const childOption = screen.getByRole('option', { name: /Fast Food/i }) as HTMLOptionElement

      // No <optgroup> anywhere in this select may carry the parent's name as its label.
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      for (const grp of categorySelect.querySelectorAll('optgroup')) {
        expect(grp.getAttribute('label')).not.toBe('Food')
      }

      // The parent→child relationship is still communicated structurally: the child
      // option immediately follows its parent option in document order.
      const allOptions = Array.from(categorySelect.querySelectorAll('option'))
      const parentIndex = allOptions.indexOf(parentOption)
      expect(allOptions[parentIndex + 1]).toBe(childOption)
    })

    // Issue #1514 (scanner-completeness / optgroup-coverage review): the test above only
    // ever exercised ONE parent with ONE child. These three cases cover the shapes an
    // independent review flagged as unproven: multiple present parents (do their
    // flattened option runs stay correctly grouped by proximity, not interleaved?), a
    // childless parent mixed in with one that has children (does flattening only apply to
    // groups that actually need it?), and an orphaned child.
    it('flattens multiple parents-with-children without interleaving their child options', async () => {
      const foodParent = makeCustomCategory('cat-food', 'Food')
      const foodChild: CustomCategory = { ...makeCustomCategory('cat-fastfood', 'Fast Food'), parent_id: 'cat-food' }
      const travelParent = makeCustomCategory('cat-travel', 'Travel')
      const travelChild: CustomCategory = { ...makeCustomCategory('cat-flights', 'Flights'), parent_id: 'cat-travel' }
      mockUseGetUserCategories.mockReturnValue({
        data: [foodParent, foodChild, travelParent, travelChild],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )
      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      // No optgroup may carry either parent's name (both are PII-bearing groups).
      for (const grp of categorySelect.querySelectorAll('optgroup')) {
        expect(['Food', 'Travel']).not.toContain(grp.getAttribute('label'))
      }

      const allOptions = Array.from(categorySelect.querySelectorAll('option')).map((o) => o.textContent?.trim())
      const foodIdx = allOptions.indexOf('Food')
      const travelIdx = allOptions.indexOf('Travel')
      // Each parent's own child sits immediately after it — not after the OTHER parent.
      expect(allOptions[foodIdx + 1]).toBe('— Fast Food')
      expect(allOptions[travelIdx + 1]).toBe('— Flights')
    })

    it('renders a childless parent as a plain option alongside a parent that has children', async () => {
      const lonelyParent = makeCustomCategory('cat-lonely', 'Lonely')
      const foodParent = makeCustomCategory('cat-food', 'Food')
      const foodChild: CustomCategory = { ...makeCustomCategory('cat-fastfood', 'Fast Food'), parent_id: 'cat-food' }
      mockUseGetUserCategories.mockReturnValue({
        data: [lonelyParent, foodParent, foodChild],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )
      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      // The childless parent still renders as a selectable option...
      expect(screen.getByRole('option', { name: /^Lonely$/i })).toBeInTheDocument()
      // ...under the static, non-PII "Custom (no parent)" header (this group's label was
      // never PII-bearing — it's a childless-top-level bucket, not named after any one
      // category — so it keeps a real <optgroup>, unlike the Food/Fast Food group).
      const lonelyOption = screen.getByRole('option', { name: /^Lonely$/i })
      expect(lonelyOption.closest('optgroup')?.getAttribute('label')).toBe('Custom (no parent)')
      // The Food/Fast Food pairing is unaffected by Lonely's presence.
      const allOptions = Array.from(categorySelect.querySelectorAll('option')).map((o) => o.textContent?.trim())
      const foodIdx = allOptions.indexOf('Food')
      expect(allOptions[foodIdx + 1]).toBe('— Fast Food')
    })

    // KNOWN BUG (issue #1514 scanner-completeness review), NOT FIXED HERE: confirmed via
    // buildCategoryHierarchy.ts — `childrenByParent` collects every category with a
    // `parent_id`, including ones whose parent is absent from the `categories` array, but
    // `topLevel` (the only place children are read back out) is built by mapping over
    // `topLevelParents` alone. A child whose `parent_id` doesn't match any category in the
    // list is therefore silently dropped from the picker entirely — not shown flat, not
    // shown under any group, just gone. This is a pre-existing data-loss bug, orthogonal to
    // PII masking (the fix in this round only changes how a PRESENT parent's group is
    // labeled). Investigated and confirmed, but a real fix requires a product decision
    // (surface orphans in their own group? fall back to treating them as top-level? show a
    // warning?) that doesn't belong in a security-fix round — reported for a follow-up
    // ticket instead. This test documents the CURRENT (buggy) behavior so a silent
    // behavior change is caught, not to bless it as correct.
    it('documents a known bug: an orphaned child (parent not in the category list) disappears entirely', async () => {
      const orphan: CustomCategory = { ...makeCustomCategory('cat-orphan', 'Orphaned Child'), parent_id: 'cat-missing-parent' }
      mockUseGetUserCategories.mockReturnValue({
        data: [orphan],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )
      await switchToCustom()

      // BUG: the orphaned child should arguably still be selectable somehow, but today it
      // is not present anywhere in the category picker at all.
      expect(screen.queryByRole('option', { name: /Orphaned Child/i })).not.toBeInTheDocument()
    })

    // Issue #1514 (fix round 4): the custom-category <option> text (parent AND child
    // names) is user-authored and must be masked — assert with the widened scanner so it
    // covers text, not just attributes.
    it('masks custom category names in the category <option> text', async () => {
      const parent = makeCustomCategory('cat-parent', 'SENTINEL_PARENT_CAT')
      const child: CustomCategory = {
        ...makeCustomCategory('cat-child', 'SENTINEL_CHILD_CAT'),
        parent_id: 'cat-parent',
      }
      mockUseGetUserCategories.mockReturnValue({
        data: [parent, child],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const sentinels: PIISentinel[] = [
        { value: 'SENTINEL_PARENT_CAT', fieldDescription: 'custom_category.name in category <option>' },
        { value: 'SENTINEL_CHILD_CAT', fieldDescription: 'custom_category.name (child) in category <option>' },
      ]
      expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
    })

    it('should NOT show Custom Categories button when useGetUserCategories returns empty', () => {
      mockUseGetUserCategories.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
      })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockPFCAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.queryByRole('button', { name: /custom categories/i })).not.toBeInTheDocument()
    })

    it('should clear pfc fields when switching to custom', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // Switch to custom and select
      await switchToCustom()
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      // Switch back to pfc - custom_category_id should be cleared
      const pfcBtn = screen.getByRole('button', { name: /plaid categories/i })
      fireEvent.click(pfcBtn)

      // Tree select is shown again
      expect(screen.getByRole('button', { name: /select pfc category/i })).toBeInTheDocument()
      // Flat select is gone
      expect(screen.queryByRole('combobox', { name: /^category$/i })).not.toBeInTheDocument()
    })

    it('should require category selection before submit (pfc mode)', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/category.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should require category selection before submit (custom mode)', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/category.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should call onSubmit with CreateBudgetCategoryRequest (custom mode)', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '500' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: 'cat-uuid-1',
            allocated_amount: 500,
            match_logic: 'category_only',
          })
        )
      })
    })
  })

  describe('Edit Mode', () => {
    it('should pre-fill form with initialData in edit mode', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.getByText(/edit.*category/i)).toBeInTheDocument()

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      expect(amountInput).toHaveValue(500)
    })

    it('should disable category kind toggle buttons in edit mode', () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const pfcBtn = screen.getByRole('button', { name: /plaid categories/i })
      expect(pfcBtn).toBeDisabled()

      // Custom Categories button only shown when custom categories exist
      const customBtn = screen.queryByRole('button', { name: /custom categories/i })
      if (customBtn) {
        expect(customBtn).toBeDisabled()
      }
    })

    it('should show tree select disabled in edit mode when kind is pfc', () => {
      const category = createMockCategory({ category_kind: 'pfc' })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      expect(treeSelectBtn).toBeDisabled()
    })

    it('should show flat select disabled in edit mode when kind is custom', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: '',
        pfc_detailed: '',
        custom_category_id: 'cat-uuid-1',
      })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect(categorySelect).toBeDisabled()
    })

    it('should call onSubmit with UpdateBudgetCategoryRequest', async () => {
      const category = createMockCategory()
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '600' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            allocated_amount: 600,
          })
        )
      })
    })
  })

  describe('Validation', () => {
    it('should validate allocated_amount must be greater than 0', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      await user.clear(amountInput)
      await user.type(amountInput, '0')

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })
      await user.click(submitButton)

      await waitFor(() => {
        expect(screen.getByText(/amount.*greater.*0/i)).toBeInTheDocument()
      })

      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should show validation error for empty required fields', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })
      await user.click(submitButton)

      await waitFor(() => {
        expect(screen.getByText(/category.*required/i)).toBeInTheDocument()
      })

      expect(mockOnSubmit).not.toHaveBeenCalled()
    })

    it('should validate at least one tag when match_logic is tag_only', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })

      // Select "Match by tags only" radio option - the real CategoryMatchConfig uses radio buttons
      const tagOnlyRadio = screen.getByRole('radio', { name: /match by tags only/i })
      fireEvent.click(tagOnlyRadio)

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/at least one tag.*required/i)).toBeInTheDocument()
      expect(mockOnSubmit).not.toHaveBeenCalled()
    })
  })

  describe('Match Logic Integration', () => {
    it('should render CategoryMatchConfig and toggle tag picker based on logic', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // CategoryMatchConfig renders as a fieldset with radio buttons
      expect(screen.getByRole('group', { name: /match logic/i })).toBeInTheDocument()
      expect(screen.queryByText(/^tags$/i)).not.toBeInTheDocument()

      // Select "Match by tags only" radio option to show tags picker
      const tagOnlyRadio = screen.getByRole('radio', { name: /match by tags only/i })
      await user.click(tagOnlyRadio)

      await waitFor(() => {
        expect(screen.getByText(/^tags$/i)).toBeInTheDocument()
      })
    })
  })

  describe('Submit Behavior', () => {
    const fillFormBasic = async () => {
      await switchToCustom()
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '100' } })
    }

    it('should disable submit button while submitting', async () => {
      let resolveSubmit: () => void
      mockOnSubmit.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            resolveSubmit = resolve
          })
      )

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await fillFormBasic()

      const submitButton = screen.getByRole('button', { name: /save|add|create/i })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(submitButton).toBeDisabled()
      })

      await act(async () => {
        resolveSubmit!()
      })
    })

    it('should show error message on failed submit', async () => {
      mockOnSubmit.mockRejectedValue(new Error('Network error'))

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await fillFormBasic()

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      expect(await screen.findByText(/failed|error/i)).toBeInTheDocument()
    })
  })

  describe('Dialog Behavior', () => {
    it('should close dialog when cancel clicked', async () => {
      const user = userEvent.setup({ delay: null })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      await user.click(cancelButton)

      expect(mockOnClose).toHaveBeenCalledTimes(1)
    })

    it('should not render when isOpen is false', () => {
      render(
        <BudgetCategoryForm
          isOpen={false}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      expect(screen.queryByText(/add.*category/i)).not.toBeInTheDocument()
    })
  })

  describe('Soft Warning — allocation exceeds expected income', () => {
    it('uses the budget currency for the amount prefix and over-allocation warning', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          currencyCode="EUR"
          expectedIncome={1000}
          totalAllocatedExcludingCurrent={900}
        />
      )

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      expect(amountInput.parentElement).toHaveTextContent('€')
      expect(amountInput.parentElement).not.toHaveTextContent('$')

      fireEvent.change(amountInput, { target: { value: '200' } })

      const warning = await screen.findByRole('status')
      expect(warning).toHaveTextContent('€100.00')
      expect(screen.getByText('€100.00')).toHaveAttribute('data-ph-mask')
    })

    it('TestSoftWarning_ShowsWhenAllocationExceedsIncome — shows warning and keeps save enabled', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          expectedIncome={1000}
          totalAllocatedExcludingCurrent={900}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '200' } })

      // Warning should be visible (900 + 200 = 1100 > 1000)
      expect(await screen.findByText(/exceed/i)).toBeInTheDocument()

      // Save button must remain enabled
      const saveButton = screen.getByRole('button', { name: /save|add|create/i })
      expect(saveButton).not.toBeDisabled()
    })

    it('TestSoftWarning_HiddenWhenUnder — no warning when allocation is within income', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          expectedIncome={1000}
          totalAllocatedExcludingCurrent={900}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-1' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '50' } })

      // No warning (900 + 50 = 950 <= 1000)
      expect(screen.queryByText(/exceed/i)).not.toBeInTheDocument()
    })
  })

  describe('Custom Category UUID fix (Gap 1)', () => {
    it('submits real UUID as custom_category_id when a custom category is selected', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: 'cat-uuid-2' } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '300' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: 'cat-uuid-2',
          })
        )
        // Must NOT be a name string — verify it's a UUID-shaped value
        const call = mockOnSubmit.mock.calls[0][0] as { custom_category_id?: string }
        expect(call.custom_category_id).toBe('cat-uuid-2')
        expect(call.custom_category_id).not.toContain('|')
        expect(call.custom_category_id).not.toContain('Food')
      })
    })

    it('shows Custom Categories button when useGetUserCategories returns categories', () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={[]}
          availableTags={mockAvailableTags}
        />
      )

      // Button visible because mockUseGetUserCategories returns 4 categories by default
      expect(screen.getByRole('button', { name: /custom categories/i })).toBeInTheDocument()
    })

    it('groups custom categories with parent_pfc_primary into optgroups', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })

      // "Restaurants" (FOOD_AND_DRINK parent) and "Gas" (TRANSPORTATION parent)
      // should be rendered as options inside optgroups
      expect(categorySelect.querySelector('option[value="cat-uuid-1"]')).not.toBeNull()
      expect(categorySelect.querySelector('option[value="cat-uuid-3"]')).not.toBeNull()

      // Check optgroup elements exist (at least one group with parent label)
      const optgroups = categorySelect.querySelectorAll('optgroup')
      expect(optgroups.length).toBeGreaterThan(0)

      // "Groceries" and "Clothing" have no parent — should be in a "no parent" group
      expect(categorySelect.querySelector('option[value="cat-uuid-2"]')).not.toBeNull()
      expect(categorySelect.querySelector('option[value="cat-uuid-4"]')).not.toBeNull()
    })

    it('pre-fills select with existing UUID in edit mode', () => {
      const category = createMockCategory({
        category_kind: 'custom',
        pfc_primary: undefined,
        pfc_detailed: undefined,
        custom_category_id: 'cat-uuid-3',
      })
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="edit"
          initialData={category}
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      expect((categorySelect as HTMLSelectElement).value).toBe('cat-uuid-3')
    })
  })

  describe('Category form submission — PFC and Custom paths', () => {
    // Radix Popover portals render synchronously in jsdom — data-testid rows in PopoverContent are accessible
    it('submits pfc_primary with pfc_detailed undefined when only primary selected', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // Open the PFC tree picker and select only a primary category
      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      fireEvent.click(treeSelectBtn)

      // Click on the FOOD_AND_DRINK primary row (no detailed)
      const primaryRow = screen.getByTestId('primary-row-FOOD_AND_DRINK')
      fireEvent.click(primaryRow)

      // Fill in the amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '150' } })

      // Submit
      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'pfc',
            pfc_primary: 'FOOD_AND_DRINK',
            pfc_detailed: undefined,
            allocated_amount: 150,
          })
        )
      })
    })

    it('submits both pfc_primary and pfc_detailed when both are selected', async () => {
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      // Open the PFC tree picker
      const treeSelectBtn = screen.getByRole('button', { name: /select pfc category/i })
      fireEvent.click(treeSelectBtn)

      // Expand FOOD_AND_DRINK to see detailed options
      fireEvent.click(screen.getByTestId('expand-toggle-FOOD_AND_DRINK'))

      // Select a detailed sub-category
      fireEvent.click(screen.getByTestId('detailed-row-FOOD_AND_DRINK_RESTAURANT'))

      // Fill in the amount
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '200' } })

      // Submit
      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'pfc',
            pfc_primary: 'FOOD_AND_DRINK',
            pfc_detailed: 'FOOD_AND_DRINK_RESTAURANT',
            allocated_amount: 200,
          })
        )
      })
    })

    it('submits real UUID as custom_category_id when a custom category is selected', async () => {
      const realUUID = 'f47ac10b-58cc-4372-a567-0e02b2c3d479'
      mockUseGetUserCategories.mockReturnValue({
        data: [makeCustomCategory(realUUID, 'Restaurants', 'FOOD_AND_DRINK')],
        isLoading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      // Select a custom category by its real UUID (not its display name)
      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      fireEvent.change(categorySelect, { target: { value: realUUID } })

      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      fireEvent.change(amountInput, { target: { value: '75' } })

      const form = document.querySelector('form')
      fireEvent.submit(form!)

      await waitFor(() => {
        expect(mockOnSubmit).toHaveBeenCalledTimes(1)
        expect(mockOnSubmit).toHaveBeenCalledWith(
          expect.objectContaining({
            category_kind: 'custom',
            custom_category_id: realUUID,
            allocated_amount: 75,
          })
        )
        // Ensure it's the UUID, not the category name or any concatenated string
        const call = mockOnSubmit.mock.calls[0][0] as { custom_category_id?: string }
        expect(call.custom_category_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i)
        expect(call.custom_category_id).not.toContain('Restaurants')
        expect(call.custom_category_id).not.toContain('|')
      })
    })
  })

  describe('Category Preview', () => {
    it('should show preview when category selected and budget dates provided', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        expect(screen.getByTestId('category-preview')).toBeInTheDocument()
        expect(screen.getByText(/matches/i)).toBeInTheDocument()
        expect(screen.getByText('5')).toBeInTheDocument()
        expect(screen.getByText(/\$250\.50/)).toBeInTheDocument()
      })
    })

    it('should show loading state while fetching preview', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: null,
        loading: true,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        expect(screen.getByText(/calculating/i)).toBeInTheDocument()
      })
    })

    it('should show error message when preview fails', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: null,
        loading: false,
        error: new Error('Network error'),
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        expect(screen.getByText(/unable to load preview/i)).toBeInTheDocument()
      })
    })

    it('should not show preview when no category selected', () => {
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      expect(screen.queryByTestId('category-preview')).not.toBeInTheDocument()
    })

    it('should not show preview when budget dates not provided', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      expect(screen.queryByTestId('category-preview')).not.toBeInTheDocument()
    })

    it('should show singular transaction text when count is 1', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 1, total_amount: 50.00 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        const preview = screen.getByTestId('category-preview')
        expect(preview).toHaveTextContent(/transaction totaling/i)
        expect(preview).not.toHaveTextContent(/transactions totaling/i)
      })
    })

    // Issue #1630 Task 5: a pfc_primary rollup (Issue #1203) is a CORRECT total that looked
    // like a double-count bug because the preview never disclosed the nested child's share.
    it('should disclose the nested-child breakdown when a rollup occurred', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: {
          transaction_count: 2,
          total_amount: 615.28,
          direct_transaction_count: 1,
          direct_amount: 192.08,
          children: [
            { category_id: 'cc-hoa', category_name: 'HOA', transaction_count: 1, total_amount: 423.20 },
          ],
        },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        const breakdown = screen.getByTestId('category-preview-breakdown')
        expect(breakdown).toHaveTextContent(/\$192\.08/)
        expect(breakdown).toHaveTextContent(/direct/i)
        expect(breakdown).toHaveTextContent(/\$423\.20/)
        expect(breakdown).toHaveTextContent(/HOA/)
      })
    })

    it('should not show a breakdown when the preview has no nested children', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: { transaction_count: 5, total_amount: 250.50 },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        expect(screen.getByTestId('category-preview')).toBeInTheDocument()
      })
      expect(screen.queryAllByTestId('category-preview-breakdown')).toHaveLength(0)
    })

    // Issue #1630 Task B5: a real partition of total_amount includes a THIRD bucket — an
    // unrelated tag match pulled in by match_logic or/tag_only, which the pre-B5 breakdown
    // never disclosed at all.
    it('should disclose the tag-only-outside-category bucket', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: {
          transaction_count: 3,
          total_amount: 75.00,
          direct_transaction_count: 1,
          direct_amount: 40.00,
          tag_only_transaction_count: 1,
          tag_only_amount: 15.00,
          children: [
            { category_id: 'cc-coffee', category_name: 'Coffee Club', transaction_count: 1, total_amount: 20.00 },
          ],
        },
        loading: false,
        error: null,
        // The mock's inferred return type is fixed from its first `mock(() => ({preview:
        // null, ...}))` call (bun:test's `mock` has no separate type-param overload), so
        // every override with a non-null preview is a pre-existing TS2322 mismatch (see the
        // sibling test above and others in this file) — cast to sidestep it without widening
        // the shared mock's declared type.
      } as any) // eslint-disable-line @typescript-eslint/no-explicit-any

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        const breakdown = screen.getByTestId('category-preview-tag-only')
        expect(breakdown).toHaveTextContent(/\$15\.00/)
        expect(breakdown).toHaveTextContent(/tag/i)
      })
    })

    it('should mask money and the child category name in the breakdown for PII (data-ph-mask)', async () => {
      const user = userEvent.setup({ delay: null })
      mockUsePreviewCategories.mockReturnValue({
        preview: {
          transaction_count: 2,
          total_amount: 615.28,
          direct_transaction_count: 1,
          direct_amount: 192.08,
          children: [
            { category_id: 'cc-hoa', category_name: 'HOA', transaction_count: 1, total_amount: 423.20 },
          ],
        },
        loading: false,
        error: null,
      })

      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableCategories={mockAvailableCategories}
          availableTags={mockAvailableTags}
          budgetStartDate="2024-01-01"
          budgetEndDate="2024-01-31"
        />
      )

      await switchToCustom()

      const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
      await user.selectOptions(categorySelect, ['Restaurants'])

      await waitFor(() => {
        expect(screen.getByTestId('category-preview-breakdown')).toBeInTheDocument()
      })

      const breakdown = screen.getByTestId('category-preview-breakdown')
      const items = breakdown.querySelectorAll('li')
      expect(items.length).toBeGreaterThan(0)
      items.forEach((item) => {
        expect(item).toHaveAttribute('data-ph-mask')
      })
    })
  })

  describe('Savings Goal kind (#1300)', () => {
    const renderSavingsForm = () =>
      render(
        <BudgetCategoryForm
          isOpen={true}
          onClose={mockOnClose}
          onSubmit={mockOnSubmit}
          mode="create"
          availableTags={mockAvailableTags}
        />
      )

    it('renders a "Savings Goal" kind button', () => {
      renderSavingsForm()
      expect(screen.getByRole('button', { name: /savings goal/i })).toBeTruthy()
    })

    it('shows SavingsGoalSelect when Savings Goal kind is clicked', async () => {
      const user = userEvent.setup()
      renderSavingsForm()
      await user.click(screen.getByRole('button', { name: /savings goal/i }))
      // SavingsGoalSelect renders a combobox
      expect(screen.getByRole('combobox')).toBeTruthy()
      expect(screen.getByText('House Down Payment')).toBeTruthy()
    })

    it('hides match logic config for savings kind', async () => {
      const user = userEvent.setup()
      renderSavingsForm()
      await user.click(screen.getByRole('button', { name: /savings goal/i }))
      // CategoryMatchConfig label shouldn't be visible
      expect(screen.queryByText(/match logic/i)).toBeNull()
    })

    it('disables preview for savings without fabricating a preview category kind', async () => {
      const user = userEvent.setup()
      renderSavingsForm()
      await user.click(screen.getByRole('button', { name: /savings goal/i }))

      expect(mockUsePreviewCategories).toHaveBeenLastCalledWith(
        undefined,
        undefined,
        undefined,
        undefined,
        [],
        'category_only',
        '',
        '',
        false,
        expect.any(Object),
      )
    })

    it('submits savings_goal_id and category_kind=savings', async () => {
      const user = userEvent.setup({ delay: null })
      renderSavingsForm()
      await user.click(screen.getByRole('button', { name: /savings goal/i }))
      const goalSelect = screen.getByRole('combobox')
      await user.selectOptions(goalSelect, 'goal-1')
      const amountInput = screen.getByRole('spinbutton', { name: /amount/i })
      await user.clear(amountInput)
      await user.type(amountInput, '200')
      // Submit via form element to avoid button label ambiguity with "Savings Goal" button
      // Dialog renders in Radix portal — use document.querySelector
      const form = document.querySelector('form')
      fireEvent.submit(form!)
      await waitFor(() => expect(mockOnSubmit).toHaveBeenCalled())
      const submitted = mockOnSubmit.mock.calls[0][0]
      expect(submitted.category_kind).toBe('savings')
      expect(submitted.savings_goal_id).toBe('goal-1')
    })
  })
})
