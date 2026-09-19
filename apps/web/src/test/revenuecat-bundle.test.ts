import { describe, expect, it } from 'bun:test'

// Load the build configuration at runtime; it is outside the composite test
// project's source list and is not itself a browser/test source module.
const { default: config } = await import(new URL('../../vite.config.ts', import.meta.url).href)

// Exercise the configured matchers: a dynamic import is ineffective when its
// module is assigned to the eagerly loaded vendor catch-all.
type ChunkGroup = { name: string; test: RegExp; priority: number }
const groups = (config as { build: { rolldownOptions: { output: { codeSplitting: { groups: ChunkGroup[] } } } } })
  .build.rolldownOptions.output.codeSplitting.groups

describe('RevenueCat checkout bundle isolation', () => {
  it('assigns the SDK to a separate chunk before the eager vendor catch-all', () => {
    for (const id of [
      '/repo/node_modules/@revenuecat/purchases-js/dist/Purchases.es.js',
      '/repo/node_modules/.bun/@revenuecat+purchases-js@1.0.0/node_modules/@revenuecat/purchases-js/dist/Purchases.es.js',
    ]) {
      const matches = groups.filter(group => group.test.test(id)).sort((a, b) => b.priority - a.priority)
      expect(matches[0]?.name).toBe('revenuecat')
      expect(matches.some(group => group.name === 'vendor')).toBe(true)
    }
  })

  it('keeps unrelated vendor modules in their existing chunk', () => {
    const matches = groups.filter(group => group.test.test('/repo/node_modules/react/index.js'))
      .sort((a, b) => b.priority - a.priority)
    expect(matches[0]?.name).toBe('vendor')
  })
})
