import { z } from 'zod'

// Zod schema — validated at startup via parseEnv()
// URL validation is env-aware: http:// is only allowed in development.
const EnvSchema = z
  .object({
    CLERK_SECRET_KEY: z.string().min(1),
    CLERK_PUBLISHABLE_KEY: z.string().min(1),
    CLERK_JWKS_URL: z.string().url(),
    BOOSTEDBUDGET_API_URL: z.string().url(),
    BOOSTEDBUDGET_ORIGIN_SECRET: z.string().min(1),
    MCP_RESOURCE_URL: z.string().url(),
    ENVIRONMENT: z.enum(['production', 'staging', 'development']),
  })
  .superRefine((data, ctx) => {
    if (data.ENVIRONMENT !== 'development') {
      if (!data.BOOSTEDBUDGET_API_URL.startsWith('https://')) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: 'BOOSTEDBUDGET_API_URL must use https:// in non-development environments',
          path: ['BOOSTEDBUDGET_API_URL'],
        })
      }
    }
  })

export type ParsedEnv = z.infer<typeof EnvSchema>

/**
 * Parse and validate the Cloudflare Worker environment bindings.
 * Throws on validation failure. Call once in the Worker fetch handler before request processing.
 */
export function parseEnv(raw: unknown): ParsedEnv {
  return EnvSchema.parse(raw)
}

/**
 * Full Cloudflare Worker Env interface (includes DO + dispatch bindings added in later phases).
 * Extend this as new bindings are added.
 */
export interface Env extends ParsedEnv {
  // Phase 4: Durable Object binding for McpAgent
  // MCP_AGENT: DurableObjectNamespace

  // Phase 4: Dynamic Worker Loader / dispatch namespace for sandbox
  // DISPATCHER: DispatchNamespace
}
