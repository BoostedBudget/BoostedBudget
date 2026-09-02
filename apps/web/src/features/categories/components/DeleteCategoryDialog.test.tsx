/**
 * DeleteCategoryDialog tests (Issue #1203, rewritten for #1569 Task 12 gap 3).
 *
 * Task 9 changed what deleting a category DOES, and this dialog's copy was left describing the
 * old behaviour. It is the only place the user is told anything about the consequences, so a
 * stale sentence here is not cosmetic — it is the app stating something untrue about the user's
 * own data. Every claim below is checked against
 * customCategoryRepository.DeleteWithReassignment.
 */
import { describe, it, expect, beforeEach } from 'bun:test'
import { render } from '@testing-library/react'
import { mockUseGetCategoryUsage } from '@/test/mocks/categories-api'
import { DeleteCategoryDialog } from './DeleteCategoryDialog'
import type { CustomCategory } from '../types'

const category: CustomCategory = {
  id: 'cat-1',
  user_id: 'u1',
  name: 'Coffee',
  usage_count: 0,
  created_at: '',
  updated_at: '',
}

const renderDialog = () =>
  render(
    <DeleteCategoryDialog category={category} open onOpenChange={() => {}} onConfirm={() => {}} />
  )

/**
 * The description is deliberately several elements (a paragraph, a bulleted list, a closing
 * line), so getByText — which matches within a SINGLE element — cannot see a sentence that
 * spans them. Assert against the dialog's rendered text instead. Radix portals the dialog to
 * document.body, which is why this reads from there and not from `container`.
 */
const dialogText = () => document.body.textContent ?? ''

describe('DeleteCategoryDialog', () => {
  beforeEach(() => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: { transaction_count: 2, budget_count: 1, rule_count: 3, child_count: 1 },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)
  })

  it('renders the impact counts in the warning', () => {
    renderDialog()
    expect(dialogText()).toMatch(/2 transactions/i)
    expect(dialogText()).toMatch(/1 budget/i)
    expect(dialogText()).toMatch(/1 sub-category/i)
  })

  // ── #1569 T12 gap 3 ──────────────────────────────────────────────────────────────────────
  //
  // Step 2 of DeleteWithReassignment sets enabled = false on every rule that targeted the
  // category. It does not delete them and does not blank their target. The user is entitled to
  // know that some of their automation just stopped running.
  it('says the rules that used the category are turned off', () => {
    renderDialog()
    expect(dialogText()).toMatch(/3 rules/i)
    expect(dialogText()).toMatch(/turned off/i)
  })

  // Step 4 tombstones budget_categories rows. Budget lines DO go away.
  it('says the budget lines that used the category are removed', () => {
    renderDialog()
    expect(dialogText()).toMatch(/budget line/i)
    expect(dialogText()).toMatch(/removed from your budgets/i)
  })

  // DeleteWithReassignment touches `transactions` in no step at all. The old copy claimed the
  // opposite — "will revert to their bank category and be marked unreviewed" — which is the
  // single most alarming thing the dialog could say and it was false.
  it('does NOT claim transactions revert to their bank category', () => {
    renderDialog()
    expect(dialogText()).not.toMatch(/revert to their bank category/i)
    expect(dialogText()).not.toMatch(/unreviewed/i)
  })

  it('reassures the user that their history is left alone', () => {
    renderDialog()
    expect(dialogText()).toMatch(/keep this category/i)
  })

  it('still says sub-categories move to top-level', () => {
    renderDialog()
    expect(dialogText()).toMatch(/top level/i)
  })

  // The fallback path is reached when the usage counts fail to load, and it was the other place
  // the false transactions claim lived.
  it('falls back to a warning that is true even without the counts', () => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('boom'),
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)

    renderDialog()
    expect(dialogText()).not.toMatch(/revert to their bank category/i)
    expect(dialogText()).toMatch(/turned off/i)
    expect(dialogText()).toMatch(/keep this category/i)
  })

  // Counts of zero must not produce a claim about something that will not happen.
  it('omits the rule sentence entirely when no rule targets the category', () => {
    mockUseGetCategoryUsage.mockReturnValue({
      data: { transaction_count: 0, budget_count: 0, rule_count: 0, child_count: 0 },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseGetCategoryUsage>)

    renderDialog()
    expect(dialogText()).not.toMatch(/turned off/i)
    expect(dialogText()).not.toMatch(/budget line/i)
    expect(dialogText()).not.toMatch(/top level/i)
  })

  // The category name is user-entered and is PII by this project's policy. It is masked here
  // because the dialog is outside any DataTable column set — nothing masks it by default.
  it('masks the category name for session replay', () => {
    renderDialog()
    // Radix portals the dialog to document.body, so `container` is empty — scan the document.
    const masked = Array.from(document.body.querySelectorAll('[data-ph-mask]'))
    expect(masked.some((el) => el.textContent === 'Coffee')).toBe(true)
  })
  // Fix round 1 (Minor). Radix's DialogDescription renders a <p>, and the side-effect list was a
  // <ul> inside it — invalid HTML and a permanent validateDOMNesting console error on every open.
  it('nests no block-level list inside the dialog description paragraph', () => {
    renderDialog()
    const desc = document.body.querySelector('[data-slot="dialog-description"]')
    expect(desc).not.toBeNull()
    expect(desc?.tagName.toLowerCase()).toBe('p')
    expect(desc?.querySelector('ul, ol, li, div, p')).toBeNull()
  })

  it('still presents the side effects as separate lines', () => {
    renderDialog()
    const desc = document.body.querySelector('[data-slot="dialog-description"]')!
    // One block-displayed span per applicable side effect (rules, budget lines, sub-categories).
    expect(desc.querySelectorAll('span.block').length).toBeGreaterThanOrEqual(3)
  })
})
