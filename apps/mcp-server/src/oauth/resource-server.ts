/**
 * OAuth 2.0 Protected Resource Metadata (RFC 9728).
 *
 * Serves the .well-known/oauth-protected-resource document that tells MCP
 * clients where to find the authorization server (Clerk) and what scopes
 * are supported. Also builds the WWW-Authenticate challenge for 401 responses.
 *
 * Spike result: Clerk only supports built-in scopes (openid, email, profile,
 * public_metadata, private_metadata). Custom scopes (mcp:read/mcp:write) are
 * not supported — authorization granularity is enforced by the capability manifest.
 */

interface ResourceServerEnv {
  MCP_RESOURCE_URL: string
  CLERK_JWKS_URL: string
}

export interface ProtectedResourceMetadata {
  resource: string
  authorization_servers: string[]
  scopes_supported: string[]
  bearer_methods_supported: string[]
}

/**
 * Build the RFC 9728 protected-resource metadata document.
 * Served at /.well-known/oauth-protected-resource.
 */
export function buildProtectedResourceMetadata(
  env: ResourceServerEnv,
): ProtectedResourceMetadata {
  const jwksUrl = new URL(env.CLERK_JWKS_URL)
  const clerkAsBase = `${jwksUrl.protocol}//${jwksUrl.host}`

  return {
    resource: env.MCP_RESOURCE_URL,
    authorization_servers: [clerkAsBase],
    scopes_supported: ['openid', 'email', 'profile', 'public_metadata'],
    bearer_methods_supported: ['header'],
  }
}

/**
 * Build the WWW-Authenticate response header for 401 challenges (RFC 6750 + RFC 9728).
 *
 * Example output:
 *   Bearer resource_metadata="https://mcp.boostedbudget.com/.well-known/oauth-protected-resource", error="invalid_token"
 */
export function buildWwwAuthenticate(resourceUrl: string, errorCode?: string): string {
  const baseUrl = resourceUrl.replace(/\/$/, '')
  const metadataUrl = `${baseUrl}/.well-known/oauth-protected-resource`
  let header = `Bearer resource_metadata="${metadataUrl}"`
  if (errorCode) {
    header += `, error="${errorCode}"`
  }
  return header
}

/**
 * Build the standard 401 Unauthorized response with WWW-Authenticate challenge.
 */
export function unauthorizedResponse(resourceUrl: string, errorCode?: string): Response {
  return new Response(JSON.stringify({ error: errorCode ?? 'unauthorized' }), {
    status: 401,
    headers: {
      'Content-Type': 'application/json',
      'WWW-Authenticate': buildWwwAuthenticate(resourceUrl, errorCode),
    },
  })
}
