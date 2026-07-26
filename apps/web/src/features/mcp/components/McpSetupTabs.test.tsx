/**
 * McpSetupTabs Component Tests (Issue #1411)
 *
 * Per task-P8-brief.md §4 "client setup tabs": four tabs (Claude Desktop, Claude
 * Code CLI, Cursor, VS Code), each with numbered steps, a code block, a sign-in
 * step, and a "you'll know it worked when" line. The server URL is resolved via
 * getMcpBaseUrl()/getMcpEndpoint() (never hardcoded) — when that resolves to
 * `null`, a clear "not configured" state renders instead of a broken URL. Tab
 * state persists to localStorage and reflects in the URL hash.
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpSetupTabs } from './McpSetupTabs'

// import.meta.env fields are declared readonly for app code; tests mutate via a
// locally-typed non-readonly view, mirroring src/shared/lib/mcpUrl.test.ts.
interface MutableViteEnv {
  VITE_MCP_URL?: string
  VITE_ENVIRONMENT?: string
}
function mutableEnv(): MutableViteEnv {
  return import.meta.env as unknown as MutableViteEnv
}

const ORIGINAL_VITE_MCP_URL = import.meta.env.VITE_MCP_URL
const ORIGINAL_VITE_ENVIRONMENT = import.meta.env.VITE_ENVIRONMENT
const STORAGE_KEY = 'mcp-setup-client'

describe('McpSetupTabs', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    delete mutableEnv().VITE_MCP_URL
    delete mutableEnv().VITE_ENVIRONMENT
    window.localStorage.clear()
    window.history.replaceState(null, '', '/mcp')

    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mock(() => Promise.resolve(undefined)) },
      configurable: true,
      writable: true,
    })
  })

  afterEach(() => {
    if (ORIGINAL_VITE_MCP_URL === undefined) delete mutableEnv().VITE_MCP_URL
    else mutableEnv().VITE_MCP_URL = ORIGINAL_VITE_MCP_URL
    if (ORIGINAL_VITE_ENVIRONMENT === undefined) delete mutableEnv().VITE_ENVIRONMENT
    else mutableEnv().VITE_ENVIRONMENT = ORIGINAL_VITE_ENVIRONMENT
    window.localStorage.clear()
    window.history.replaceState(null, '', '/mcp')
  })

  it('renders a TabsList labeled "MCP client" with all four clients', () => {
    render(<McpSetupTabs />)
    const tabList = screen.getByRole('tablist', { name: 'MCP client' })
    expect(within(tabList).getByRole('tab', { name: 'Claude Desktop' })).toBeInTheDocument()
    expect(within(tabList).getByRole('tab', { name: 'Claude Code CLI' })).toBeInTheDocument()
    expect(within(tabList).getByRole('tab', { name: 'Cursor' })).toBeInTheDocument()
    expect(within(tabList).getByRole('tab', { name: 'VS Code' })).toBeInTheDocument()
  })

  it('defaults to the Claude Desktop tab when there is no hash or saved preference', () => {
    render(<McpSetupTabs />)
    expect(screen.getByRole('tab', { name: 'Claude Desktop' })).toHaveAttribute('data-state', 'active')
  })

  describe('when the MCP server URL is not configured in this environment', () => {
    it('renders a "not configured" state instead of a broken URL', () => {
      render(<McpSetupTabs />)
      expect(screen.getByText(/not configured in this environment/i)).toBeInTheDocument()
      // No literal server URL should be interpolated anywhere on the page.
      expect(document.body.textContent).not.toMatch(/https?:\/\//)
    })

    it('shows the "not configured" state on every tab, not just the default one', async () => {
      const user = userEvent.setup()
      render(<McpSetupTabs />)
      await user.click(screen.getByRole('tab', { name: 'Cursor' }))
      expect(screen.getByText(/not configured in this environment/i)).toBeInTheDocument()
    })
  })

  describe('when the MCP server URL is configured', () => {
    beforeEach(() => {
      mutableEnv().VITE_MCP_URL = 'https://mcp.test.example.com'
    })

    it('interpolates the resolved endpoint into the Claude Code CLI command', async () => {
      const user = userEvent.setup()
      render(<McpSetupTabs />)
      await user.click(screen.getByRole('tab', { name: 'Claude Code CLI' }))
      expect(
        screen.getByText('claude mcp add --transport http boostedbudget https://mcp.test.example.com/mcp')
      ).toBeInTheDocument()
    })

    it('interpolates the resolved endpoint into the Cursor JSON config with no "type" key', async () => {
      const user = userEvent.setup()
      render(<McpSetupTabs />)
      await user.click(screen.getByRole('tab', { name: 'Cursor' }))
      const json = screen.getByText(/"mcpServers"/)
      expect(json.textContent).toContain('"url":"https://mcp.test.example.com/mcp"')
      expect(json.textContent).not.toContain('"type"')
    })

    it('interpolates the resolved endpoint into the VS Code config under the "servers" key (not "mcpServers")', async () => {
      const user = userEvent.setup()
      render(<McpSetupTabs />)
      await user.click(screen.getByRole('tab', { name: 'VS Code' }))
      const json = screen.getByText(/"servers"/)
      expect(json.textContent).toContain('"url":"https://mcp.test.example.com/mcp"')
      expect(json.textContent).not.toContain('"mcpServers"')
    })

    it('does not render the "not configured" notice', () => {
      render(<McpSetupTabs />)
      expect(screen.queryByText(/not configured in this environment/i)).not.toBeInTheDocument()
    })
  })

  it('fires MCP_SETUP_TAB_CHANGED with the client discriminator when switching tabs', async () => {
    const user = userEvent.setup()
    render(<McpSetupTabs />)

    await user.click(screen.getByRole('tab', { name: 'Cursor' }))

    expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.MCP_SETUP_TAB_CHANGED, {
      client: 'cursor',
    })
  })

  it('does not fire MCP_SETUP_TAB_CHANGED merely from the initial render', () => {
    render(<McpSetupTabs />)
    expect(mockTrackEvent).not.toHaveBeenCalledWith(
      ANALYTICS_EVENTS.MCP_SETUP_TAB_CHANGED,
      expect.anything()
    )
  })

  it('swaps panel content when switching tabs', async () => {
    const user = userEvent.setup()
    mutableEnv().VITE_MCP_URL = 'https://mcp.test.example.com'
    render(<McpSetupTabs />)
    expect(screen.getByText(/Settings → Connectors/)).toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: 'Claude Code CLI' }))
    expect(screen.queryByText(/Settings → Connectors/)).not.toBeInTheDocument()
    expect(screen.getByText(/claude mcp add/)).toBeInTheDocument()
  })

  it('persists the active tab to localStorage and restores it on remount', async () => {
    const user = userEvent.setup()
    const { unmount } = render(<McpSetupTabs />)
    await user.click(screen.getByRole('tab', { name: 'Cursor' }))
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('cursor')
    unmount()

    render(<McpSetupTabs />)
    expect(screen.getByRole('tab', { name: 'Cursor' })).toHaveAttribute('data-state', 'active')
  })

  it('reflects the active tab in the URL hash after switching', async () => {
    const user = userEvent.setup()
    render(<McpSetupTabs />)
    await user.click(screen.getByRole('tab', { name: 'Cursor' }))
    expect(window.location.hash).toBe('#connect-cursor')
  })

  it('honors an existing URL hash on initial mount', () => {
    window.history.replaceState(null, '', '/mcp#connect-cursor')
    render(<McpSetupTabs />)
    expect(screen.getByRole('tab', { name: 'Cursor' })).toHaveAttribute('data-state', 'active')
  })

  describe('copy buttons', () => {
    it('has an explicit aria-label per client and fires MCP_CONFIG_COPIED once the clipboard write succeeds', async () => {
      const user = userEvent.setup()
      mutableEnv().VITE_MCP_URL = 'https://mcp.test.example.com'
      render(<McpSetupTabs />)

      await user.click(screen.getByRole('tab', { name: 'Cursor' }))
      const copyButton = screen.getByRole('button', { name: /copy cursor configuration/i })
      await user.click(copyButton)

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.MCP_CONFIG_COPIED, {
          client: 'cursor',
        })
      })
    })

    // Codex review finding #7: CopyButton previously fired MCP_CONFIG_COPIED and the aria-live
    // announcement synchronously on click, before useCopyToClipboard's async write resolved —
    // so a failed clipboard write was still announced and tracked as a successful copy.
    it('does NOT fire MCP_CONFIG_COPIED when the clipboard write fails', async () => {
      const user = userEvent.setup()
      mutableEnv().VITE_MCP_URL = 'https://mcp.test.example.com'
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: mock(() => Promise.reject(new Error('denied'))) },
        configurable: true,
        writable: true,
      })
      render(<McpSetupTabs />)

      await user.click(screen.getByRole('tab', { name: 'Cursor' }))
      const copyButton = screen.getByRole('button', { name: /copy cursor configuration/i })
      await user.click(copyButton)

      // Give the rejected promise a tick to settle before asserting the negative.
      await waitFor(() => {
        expect(navigator.clipboard.writeText).toHaveBeenCalled()
      })
      expect(mockTrackEvent).not.toHaveBeenCalledWith(
        ANALYTICS_EVENTS.MCP_CONFIG_COPIED,
        expect.anything()
      )
    })

    it('exposes an aria-live confirmation region', () => {
      render(<McpSetupTabs />)
      const liveRegion = document.querySelector('[aria-live="polite"]')
      expect(liveRegion).not.toBeNull()
    })
  })

  describe('accessibility of code blocks', () => {
    it('makes horizontally-scrollable <pre> blocks focusable regions with a label', async () => {
      const user = userEvent.setup()
      mutableEnv().VITE_MCP_URL = 'https://mcp.test.example.com'
      render(<McpSetupTabs />)
      await user.click(screen.getByRole('tab', { name: 'Claude Code CLI' }))

      const region = screen.getByRole('region', { name: /claude code command/i })
      expect(region).toHaveAttribute('tabIndex', '0')
    })
  })

  it('includes a visible caveat that Cursor DCR support is unverified (#1421)', async () => {
    const user = userEvent.setup()
    render(<McpSetupTabs />)
    await user.click(screen.getByRole('tab', { name: 'Cursor' }))
    expect(screen.getByText(/unverified/i)).toBeInTheDocument()
  })

  it('tells VS Code Claude Code extension users that it does not read .vscode/mcp.json', async () => {
    const user = userEvent.setup()
    render(<McpSetupTabs />)
    await user.click(screen.getByRole('tab', { name: 'VS Code' }))
    expect(screen.getByText(/does not read \.vscode\/mcp\.json/i)).toBeInTheDocument()
  })
})
