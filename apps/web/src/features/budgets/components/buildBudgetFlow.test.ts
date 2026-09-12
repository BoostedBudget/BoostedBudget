import { describe, expect, it } from 'bun:test'
import { buildBudgetFlow, type BudgetFlowLine } from './buildBudgetFlow'

const line = (id: string, planned: number, actual: number | null, kind: 'expense' | 'savings' = 'expense'): BudgetFlowLine => ({ id, label: id, kind, planned, actual })
const incoming = (graph: ReturnType<typeof buildBudgetFlow>, id: string) => graph.links.filter((l) => l.target === id).reduce((sum, l) => sum + l.value, 0)
const outgoing = (graph: ReturnType<typeof buildBudgetFlow>, id: string) => graph.links.filter((l) => l.source === id).reduce((sum, l) => sum + l.value, 0)

describe('buildBudgetFlow', () => {
  it('balances each allocation with separate above-plan and remaining flows', () => {
    const graph = buildBudgetFlow(1000, [line('food', 300, 400), line('fun', 300, 100), line('goal', 200, 150, 'savings')], true)
    expect(outgoing(graph, 'plan')).toBe(1000)
    expect(incoming(graph, 'unallocated')).toBe(200)
    for (const node of graph.nodes.filter((n) => n.kind === 'allocation')) {
      expect(incoming(graph, node.id)).toBe(outgoing(graph, node.id))
    }
    expect(graph.links.some((l) => l.source === 'above:food' && l.value === 100)).toBe(true)
    expect(graph.nodes.find((n) => n.id === 'actual:goal')?.label).toBe('Contributed')
    expect(graph.nodes.find((n) => n.id === 'remaining:goal')?.label).toBe('Still to contribute')
  })

  it('labels over-allocation as a synthetic shortfall, never observed income', () => {
    const graph = buildBudgetFlow(100, [line('food', 150, 120)], true)
    expect(graph.links.find((l) => l.source === 'plan-shortfall')?.value).toBe(50)
    expect(incoming(graph, 'plan')).toBe(150)
    expect(graph.nodes.some((n) => n.id === 'unallocated')).toBe(false)
  })

  it('keeps a planned-only view when actuals are unavailable', () => {
    const graph = buildBudgetFlow(100, [line('goal', 80, null, 'savings')], false)
    expect(graph.nodes.some((n) => n.kind === 'actual' || n.kind === 'remaining')).toBe(false)
    expect(incoming(graph, 'allocation:goal')).toBe(80)
    expect(incoming(graph, 'unallocated')).toBe(20)
  })

  it('never turns an unknown line into a zero actual if completeness disagrees', () => {
    const graph = buildBudgetFlow(100, [line('goal', 80, null, 'savings')], true)
    expect(graph.nodes.some((n) => n.kind === 'actual' || n.kind === 'remaining')).toBe(false)
  })

  it('collapses after ten allocations without netting overspend against remaining', () => {
    const lines = Array.from({ length: 10 }, (_, i) => line(`line${i}`, 100, 100))
    lines.push(line('over', 10, 30), line('under', 10, 0))
    const graph = buildBudgetFlow(1020, lines, true)
    expect(graph.nodes.filter((n) => n.kind === 'allocation')).toHaveLength(11)
    expect(graph.nodes.find((n) => n.id === 'allocation:more')?.label).toBe('More allocations')
    expect(graph.links.find((l) => l.source === 'above:more')?.value).toBe(20)
    expect(graph.links.find((l) => l.target === 'remaining:more')?.value).toBe(10)
    expect(incoming(graph, 'allocation:more')).toBe(outgoing(graph, 'allocation:more'))
    expect(graph.hiddenLineIds).toEqual(['over', 'under'])
  })

  it('uses cent arithmetic and omits zero-width flows', () => {
    const graph = buildBudgetFlow(0.3, [line('a', 0.1, 0.1), line('b', 0.2, 0.2), line('empty', 0, 0)], true)
    expect(graph.nodes.some((n) => n.id === 'plan-shortfall' || n.id === 'unallocated')).toBe(false)
    expect(graph.links.every((l) => l.value > 0)).toBe(true)
  })

  it('does not label collapsed savings contributions as spending', () => {
    const lines = Array.from({ length: 10 }, (_, i) => line(`line${i}`, 100, 100))
    lines.push(line('expense', 10, 20), line('saving', 20, 10, 'savings'))
    const graph = buildBudgetFlow(1030, lines, true)
    expect(graph.nodes.find((n) => n.id === 'actual:more')?.label).toBe('Spent')
    expect(graph.nodes.find((n) => n.id === 'actual:more-savings')?.label).toBe('Contributed')
    expect(graph.links.find((l) => l.target === 'actual:more-savings')?.value).toBe(10)
  })
})
