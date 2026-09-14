import { describe, expect, it } from 'bun:test'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, resolve } from 'node:path'

function productionFiles(directory: string): string[] {
  return readdirSync(directory).flatMap(name => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return productionFiles(path)
    return name.endsWith('.ts') && !name.endsWith('.test.ts') ? [path] : []
  })
}

describe('MCP telemetry architecture guard', () => {
  it('has no direct PostHog import, host, token, or capture in Worker production code', () => {
    const sourceRoot = resolve(import.meta.dir, '..')
    const configurationFiles = [
      resolve(sourceRoot, '../package.json'),
      resolve(sourceRoot, '../wrangler.toml'),
    ]
    const findings = [...productionFiles(sourceRoot), ...configurationFiles].flatMap(path => {
      const source = readFileSync(path, 'utf8')
      return /(?:from|import\s*)[^\n]*posthog|(?:posthog-js|posthog-node)|(?:app|us|eu)\.posthog\.com|POSTHOG_API_KEY|\.capture\s*\(/i.test(source) ? [path] : []
    })
    expect(findings).toEqual([])
  })

  it('wraps every registered MCP tool exactly once through the central completion wrapper', () => {
    const sources = productionFiles(resolve(import.meta.dir, '..')).map(path => readFileSync(path, 'utf8'))
    const registrations = sources.flatMap(source => source.match(/\.registerTool\s*\(/g) ?? [])
    const wrappers = sources.flatMap(source => source.match(/instrumentTool\s*\(\s*\{/g) ?? [])

    expect(registrations).toHaveLength(7)
    expect(wrappers).toHaveLength(registrations.length)
  })
})
