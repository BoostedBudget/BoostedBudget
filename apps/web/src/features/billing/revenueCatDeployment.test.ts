import { describe, expect, it } from 'bun:test'
import { spawnSync } from 'bun'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { env } from 'node:process'

const root = resolve(import.meta.dir, '../../../../..')
describe('RevenueCat deployment configuration', () => {
  for (const workflow of ['deploy-to-staging.yaml', 'deploy-production.yml']) {
    it(`supplies only the public SDK key in ${workflow}`, () => {
      const source = readFileSync(resolve(root, '.github/workflows', workflow), 'utf8')
      expect(source).toContain('VITE_REVENUECAT_PUBLIC_API_KEY: ${{ secrets.VITE_REVENUECAT_PUBLIC_API_KEY }}')
      expect(source).not.toContain('VITE_REVENUECAT_SECRET')
    })
  }
  it('supports the public key for Docker runtime configuration', () => {
    const source = readFileSync(resolve(root, 'apps/web/docker-entrypoint.sh'), 'utf8')
    expect(source).toContain('REVENUECAT_PUBLIC_API_KEY:')
  })
  it('refuses unsafe runtime key characters before emitting JavaScript', () => {
    const source = readFileSync(resolve(root, 'apps/web/docker-entrypoint.sh'), 'utf8')
    const preamble = source.split('# Create runtime config file')[0]
    const result = spawnSync(['/bin/sh', '-c', preamble], { env: { ...env, REVENUECAT_PUBLIC_API_KEY: "';unexpected()//" } })
    expect(result.exitCode).not.toBe(0)
  })
})
