import { describe, it, expect } from 'bun:test'
import { buildSankeyData } from './buildSankeyData'
import type { IncomeSourceItem, SpendingByCategory } from '@boostedbudget/api-client/models'

function makeIncome(category: string, display: string, total: number): IncomeSourceItem {
  return { category, pfc_primary_display: display, total, percentage: 0, transaction_count: 1 }
}

function makeExpense(category: string, display: string, total: number): SpendingByCategory {
  return { category, pfc_primary_display: display, total, percentage: 0, transaction_count: 1 }
}

describe('buildSankeyData', () => {
  describe('empty/zero cases', () => {
    it('returns empty when both totals are zero', () => {
      expect(buildSankeyData([], [], 0, 0)).toEqual({ nodes: [], links: [], hubTotal: 0 })
    })

    it('returns empty when items exist but all totals are zero', () => {
      const result = buildSankeyData(
        [makeIncome('WAGES', 'Wages', 0)],
        [makeExpense('FOOD', 'Food', 0)],
        0, 0,
      )
      expect(result).toEqual({ nodes: [], links: [], hubTotal: 0 })
    })

    it('filters out income items with total <= 0', () => {
      const result = buildSankeyData(
        [makeIncome('WAGES', 'Wages', 5000), makeIncome('DIVIDENDS', 'Dividends', 0)],
        [makeExpense('FOOD', 'Food', 2000)],
        5000, 2000,
      )
      const ids = result.nodes.map(n => n.id)
      expect(ids).toContain('in:WAGES')
      expect(ids).not.toContain('in:DIVIDENDS')
    })

    it('filters out expense items with total <= 0', () => {
      const result = buildSankeyData(
        [makeIncome('WAGES', 'Wages', 5000)],
        [makeExpense('FOOD', 'Food', 2000), makeExpense('TRANSPORT', 'Transport', 0)],
        5000, 2000,
      )
      expect(result.nodes.map(n => n.id)).not.toContain('out:TRANSPORT')
    })
  })

  describe('surplus case (income > expenses)', () => {
    const income = [
      makeIncome('WAGES', 'Wages', 5000),
      makeIncome('DIVIDENDS', 'Dividends', 1250),
    ]
    const expenses = [
      makeExpense('FOOD', 'Food & Drink', 800),
      makeExpense('TRANSPORT', 'Transportation', 400),
    ]
    const totalIncome = 6250
    const totalExpenses = 1200

    it('creates a hub node that is non-navigable', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const hub = nodes.find(n => n.id === 'hub')
      expect(hub).toBeDefined()
      expect(hub?.navigable).toBe(false)
    })

    it('creates income nodes with in: prefix, correct name, navigable=true', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const wages = nodes.find(n => n.id === 'in:WAGES')
      expect(wages).toBeDefined()
      expect(wages?.name).toBe('Wages')
      expect(wages?.navigable).toBe(true)
      expect(wages?.nodeKind).toBe('income')
      expect(wages?.category).toBe('WAGES')
    })

    it('creates expense nodes with out: prefix, correct name, navigable=true', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const food = nodes.find(n => n.id === 'out:FOOD')
      expect(food).toBeDefined()
      expect(food?.name).toBe('Food & Drink')
      expect(food?.navigable).toBe(true)
      expect(food?.nodeKind).toBe('expense')
      expect(food?.category).toBe('FOOD')
    })

    it('creates a Savings node that is non-navigable', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const savings = nodes.find(n => n.id === 'savings')
      expect(savings).toBeDefined()
      expect(savings?.navigable).toBe(false)
      expect(savings?.nodeKind).toBe('savings')
    })

    it('does NOT create a deficit node when in surplus', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      expect(nodes.find(n => n.id === 'deficit')).toBeUndefined()
    })

    it('links each income source to hub with its exact total', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      expect(links.find(l => l.source === 'in:WAGES' && l.target === 'hub')?.value).toBe(5000)
      expect(links.find(l => l.source === 'in:DIVIDENDS' && l.target === 'hub')?.value).toBe(1250)
    })

    it('links hub to each expense with its exact total', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      expect(links.find(l => l.source === 'hub' && l.target === 'out:FOOD')?.value).toBe(800)
      expect(links.find(l => l.source === 'hub' && l.target === 'out:TRANSPORT')?.value).toBe(400)
    })

    it('links hub to Savings with surplus value (totalIncome - totalExpenses)', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const savingsLink = links.find(l => l.source === 'hub' && l.target === 'savings')
      expect(savingsLink?.value).toBeCloseTo(6250 - 1200)
    })

    it('hub inflow equals hub outflow (invariant)', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const inflow = links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
      const outflow = links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
      expect(inflow).toBeCloseTo(outflow)
    })
  })

  describe('deficit case (expenses > income)', () => {
    const income = [makeIncome('WAGES', 'Wages', 1000)]
    const expenses = [makeExpense('FOOD', 'Food', 1200), makeExpense('RENT', 'Rent', 800)]
    const totalIncome = 1000
    const totalExpenses = 2000

    it('creates a deficit node that is non-navigable', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const deficit = nodes.find(n => n.id === 'deficit')
      expect(deficit).toBeDefined()
      expect(deficit?.navigable).toBe(false)
      expect(deficit?.nodeKind).toBe('deficit')
    })

    it('does NOT create a savings node when in deficit', () => {
      const { nodes } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      expect(nodes.find(n => n.id === 'savings')).toBeUndefined()
    })

    it('links deficit to hub with the shortfall value', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const defLink = links.find(l => l.source === 'deficit' && l.target === 'hub')
      expect(defLink?.value).toBe(1000)
    })

    it('hub inflow equals hub outflow (invariant)', () => {
      const { links } = buildSankeyData(income, expenses, totalIncome, totalExpenses)
      const inflow = links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
      const outflow = links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
      expect(inflow).toBeCloseTo(outflow)
    })
  })

  describe('exact-balance case (income === expenses)', () => {
    it('creates neither savings nor deficit nodes', () => {
      const { nodes } = buildSankeyData(
        [makeIncome('WAGES', 'Wages', 2000)],
        [makeExpense('FOOD', 'Food', 2000)],
        2000, 2000,
      )
      expect(nodes.find(n => n.id === 'savings')).toBeUndefined()
      expect(nodes.find(n => n.id === 'deficit')).toBeUndefined()
    })
  })

  describe('top-10 expense cap', () => {
    const income = [makeIncome('WAGES', 'Wages', 100000)]
    const manyExpenses = Array.from({ length: 15 }, (_, i) =>
      makeExpense(`CAT_${i}`, `Category ${i}`, 500 + i * 100),
    )
    const totalExpenses = manyExpenses.reduce((s, e) => s + e.total, 0)

    it('keeps exactly 10 expense nodes (the top 10 by total)', () => {
      const { nodes } = buildSankeyData(income, manyExpenses, 100000, totalExpenses)
      expect(nodes.filter(n => n.id.startsWith('out:')).length).toBe(10)
    })

    it('creates an "other" node with the correct count and non-navigable', () => {
      const { nodes } = buildSankeyData(income, manyExpenses, 100000, totalExpenses)
      const other = nodes.find(n => n.id === 'other')
      expect(other?.name).toBe('Other Expenses (5 categories)')
      expect(other?.navigable).toBe(false)
      expect(other?.nodeKind).toBe('other')
    })

    it('other node link carries the summed total of collapsed expenses', () => {
      const { links } = buildSankeyData(income, manyExpenses, 100000, totalExpenses)
      // Smallest 5 expenses: index 0-4 → totals 500,600,700,800,900 = 3500
      const expectedOtherValue = 500 + 600 + 700 + 800 + 900
      const otherLink = links.find(l => l.target === 'other')
      expect(otherLink?.value).toBe(expectedOtherValue)
    })

    it('does not create an other node when expenses are 10 or fewer', () => {
      const tenExpenses = manyExpenses.slice(0, 10)
      const tenTotal = tenExpenses.reduce((s, e) => s + e.total, 0)
      const { nodes } = buildSankeyData(income, tenExpenses, 100000, tenTotal)
      expect(nodes.find(n => n.id === 'other')).toBeUndefined()
    })
  })

  describe('transfer category collision handling', () => {
    it('in:/out: prefixes keep income and expense node ids disjoint', () => {
      const inc = [makeIncome('TRANSFER_IN', 'Transfer In', 1000)]
      const exp = [makeExpense('TRANSFER_OUT', 'Transfer Out', 800)]
      const { nodes } = buildSankeyData(inc, exp, 1000, 800)
      const ids = nodes.map(n => n.id)
      expect(ids).toContain('in:TRANSFER_IN')
      expect(ids).toContain('out:TRANSFER_OUT')
    })
  })

  describe('hub invariant with mismatched passed-in totals (G5 — Codex fix #5 regression guard)', () => {
    it('hub inflow equals hub outflow even when passed totalIncome/totalExpenses diverge from array sums', () => {
      const income = [makeIncome('WAGES', 'Wages', 3000)]
      const expenses = [makeExpense('FOOD', 'Food', 2000)]
      // Pass intentionally wrong totals (simulates API total including filtered rows)
      const { links } = buildSankeyData(income, expenses, /*wrong*/ 9999, /*wrong*/ 50)
      const inflow = links.filter(l => l.target === 'hub').reduce((s, l) => s + l.value, 0)
      const outflow = links.filter(l => l.source === 'hub').reduce((s, l) => s + l.value, 0)
      expect(inflow).toBeCloseTo(outflow)
    })

    it('hubTotal in the result equals max of filtered array sums, not passed-in params', () => {
      const income = [makeIncome('WAGES', 'Wages', 3000)]
      const expenses = [makeExpense('FOOD', 'Food', 2000)]
      // Correct hubTotal = max(3000, 2000) = 3000; wrong params should be ignored
      const { hubTotal } = buildSankeyData(income, expenses, /*wrong*/ 9999, /*wrong*/ 50)
      expect(hubTotal).toBe(3000)
    })
  })

  describe('custom categories', () => {
    it('marks custom category expense nodes with isCustom: true', () => {
      const exp = [makeExpense('custom:uuid-abc', 'My Budget', 500)]
      const { nodes } = buildSankeyData([makeIncome('WAGES', 'Wages', 1000)], exp, 1000, 500)
      const node = nodes.find(n => n.id === 'out:custom:uuid-abc')
      expect(node?.isCustom).toBe(true)
      expect(node?.navigable).toBe(true)
    })

    it('does not set isCustom on regular PFC expense nodes', () => {
      const exp = [makeExpense('FOOD_AND_DRINK', 'Food', 500)]
      const { nodes } = buildSankeyData([makeIncome('WAGES', 'Wages', 1000)], exp, 1000, 500)
      const node = nodes.find(n => n.id === 'out:FOOD_AND_DRINK')
      expect(node?.isCustom).toBeUndefined()
    })
  })

  describe('node ids are unique', () => {
    it('all node ids in the result are unique', () => {
      const inc = [makeIncome('WAGES', 'Wages', 3000), makeIncome('OTHER', 'Other', 1000)]
      const exp = [makeExpense('FOOD', 'Food', 2000), makeExpense('TRAVEL', 'Travel', 500)]
      const { nodes } = buildSankeyData(inc, exp, 4000, 2500)
      const ids = nodes.map(n => n.id)
      expect(new Set(ids).size).toBe(ids.length)
    })
  })
})
