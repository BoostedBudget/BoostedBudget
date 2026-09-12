import { z } from 'zod'

/** Keep omission intact: confirmation binds both the value and presence of detail. */
export const executeCallSchema = z.object({
  operationId: z.string().describe('The operationId from search() results'),
  params: z.record(z.string(), z.unknown()).optional().describe('Query parameters'),
  body: z.record(z.string(), z.unknown()).optional().describe('Request body (including safe POST previews)'),
  pathParams: z.record(z.string(), z.string()).optional().describe('Path parameters (e.g. { id: "abc" })'),
  detail: z.enum(['compact', 'full']).optional().describe('Response detail: compact by default; full returns the original payload subject to output limits'),
})
