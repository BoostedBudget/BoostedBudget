import type { IncomeSourceItem, SpendingByCategory } from '@boostedbudget/api-client/models'
import { CASH_FLOW_COLORS, CATEGORY_COLORS } from '@/shared/charts'

const MAX_EXPENSE_NODES = 10

export interface SankeyNode {
  id: string
  name: string
  itemStyle: { color: string }
  navigable: boolean
  nodeKind: 'income' | 'expense' | 'hub' | 'savings' | 'deficit' | 'other'
  category?: string
  isCustom?: true
}

export interface SankeyLink {
  source: string
  target: string
  value: number
}

export interface SankeyData {
  nodes: SankeyNode[]
  links: SankeyLink[]
}

export function buildSankeyData(
  income: IncomeSourceItem[],
  expenses: SpendingByCategory[],
  totalIncome: number,
  totalExpenses: number,
): SankeyData {
  if (totalIncome <= 0 && totalExpenses <= 0) {
    return { nodes: [], links: [] }
  }

  const validIncome = income.filter(i => i.total > 0)
  const validExpenses = expenses.filter(e => e.total > 0)

  if (validIncome.length === 0 && validExpenses.length === 0) {
    return { nodes: [], links: [] }
  }

  const nodes: SankeyNode[] = []
  const links: SankeyLink[] = []

  nodes.push({
    id: 'hub',
    name: 'Total Income',
    itemStyle: { color: CASH_FLOW_COLORS.income },
    navigable: false,
    nodeKind: 'hub',
  })

  for (const src of validIncome) {
    const id = `in:${src.category}`
    nodes.push({
      id,
      name: src.pfc_primary_display ?? src.category,
      itemStyle: { color: CASH_FLOW_COLORS.income },
      navigable: true,
      nodeKind: 'income',
      category: src.category,
    })
    links.push({ source: id, target: 'hub', value: src.total })
  }

  const incomeSum = validIncome.reduce((s, i) => s + i.total, 0)
  const expenseSum = validExpenses.reduce((s, e) => s + e.total, 0)
  const surplus = incomeSum - expenseSum
  if (surplus < 0) {
    nodes.push({
      id: 'deficit',
      name: 'Drawn from Savings / Credit',
      itemStyle: { color: CASH_FLOW_COLORS.netNegative },
      navigable: false,
      nodeKind: 'deficit',
    })
    links.push({ source: 'deficit', target: 'hub', value: -surplus })
  }

  const topExpenses = [...validExpenses]
    .sort((a, b) => b.total - a.total)
    .slice(0, MAX_EXPENSE_NODES)
  const otherExpenses = validExpenses.length > MAX_EXPENSE_NODES
    ? validExpenses.sort((a, b) => b.total - a.total).slice(MAX_EXPENSE_NODES)
    : []

  topExpenses.forEach((exp, i) => {
    const id = `out:${exp.category}`
    nodes.push({
      id,
      name: exp.pfc_primary_display ?? exp.category,
      itemStyle: { color: CATEGORY_COLORS[i % CATEGORY_COLORS.length] },
      navigable: true,
      nodeKind: 'expense',
      category: exp.category,
      ...(exp.category.startsWith('custom:') && { isCustom: true as const }),
    })
    links.push({ source: 'hub', target: id, value: exp.total })
  })

  if (otherExpenses.length > 0) {
    const otherTotal = otherExpenses.reduce((s, e) => s + e.total, 0)
    nodes.push({
      id: 'other',
      name: `Other Expenses (${otherExpenses.length} categories)`,
      itemStyle: { color: CATEGORY_COLORS[topExpenses.length % CATEGORY_COLORS.length] },
      navigable: false,
      nodeKind: 'other',
    })
    links.push({ source: 'hub', target: 'other', value: otherTotal })
  }

  if (surplus > 0) {
    nodes.push({
      id: 'savings',
      name: 'Savings',
      itemStyle: { color: CASH_FLOW_COLORS.netPositive },
      navigable: false,
      nodeKind: 'savings',
    })
    links.push({ source: 'hub', target: 'savings', value: surplus })
  }

  return { nodes, links }
}
