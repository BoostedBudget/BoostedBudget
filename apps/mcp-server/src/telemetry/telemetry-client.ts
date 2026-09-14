import { MCP_WORKER_USER_AGENT } from '../client/user-agent'
import type { MCPToolTelemetry } from './tool-telemetry'

interface MCPToolTelemetryClientOptions {
  baseUrl: string
  originSecret: string
  workerSecret: string
  getAccessToken: () => string | undefined
  timeoutMilliseconds?: number
}

type FailureClass = 'authentication' | 'http_4xx' | 'http_5xx' | 'http_other' | 'network' | 'timeout'

function warnDeliveryFailure(failureClass: FailureClass): void {
  console.warn('[mcp-telemetry] delivery failed', { failureClass })
}

function httpFailureClass(status: number): FailureClass {
  if (status >= 400 && status < 500) return 'http_4xx'
  if (status >= 500 && status < 600) return 'http_5xx'
  return 'http_other'
}

export function createMCPToolTelemetryClient(options: MCPToolTelemetryClientOptions): MCPToolTelemetry {
  return {
    async record(completion): Promise<void> {
      const accessToken = options.getAccessToken()
      if (!accessToken) {
        warnDeliveryFailure('authentication')
        return
      }

      try {
        const response = await fetch(`${options.baseUrl}/api/v1/mcp/telemetry`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'User-Agent': MCP_WORKER_USER_AGENT,
            Authorization: `Bearer ${accessToken}`,
            'X-Origin-Secret': options.originSecret,
            'X-MCP-Worker-Secret': options.workerSecret,
          },
          body: JSON.stringify(completion),
          signal: AbortSignal.timeout(options.timeoutMilliseconds ?? 750),
        })
        if (!response.ok) warnDeliveryFailure(httpFailureClass(response.status))
      } catch (error) {
        warnDeliveryFailure(error instanceof DOMException && error.name === 'TimeoutError' ? 'timeout' : 'network')
      }
    },
  }
}
