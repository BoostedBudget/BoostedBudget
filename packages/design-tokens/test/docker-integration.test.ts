import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const dockerfile = readFileSync(resolve(import.meta.dirname, '../../../apps/web/Dockerfile'), 'utf8')

describe('web Docker design-token workspace integration', () => {
  it('copies the design-token manifest before npm creates workspace links', () => {
    const manifestCopy = dockerfile.indexOf(
      'COPY packages/design-tokens/package.json ./packages/design-tokens/',
    )
    const install = dockerfile.indexOf('RUN npm install --legacy-peer-deps')
    const packageSources = dockerfile.indexOf('COPY packages/ ./packages/')

    expect(manifestCopy).toBeGreaterThan(-1)
    expect(manifestCopy).toBeLessThan(install)
    expect(install).toBeLessThan(packageSources)
  })
})
