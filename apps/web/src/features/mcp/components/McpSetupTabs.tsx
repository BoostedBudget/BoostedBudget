/**
 * McpSetupTabs — the four client setup tabs in §4 "Connect your client" (Issue #1411).
 *
 * Reuses @/components/ui/tabs (Radix) rather than hand-rolling a switcher — it's
 * already in the vendor chunk with six existing consumers, so this costs zero
 * new bytes. The server URL is resolved via getMcpBaseUrl()/getMcpEndpoint()
 * (never hardcoded); when that resolves to `null` (development or preview
 * environments — by design, see @/shared/lib/mcpUrl), every tab renders a "not configured"
 * notice instead of a broken URL.
 *
 * Tab selection persists to localStorage and reflects in the URL hash
 * (#connect-<slug>) so support can link a user straight to their client.
 */

import { useEffect, useRef, useState } from 'react'
import { Copy } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { useCopyToClipboard } from '@/shared/hooks/useCopyToClipboard'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { getMcpEndpoint } from '@/shared/lib/mcpUrl'
import { cn } from '@/lib/utils'

export type McpClientId = 'claude_desktop' | 'claude_code' | 'cursor' | 'vscode'

interface ClientMeta {
  id: McpClientId
  label: string
  /** URL hash suffix, e.g. "cursor" → "#connect-cursor". */
  hashSlug: string
}

const CLIENTS: ClientMeta[] = [
  { id: 'claude_desktop', label: 'Claude Desktop', hashSlug: 'claude-desktop' },
  { id: 'claude_code', label: 'Claude Code CLI', hashSlug: 'claude-code' },
  { id: 'cursor', label: 'Cursor & other clients', hashSlug: 'cursor' },
  { id: 'vscode', label: 'VS Code', hashSlug: 'vscode' }
]

const STORAGE_KEY = 'mcp-setup-client'
const DEFAULT_CLIENT: McpClientId = 'claude_desktop'

function clientFromHashSlug(slug: string): McpClientId | null {
  const match = CLIENTS.find((c) => c.hashSlug === slug)
  return match ? match.id : null
}

function hashSlugFromClient(client: McpClientId): string {
  return CLIENTS.find((c) => c.id === client)?.hashSlug ?? client
}

