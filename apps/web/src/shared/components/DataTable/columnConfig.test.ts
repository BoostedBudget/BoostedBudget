import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

// Root of the web app workspace
const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../../../../')

/**
 * For each consumer file, list the column IDs that must declare defaultSize.
 * This acts as a regression guard: if a new column is added without defaultSize,
 * or if defaultSize is removed, this test fails.
 *
 * We read source files and assert that defaultSize appears within 1200 characters
 * of each column's id declaration — enough to cover any column def object, even
 * those with verbose render functions.
 */
const CONSUMERS: Array<{ file: string; columnIds: string[] }> = [
  {
    file: 'src/features/reports/components/TopMerchantsTable.tsx',
    columnIds: ['merchant_name', 'total', 'transaction_count', 'percentage'],
  },
  {
    file: 'src/features/net-worth/components/AccountBreakdownTable.tsx',
    columnIds: ['institution', 'name', 'type', 'balance', 'percent'],
  },
  {
    file: 'src/features/admin/components/DeadLetterPage.tsx',
    columnIds: ['kind', 'queue', 'user_id', 'failed_at', 'status', 'notes'],
  },
  {
    file: 'src/features/admin/components/UsersPage.tsx',
    columnIds: ['email', 'tier', 'status', 'override_status', 'region', 'last_login', 'accounts', 'rules', 'referrals'],
  },
  {
    file: 'src/features/admin/components/WebhooksPage.tsx',
    columnIds: ['event_type', 'event_id', 'status', 'attempts', 'error', 'created_at'],
  },
  {
    file: 'src/features/auto-categorization/components/AutoCategorizationRuleTable.tsx',
    columnIds: ['name', 'status', 'priority', 'conditions', 'category', 'matches'],
  },
  {
    file: 'src/features/transactions/components/TransactionTable.tsx',
    columnIds: ['date', 'merchant', 'account', 'category', 'amount'],
  },
  {
    file: 'src/features/accounts/components/AccountTable.tsx',
    columnIds: ['institution', 'name', 'type', 'mask', 'balance'],
  },
  {
    file: 'src/features/budgets/components/BudgetTable.tsx',
    columnIds: ['name', 'period', 'amount', 'income', 'progress', 'remaining', 'categories'],
  },
  {
    file: 'src/features/rules/components/RuleTable.tsx',
    // 'select' is intentionally fixed-width (48/48/48) — still must have defaultSize
    columnIds: ['select', 'name', 'type', 'status', 'trigger', 'lastRun', 'priority'],
  },
  {
    file: 'src/features/rules/components/RuleExecutionHistory.tsx',
    columnIds: ['time', 'status', 'duration', 'trigger', 'details'],
  },
]

describe('DataTable consumer column configs', () => {
  for (const { file, columnIds } of CONSUMERS) {
    describe(file.split('/').pop()!, () => {
      const src = readFileSync(resolve(ROOT, file), 'utf8')

      for (const id of columnIds) {
        it(`column "${id}" declares defaultSize`, () => {
          const idPattern = new RegExp(`id:\\s*['"]${id}['"]`)
          const match = idPattern.exec(src)
          expect(match).not.toBeNull()
          if (!match) return

          // Find the end of this column's object: advance to the next sibling
          // `id:` declaration or EOF, so defaultSize from a later column cannot
          // satisfy the assertion for a column that is missing its own.
          const afterId = match.index + match[0].length
          const nextIdMatch = /id:\s*['"][a-z_]+['"]/g
          nextIdMatch.lastIndex = afterId
          const sibling = nextIdMatch.exec(src)
          const end = sibling ? sibling.index : src.length
          const block = src.slice(match.index, end)

          expect(block).toMatch(/defaultSize\s*:/)
        })
      }
    })
  }
})
