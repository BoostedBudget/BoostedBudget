import { describe, expect, it, mock, beforeEach } from 'bun:test'
import { createAuditClient, type AuditEntry } from './audit-client'
import { MCP_WORKER_USER_AGENT } from '../client/user-agent'
import type { Env } from '../env'

const ENV = {
  BOOSTEDBUDGET_API_URL: 'https://api.boostedbudget.com',
  BOOSTEDBUDGET_ORIGIN_SECRET: 'origin-secret-value',
  MCP_WORKER_SECRET: 'worker-secret-value',
} as unknown as Env

const ENTRY: AuditEntry = {
  actorUserId: 'user_123',
  grantId: 'grant-abc',
  operationClass: 'read',
  operationId: 'getUserAccounts',
  result: 'success',
}

describe('createAuditClient', () => {
  let fetchMock: ReturnType<typeof mock>

  beforeEach(() => {
    fetchMock = mock(() => Promise.resolve(new Response(null, { status: 204 })))
    globalThis.fetch = fetchMock
  })

  it('sends a non-empty User-Agent header (avoids CF "no User-Agent" WAF challenge)', async () => {
    const client = createAuditClient(ENV)

    await client.record(ENTRY)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/mcp/audit')
    const headers = init.headers as Record<string, string>
    expect(headers['User-Agent']).toBe(MCP_WORKER_USER_AGENT)
  })

  it('is a no-op when env is not configured', async () => {
    const client = createAuditClient({} as Env)
    await client.record(ENTRY)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
