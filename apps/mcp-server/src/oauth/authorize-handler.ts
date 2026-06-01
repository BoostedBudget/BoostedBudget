import type { OAuthHelpers } from '@cloudflare/workers-oauth-provider'
import type { Env } from '../env'

/** Scopes requested from Clerk. The Clerk OAuth Application MUST have each of these
 *  enabled (dashboard → OAuth Applications → Scopes), or Clerk redirects back to
 *  /oauth/callback with ?error=invalid_scope. `openid` yields the id_token we decode
 *  for the user's `sub`. */
const CLERK_OAUTH_SCOPES = 'openid email profile'

/** Standard OAuth 2.0 authorization error codes (RFC 6749 §4.1.2.1). Only these are
 *  reflected to the user; anything else is shown as a generic message. This prevents
 *  reflected XSS from an attacker-crafted /oauth/callback?error=... URL. */
const KNOWN_OAUTH_ERRORS = new Set([
  'invalid_request',
  'unauthorized_client',
  'access_denied',
  'unsupported_response_type',
  'invalid_scope',
  'server_error',
  'temporarily_unavailable',
])

/** Escape HTML special chars — defense-in-depth for any interpolated value. */
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/** Render a user-facing HTML error page (shown in the browser tab opened for OAuth). */
function errorPage(message: string, status: number): Response {
  return new Response(
    `<!doctype html><html><head><title>Authorization failed</title></head><body style="font-family:sans-serif;max-width:480px;margin:80px auto;padding:0 24px">
<h2>Authorization failed</h2>
<p>${escapeHtml(message)}</p>
<p style="color:#888;font-size:13px">Please close this window and try again. If this keeps happening, contact support.</p>
</body></html>`,
    { status, headers: { 'Content-Type': 'text/html; charset=utf-8' } },
  )
}

/**
 * /oauth/authorize — redirect the user to Clerk for authentication.
 *
 * The Worker is the OAuth AS (via workers-oauth-provider). It delegates user
 * authentication to Clerk. We store the original auth request in OAUTH_KV so we
 * can complete the authorization when Clerk redirects back to /oauth/callback.
 *
 * State key is a random UUID used as the `state` parameter sent to Clerk.
 * The key expires in 15 minutes (more than enough for a login flow).
 */
export async function handleAuthorize(
  request: Request,
  env: Env,
  oauthHelpers: OAuthHelpers,
): Promise<Response> {
  let authRequest
  try {
    authRequest = await oauthHelpers.parseAuthRequest(request)
  } catch {
    return new Response(JSON.stringify({ error: 'invalid_request' }), {
      status: 400,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  const stateKey = crypto.randomUUID()
  await env.OAUTH_KV.put(`oauth_state:${stateKey}`, JSON.stringify(authRequest), {
    expirationTtl: 900,
  })

  const clerkUrl = new URL(`${env.CLERK_ISSUER_URL}/oauth/authorize`)
  clerkUrl.searchParams.set('response_type', 'code')
  clerkUrl.searchParams.set('client_id', env.MCP_OAUTH_AUDIENCE)
  clerkUrl.searchParams.set('redirect_uri', `${env.MCP_RESOURCE_URL}/oauth/callback`)
  clerkUrl.searchParams.set('scope', CLERK_OAUTH_SCOPES)
  clerkUrl.searchParams.set('state', stateKey)

  return Response.redirect(clerkUrl.toString(), 302)
}

/**
 * /oauth/callback — receive Clerk's authorization code, exchange for a token,
 * then complete the OAuth authorization so workers-oauth-provider issues its own
 * access token to the MCP client.
 *
 * props stored with the grant:
 *   clerkUserId   — used as the actor for backend API calls and audit logs
 *   accessToken   — Clerk access token forwarded as Authorization to the Go backend
 */
export async function handleCallback(
  request: Request,
  env: Env,
  oauthHelpers: OAuthHelpers,
): Promise<Response> {
  const url = new URL(request.url)
  const code = url.searchParams.get('code')
  const stateKey = url.searchParams.get('state')

  // Surface upstream (Clerk) authorization errors instead of masking them as
  // "missing code or state". Clerk redirects here with ?error=... on failure
  // (e.g. invalid_scope, access_denied).
  const upstreamError = url.searchParams.get('error')
  if (upstreamError) {
    const desc = url.searchParams.get('error_description') ?? ''
    console.error('[oauth-callback] Clerk authorization error:', upstreamError, desc)
    // Only reflect a whitelisted error code; never interpolate raw query input.
    const safeCode = KNOWN_OAUTH_ERRORS.has(upstreamError) ? ` (${upstreamError})` : ''
    return errorPage(`The identity provider declined the sign-in${safeCode}.`, 502)
  }

  if (!code || !stateKey) {
    return new Response(JSON.stringify({ error: 'invalid_callback', detail: 'missing code or state' }), {
      status: 400,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  const authRequestJson = await env.OAUTH_KV.get(`oauth_state:${stateKey}`)
  if (!authRequestJson) {
    return new Response(JSON.stringify({ error: 'invalid_state', detail: 'state expired or unknown' }), {
      status: 400,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  // One-time use — delete immediately after retrieval
  await env.OAUTH_KV.delete(`oauth_state:${stateKey}`)

  const authRequest = JSON.parse(authRequestJson)

  // Exchange the Clerk authorization code for an access token.
  // CLERK_OAUTH_CLIENT_SECRET is the client_secret from the Clerk OAuth Application
  // (Clerk dashboard → Configure → OAuth Applications → your app's secret).
  const tokenBody = new URLSearchParams({
    grant_type: 'authorization_code',
    code,
    client_id: env.MCP_OAUTH_AUDIENCE,
    client_secret: env.CLERK_OAUTH_CLIENT_SECRET,
    redirect_uri: `${env.MCP_RESOURCE_URL}/oauth/callback`,
  })

  const tokenRes = await fetch(`${env.CLERK_ISSUER_URL}/oauth/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: tokenBody,
  })

  if (!tokenRes.ok) {
    const body = await tokenRes.text()
    console.error('[oauth-callback] Clerk token exchange failed:', tokenRes.status, body)
    return errorPage('Could not complete sign-in with your BoostedBudget account.', 502)
  }

  const tokens = (await tokenRes.json()) as {
    access_token: string
    id_token?: string
  }

  // Extract the Clerk user ID from the id_token (JWT, no verification needed here —
  // we just fetched it directly from Clerk over TLS; the full token is verified by
  // the Go backend on each API call).
  const jwtPayload = tokens.id_token ?? tokens.access_token
  let clerkUserId: string
  try {
    const b64 = jwtPayload.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const payload = JSON.parse(atob(b64)) as { sub?: string }
    clerkUserId = payload.sub ?? ''
  } catch {
    clerkUserId = ''
  }

  if (!clerkUserId) {
    console.error('[oauth-callback] No sub claim in Clerk token')
    return new Response(JSON.stringify({ error: 'upstream_error', detail: 'cannot extract user id' }), {
      status: 502,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  // Complete the authorization — workers-oauth-provider issues its own access token
  // and redirects the MCP client to its registered callback with the code.
  const { redirectTo } = await oauthHelpers.completeAuthorization({
    request: authRequest,
    userId: clerkUserId,
    metadata: { clerkUserId },
    scope: authRequest.scope,
    props: {
      clerkUserId,
      accessToken: tokens.access_token,
    },
  })

  return Response.redirect(redirectTo, 302)
}
