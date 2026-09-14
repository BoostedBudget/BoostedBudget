import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const workflow = readFileSync(resolve(import.meta.dirname, '../../../.github/workflows/test.yml'), 'utf8')

describe('design-token CI integration', () => {
  it('checks committed generated artifacts in the frontend job', () => {
    const frontendJob = workflow.match(/  frontend-test:\n([\s\S]*?)(?=\n  [a-z][a-z0-9-]+:\n)/)?.[1]

    expect(frontendJob).toBeDefined()
    expect(frontendJob).toContain('bun run --filter @boostedbudget/design-tokens check')
  })
})
