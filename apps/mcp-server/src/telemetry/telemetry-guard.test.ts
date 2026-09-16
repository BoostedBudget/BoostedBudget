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

  it('keeps the generated endpoint client explicit about its required Worker secret header', () => {
    const client = readFileSync(
      resolve(import.meta.dir, '../../../../packages/api-client/src/endpoints/mcp-telemetry/mcp-telemetry.ts'),
      'utf8',
    )
    const headersModel = readFileSync(
      resolve(import.meta.dir, '../../../../packages/api-client/src/models/recordMCPToolTelemetryHeaders.ts'),
      'utf8',
    )

    expect(client).toContain('headers: RecordMCPToolTelemetryHeaders')
    expect(client).toContain("headers: {'Content-Type': 'application/json', ...headers}")
    expect(headersModel).toContain("'X-MCP-Worker-Secret': string")
    expect(headersModel).not.toContain("'X-MCP-Worker-Secret'?: string")
  })

  it('does not add typed header parameters to unrelated generated endpoint signatures', () => {
    const endpointRoot = resolve(import.meta.dir, '../../../../packages/api-client/src/endpoints')
    const endpointsWithHeaderParameters = productionFiles(endpointRoot)
      .filter(path => /headers\??:\s*\w+Headers/.test(readFileSync(path, 'utf8')))
      .map(path => path.slice(endpointRoot.length + 1))

    expect(endpointsWithHeaderParameters).toEqual(['mcp-telemetry/mcp-telemetry.ts'])
  })
})
