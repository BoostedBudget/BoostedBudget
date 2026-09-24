/**
 * useDocumentHead tests (Issue #1666, Wave 3 item 9).
 *
 * Pins the save/restore semantics the hook was extracted from McpPage with:
 * on mount it sets document.title, a meta[name=description], and a
 * link[rel=canonical]; on unmount it restores whatever was there before and
 * REMOVES any tag it created itself. McpPage.test.tsx asserts the same
 * observable behaviour against the page (which is why extraction was safe);
 * these tests pin it against the hook directly, including the two branches
 * McpPage can't exercise in one process: a pre-existing tag restored to its
 * prior value, and a hook-created tag removed.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { useDocumentHead } from './useDocumentHead'

const CANONICAL = 'https://boostedbudget.com/pricing'

function clearHead(): void {
  document.title = ''
  document.querySelectorAll('meta[name="description"]').forEach((tag) => tag.remove())
  document.querySelectorAll('link[rel="canonical"]').forEach((tag) => tag.remove())
}

function appendMeta(content: string): void {
  const meta = document.createElement('meta')
  meta.setAttribute('name', 'description')
  meta.setAttribute('content', content)
  document.head.appendChild(meta)
}

function appendCanonical(href: string): void {
  const link = document.createElement('link')
  link.setAttribute('rel', 'canonical')
  link.setAttribute('href', href)
  document.head.appendChild(link)
}

describe('useDocumentHead', () => {
  beforeEach(clearHead)
  afterEach(clearHead)

  it('sets title, meta description, and canonical link on mount', () => {
    renderHook(() => useDocumentHead('Pricing — BoostedBudget', 'Pricing description', CANONICAL))

    expect(document.title).toBe('Pricing — BoostedBudget')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'Pricing description')
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', CANONICAL)
  })

  it('restores a pre-existing title, meta description, and canonical link on unmount', () => {
    document.title = 'Original Title'
    appendMeta('Original description')
    appendCanonical('https://boostedbudget.com/')

    const { unmount } = renderHook(() => useDocumentHead('Pricing — BoostedBudget', 'Pricing description', CANONICAL))
    unmount()

    expect(document.title).toBe('Original Title')
    expect(document.querySelectorAll('meta[name="description"]')).toHaveLength(1)
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'Original description')
    expect(document.querySelectorAll('link[rel="canonical"]')).toHaveLength(1)
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', 'https://boostedbudget.com/')
  })

  it('removes the meta description and canonical link it created itself on unmount', () => {
    const { unmount } = renderHook(() => useDocumentHead('Features — BoostedBudget', 'Features description', CANONICAL))

    expect(document.querySelector('meta[name="description"]')).not.toBeNull()
    expect(document.querySelector('link[rel="canonical"]')).not.toBeNull()

    unmount()

    expect(document.querySelectorAll('meta[name="description"]')).toHaveLength(0)
    expect(document.querySelectorAll('link[rel="canonical"]')).toHaveLength(0)
  })

  it('removes only the tag it created when the other pre-existed', () => {
    appendMeta('Original description')

    const { unmount } = renderHook(() => useDocumentHead('FAQ — BoostedBudget', 'FAQ description', CANONICAL))
    unmount()

    expect(document.querySelectorAll('meta[name="description"]')).toHaveLength(1)
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'Original description')
    expect(document.querySelectorAll('link[rel="canonical"]')).toHaveLength(0)
  })

  it('updates the values when the arguments change, restoring on final unmount', () => {
    document.title = 'Original Title'
    appendMeta('Original description')
    appendCanonical('https://boostedbudget.com/')

    const { rerender, unmount } = renderHook(
      ({ title, description, canonical }) => useDocumentHead(title, description, canonical),
      { initialProps: { title: 'One', description: 'one', canonical: 'https://boostedbudget.com/one' } }
    )

    rerender({ title: 'Two', description: 'two', canonical: 'https://boostedbudget.com/two' })

    expect(document.title).toBe('Two')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'two')
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', 'https://boostedbudget.com/two')

    unmount()

    expect(document.title).toBe('Original Title')
    expect(document.querySelector('meta[name="description"]')).toHaveAttribute('content', 'Original description')
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute('href', 'https://boostedbudget.com/')
  })
})