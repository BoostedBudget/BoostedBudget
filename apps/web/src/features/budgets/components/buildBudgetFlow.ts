export interface BudgetFlowLine {
  id: string
  label: string
  kind: 'expense' | 'savings' | 'unbudgeted'
  planned: number
  actual: number | null
}

export interface BudgetFlowNode {
  id: string
  label: string
  kind: 'source' | 'plan' | 'allocation' | 'actual' | 'remaining' | 'synthetic'
  lineId?: string
}

export interface BudgetFlowGraph {
  nodes: BudgetFlowNode[]
  links: { source: string; target: string; value: number }[]
  hiddenLineIds: string[]
}

const cents = (value: number) => Math.round(value * 100)
const VISIBLE_ALLOCATIONS = 10

/** Layout only: eligibility, matching and currency decisions belong to the server. */
export function buildBudgetFlow(expected: number, lines: BudgetFlowLine[], showActuals: boolean): BudgetFlowGraph {
  showActuals = showActuals && lines.every((line) => line.actual != null)
  const graph: BudgetFlowGraph = { nodes: [], links: [], hiddenLineIds: [] }
  const node = (id: string, label: string, kind: BudgetFlowNode['kind'], lineId?: string) => {
    graph.nodes.push({ id, label, kind, ...(lineId && { lineId }) })
  }
  const link = (source: string, target: string, amount: number) => {
    if (amount > 0) graph.links.push({ source, target, value: amount / 100 })
  }
  const active = lines.filter((l) => cents(l.planned) > 0 || (showActuals && l.actual != null && cents(l.actual) > 0))
  const allocated = active.reduce((sum, l) => sum + cents(l.planned), 0)
  const expectedCents = cents(expected)
  if (allocated === 0 && expectedCents === 0 && active.length === 0) return graph
  node('expected', 'Expected income', 'source')
  node('plan', 'Budget plan', 'plan')
  link('expected', 'plan', expectedCents)
  if (allocated > expectedCents) {
    node('plan-shortfall', 'Over-allocation shortfall (not income)', 'synthetic')
    link('plan-shortfall', 'plan', allocated - expectedCents)
  }
  if (expectedCents > allocated) {
    node('unallocated', 'Unallocated', 'synthetic')
    link('plan', 'unallocated', expectedCents - allocated)
  }

  const outcomes = (id: string, allocationId: string, members: BudgetFlowLine[], savings: boolean) => {
    const actual = members.reduce((sum, l) => sum + cents(l.actual ?? 0), 0)
    const above = members.reduce((sum, l) => sum + Math.max(0, cents(l.actual ?? 0) - cents(l.planned)), 0)
    const remaining = members.reduce((sum, l) => sum + Math.max(0, cents(l.planned) - cents(l.actual ?? 0)), 0)
    if (above > 0) {
      node(`above:${id}`, savings ? 'Above target (not income)' : 'Above plan (not income)', 'synthetic')
      link(`above:${id}`, allocationId, above)
    }
    if (actual > 0) {
      node(`actual:${id}`, savings ? 'Contributed' : 'Spent', 'actual')
      link(allocationId, `actual:${id}`, actual)
    }
    if (remaining > 0) {
      node(`remaining:${id}`, savings ? 'Still to contribute' : 'Remaining allocation', 'remaining')
      link(allocationId, `remaining:${id}`, remaining)
    }
  }
  for (const item of active.slice(0, VISIBLE_ALLOCATIONS)) {
    const id = `allocation:${item.id}`
    node(id, item.label, 'allocation', item.id)
    link('plan', id, cents(item.planned))
    if (showActuals) outcomes(item.id, id, [item], item.kind === 'savings')
  }
  const hidden = active.slice(VISIBLE_ALLOCATIONS)
  if (hidden.length) {
    graph.hiddenLineIds = hidden.map((l) => l.id)
    node('allocation:more', 'More allocations', 'allocation')
    link('plan', 'allocation:more', hidden.reduce((sum, l) => sum + cents(l.planned), 0))
    if (showActuals) {
      outcomes('more', 'allocation:more', hidden.filter((l) => l.kind !== 'savings'), false)
      outcomes('more-savings', 'allocation:more', hidden.filter((l) => l.kind === 'savings'), true)
    }
  }
  // ECharts should never receive disconnected, zero-width source nodes.
  const connected = new Set(graph.links.flatMap((l) => [l.source, l.target]))
  graph.nodes = graph.nodes.filter((n) => connected.has(n.id))
  return graph
}