function readInitialClient(): McpClientId {
  const hash = window.location.hash.replace(/^#/, '')
  const hashMatch = hash.startsWith('connect-') ? clientFromHashSlug(hash.slice('connect-'.length)) : null
  if (hashMatch) return hashMatch

  const stored = window.localStorage.getItem(STORAGE_KEY)
  if (stored && CLIENTS.some((c) => c.id === stored)) return stored as McpClientId

  return DEFAULT_CLIENT
}

function NotConfiguredNotice() {
  return (
    <p
      role="status"
      className="rounded-md border border-warning/40 bg-warning/10 px-4 py-3 text-sm text-warning-foreground"
    >
      The MCP server address is not configured in this environment (for example, local development or a preview
      environment). The exact URL and commands below will appear once it is — there is nothing to type in the meantime.
    </p>
  )
}

function CodeBlock({ label, content }: { label: string; content: string }) {
  return (
    <pre
      tabIndex={0}
      role="region"
      aria-label={label}
      className="overflow-x-auto rounded-md border border-border bg-muted px-4 py-3 text-sm text-foreground"
    >
      <code>{content}</code>
    </pre>
  )
}

interface CopyButtonProps {
  label: string
  textToCopy: string
  onCopy: () => void
}

function CopyButton({ label, textToCopy, onCopy }: CopyButtonProps) {
  const { copy, copied } = useCopyToClipboard()
  // Fires onCopy() only on the transition to `copied === true` — i.e. after the clipboard
  // write actually succeeds — not on click. useCopyToClipboard() never rejects (it swallows
  // clipboard errors into its own `error` state), so calling onCopy() synchronously in the
  // click handler would announce "copied" and fire MCP_CONFIG_COPIED even on failure.
  const wasCopied = useRef(false)

  useEffect(() => {
    if (copied && !wasCopied.current) {
      onCopy()
    }
    wasCopied.current = copied
  }, [copied, onCopy])

  return (
    <button
      type="button"
      aria-label={label}
      onClick={() => void copy(textToCopy)}
      className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-sm font-medium text-foreground transition-colors hover:bg-muted"
    >
      <Copy className="h-3.5 w-3.5" aria-hidden="true" />
      Copy
    </button>
  )
}

interface CodeSectionProps {
  value: string | null
  blockLabel: string
  copyLabel: string
  client: McpClientId
  onCopy: (client: McpClientId) => void
  isInline?: boolean
}

function CodeSection({ value, blockLabel, copyLabel, client, onCopy, isInline }: CodeSectionProps) {
  if (!value) {
    return (
      <div className="mt-2">
        <NotConfiguredNotice />
      </div>
    )
  }

  return (
    <div className={isInline ? 'mt-2 flex flex-wrap items-center gap-2' : 'mt-2 space-y-2'}>
      {isInline ? (
        <code
          tabIndex={0}
          role="region"
          aria-label={blockLabel}
          className="overflow-x-auto rounded-md border border-border bg-muted px-3 py-1.5 text-sm text-foreground"
        >
          {value}
        </code>
      ) : (
        <CodeBlock label={blockLabel} content={value} />
      )}
      <CopyButton label={copyLabel} textToCopy={value} onCopy={() => onCopy(client)} />
    </div>
  )
}

function Steps({ children }: { children: React.ReactNode }) {
  return <ol className="list-decimal space-y-3 pl-5 text-sm text-foreground">{children}</ol>
}

function WorksWhen({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-border bg-card px-4 py-3 text-sm text-foreground">
      <span className="font-medium text-foreground">You&apos;ll know it worked when: </span>
      {children}
    </p>
  )
}

interface TabPanelProps {
  serverUrl: string | null
  onCopy: (client: McpClientId) => void
}

function ClaudeDesktopPanel({ serverUrl, onCopy }: TabPanelProps) {
  return (
    <div className="space-y-4">
      <Steps>
        <li>In Claude Desktop, open Settings → Connectors.</li>
        <li>Click Add, then Add custom connector.</li>
        <li>
          Paste the server URL below, then click Add. Leave Advanced settings empty — the server supports Dynamic Client
          Registration, so Claude registers itself automatically.
          <CodeSection
            value={serverUrl}
            blockLabel="Claude Desktop server URL"
            copyLabel="Copy Claude Desktop server URL"
            client="claude_desktop"
            onCopy={onCopy}
            isInline
          />
        </li>
        <li>A browser window opens for you to sign in with your BoostedBudget account and authorize access.</li>
      </Steps>
      <WorksWhen>
        <code>boostedbudget</code> appears under Connectors with <code>search</code>, <code>readExecute</code>, and{' '}
        <code>writeExecute</code> listed.
      </WorksWhen>
    </div>
  )
}

function ClaudeCodePanel({ serverUrl, onCopy }: TabPanelProps) {
  const command = serverUrl ? `claude mcp add --transport http boostedbudget ${serverUrl}` : null

  return (
    <div className="space-y-4">
      <Steps>
        <li>
          Run:
          <CodeSection
            value={command}
            blockLabel="Claude Code command"
            copyLabel="Copy Claude Code command"
            client="claude_code"
            onCopy={onCopy}
          />
          Add <code>--scope user</code> instead of the default to make it available across all your projects, not just
          the current one.
        </li>
        <li>
          Inside Claude Code, run <code>/mcp</code>, select <code>boostedbudget</code>, then choose Authenticate. A
          browser window opens for you to sign in with your BoostedBudget account.
        </li>
      </Steps>
      <WorksWhen>
        <code>claude mcp list</code> shows <code>✔ Connected</code> for boostedbudget. Before you authenticate, it shows{' '}
        <code>! Needs authentication</code> — that&apos;s expected, not an error.
      </WorksWhen>
    </div>
  )
}

function OtherClientsPanel({ serverUrl, onCopy }: TabPanelProps) {
  return (
    <div className="space-y-4">
      <Steps>
        <li>
          This server speaks the Model Context Protocol over Streamable HTTP at:
          <CodeSection
            value={serverUrl}
            blockLabel="MCP server URL"
            copyLabel="Copy MCP server URL"
            client="cursor"
            onCopy={onCopy}
            isInline
          />
        </li>
        <li>
          Add it as a remote MCP server, following your client&apos;s own documentation — setup steps vary by client,
          but the URL above is the one thing every client needs.
        </li>
        <li>
          The server handles authentication itself: the first time your agent calls a boostedbudget tool, a browser
          window should open for you to sign in with your BoostedBudget account and authorize access.
        </li>
      </Steps>
      <WorksWhen>your client lists boostedbudget as connected, and a sign-in window has completed.</WorksWhen>
    </div>
  )
}

function VsCodePanel({ serverUrl, onCopy }: TabPanelProps) {
  const json = serverUrl ? `{"servers":{"boostedbudget":{"type":"http","url":"${serverUrl}"}}}` : null

  return (
    <div className="space-y-6">
      <div className="space-y-4">
        <p className="text-sm text-foreground">If you use GitHub Copilot Chat&apos;s built-in MCP support:</p>
        <Steps>
          <li>
            Create <code>.vscode/mcp.json</code> in your project.
          </li>
          <li>
            Add the server. Note the top-level key is <code>servers</code>, not <code>mcpServers</code>.
            <CodeSection
              value={json}
              blockLabel="VS Code configuration"
              copyLabel="Copy VS Code configuration"
              client="vscode"
              onCopy={onCopy}
            />
          </li>
          <li>
            VS Code prompts to start the server — approve it. A browser window opens for you to sign in with your
            BoostedBudget account.
          </li>
        </Steps>
        <WorksWhen>the MCP output panel shows boostedbudget as Running.</WorksWhen>
      </div>

      <p className="text-sm text-muted-foreground">
        If you use the Claude Code extension for VS Code instead: it does not read .vscode/mcp.json. Use the Claude Code
        CLI command from the previous tab in the integrated terminal, then run <code>/mcp</code> inside the
        extension&apos;s chat.
      </p>
    </div>
  )
}

export function McpSetupTabs() {
  const trackEvent = useTrackEvent()
  const [activeClient, setActiveClient] = useState<McpClientId>(() => readInitialClient())
  const serverUrl = getMcpEndpoint()
  const [copyAnnouncement, setCopyAnnouncement] = useState('')

  useEffect(() => {
    if (copyAnnouncement === '') return
    const timer = setTimeout(() => setCopyAnnouncement(''), 2000)
    return () => clearTimeout(timer)
  }, [copyAnnouncement])

  const handleTabChange = (value: string) => {
    const client = value as McpClientId
    setActiveClient(client)
    window.localStorage.setItem(STORAGE_KEY, client)
    window.history.replaceState(
      null,
      '',
      `${window.location.pathname}${window.location.search}#connect-${hashSlugFromClient(client)}`
    )
    trackEvent(ANALYTICS_EVENTS.MCP_SETUP_TAB_CHANGED, { client })
  }

  const handleCopy = (client: McpClientId) => {
    trackEvent(ANALYTICS_EVENTS.MCP_CONFIG_COPIED, { client })
    const label = CLIENTS.find((c) => c.id === client)?.label ?? client
    setCopyAnnouncement(`${label} configuration copied to clipboard.`)
  }

  return (
    <div>
      <Tabs value={activeClient} onValueChange={handleTabChange}>
        <TabsList aria-label="MCP client" className={cn('flex-wrap')}>
          {CLIENTS.map((client) => (
            <TabsTrigger key={client.id} value={client.id}>
              {client.label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="claude_desktop">
          <ClaudeDesktopPanel serverUrl={serverUrl} onCopy={handleCopy} />
        </TabsContent>
        <TabsContent value="claude_code">
          <ClaudeCodePanel serverUrl={serverUrl} onCopy={handleCopy} />
        </TabsContent>
        <TabsContent value="cursor">
          <OtherClientsPanel serverUrl={serverUrl} onCopy={handleCopy} />
        </TabsContent>
        <TabsContent value="vscode">
          <VsCodePanel serverUrl={serverUrl} onCopy={handleCopy} />
        </TabsContent>
      </Tabs>

      <span role="status" aria-live="polite" className="sr-only">
        {copyAnnouncement}
      </span>
    </div>
  )
}
