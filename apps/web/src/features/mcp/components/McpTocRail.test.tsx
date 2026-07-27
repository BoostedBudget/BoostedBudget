/**
 * McpTocRail Component Tests (Issue #1411)
 *
 * Per task-P8-brief.md § "Accessibility": `<nav aria-label="On this page">` with
 * a `<ul>` of real anchors, active item tracked via IntersectionObserver (not
 * scroll math), and collapses to a `<details>` "On this page" on mobile rather
 * than disappearing.
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, within, act } from '@testing-library/react'
import { McpTocRail } from './McpTocRail'

const sections = [
  { id: 'overview', label: 'Overview' },
  { id: 'capabilities', label: 'Capabilities' },
  { id: 'connect', label: 'Connect' },
]

type ObserverEntry = { isIntersecting: boolean; target: Element; boundingClientRect: { top: number } }

class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []
  callback: (entries: ObserverEntry[]) => void
  observed: Element[] = []
  constructor(callback: (entries: ObserverEntry[]) => void) {
    this.callback = callback
    FakeIntersectionObserver.instances.push(this)
  }
  observe(el: Element) {
    this.observed.push(el)
  }
  unobserve = mock(() => {})
  disconnect = mock(() => {})
  takeRecords() {
    return []
  }
}

describe('McpTocRail', () => {
  let originalIntersectionObserver: typeof IntersectionObserver

  beforeEach(() => {
    FakeIntersectionObserver.instances = []
    originalIntersectionObserver = window.IntersectionObserver
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    window.IntersectionObserver = FakeIntersectionObserver as any
  })

  afterEach(() => {
    window.IntersectionObserver = originalIntersectionObserver
    document.body.innerHTML = ''
  })

  function renderWithSectionTargets() {
    for (const section of sections) {
      const el = document.createElement('h2')
      el.id = section.id
      document.body.appendChild(el)
    }
    return render(<McpTocRail sections={sections} />)
  }

  it('renders exactly one nav[aria-label="On this page"]', () => {
    renderWithSectionTargets()
    const navs = screen.getAllByRole('navigation', { name: 'On this page' })
    expect(navs).toHaveLength(1)
  })

  it('renders one anchor per section, in order, with href matching the section id', () => {
    renderWithSectionTargets()
    const nav = screen.getByRole('navigation', { name: 'On this page' })
    const links = within(nav).getAllByRole('link')
    expect(links).toHaveLength(sections.length)
    links.forEach((link, i) => {
      expect(link).toHaveAttribute('href', `#${sections[i].id}`)
      expect(link).toHaveTextContent(sections[i].label)
    })
  })

  it('renders a mobile "On this page" details disclosure with the same links', () => {
    renderWithSectionTargets()
    expect(screen.getByText('On this page')).toBeInTheDocument()
    const details = document.querySelector('details')
    expect(details).not.toBeNull()
    const detailLinks = within(details as HTMLElement).getAllByRole('link')
    expect(detailLinks).toHaveLength(sections.length)
  })

  it('observes every section target element via IntersectionObserver', () => {
    renderWithSectionTargets()
    const instance = FakeIntersectionObserver.instances[0]
    expect(instance).toBeDefined()
    expect(instance.observed).toHaveLength(sections.length)
  })

  it('marks the intersecting section as the active TOC item (aria-current)', () => {
    renderWithSectionTargets()
    const instance = FakeIntersectionObserver.instances[0]
    const capabilitiesEl = document.getElementById('capabilities') as Element

    act(() => {
      instance.callback([
        { isIntersecting: true, target: capabilitiesEl, boundingClientRect: { top: 10 } },
      ])
    })

    const nav = screen.getByRole('navigation', { name: 'On this page' })
    const activeLink = within(nav).getByRole('link', { name: 'Capabilities' })
    expect(activeLink).toHaveAttribute('aria-current', 'true')

    const overviewLink = within(nav).getByRole('link', { name: 'Overview' })
    expect(overviewLink).not.toHaveAttribute('aria-current')
  })

  it('disconnects the observer on unmount', () => {
    const { unmount } = renderWithSectionTargets()
    const instance = FakeIntersectionObserver.instances[0]
    unmount()
    expect(instance.disconnect).toHaveBeenCalled()
  })

  it('still renders the rail, without active-section highlighting, when IntersectionObserver is unsupported', () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    window.IntersectionObserver = undefined as any

    expect(() => renderWithSectionTargets()).not.toThrow()
    expect(FakeIntersectionObserver.instances).toHaveLength(0)

    const nav = screen.getByRole('navigation', { name: 'On this page' })
    const links = within(nav).getAllByRole('link')
    expect(links).toHaveLength(sections.length)
    links.forEach((link) => {
      expect(link).not.toHaveAttribute('aria-current')
    })
  })
})
