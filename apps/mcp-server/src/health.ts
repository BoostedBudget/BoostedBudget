/**
 * Health check handler — no auth, no deps, no side-effects.
 * Used by load-balancers and smoke tests.
 */
export function handleHealth(): Response {
  return new Response(JSON.stringify({ status: 'ok', service: 'mcp-server' }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
