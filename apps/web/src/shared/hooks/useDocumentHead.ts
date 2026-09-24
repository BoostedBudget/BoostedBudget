/**
 * useDocumentHead — sets the per-page <head> for a route and restores it on unmount.
 *
 * Extracted from McpPage (Issue #1666, Wave 3 item 9) so every public page uses
 * the one proven implementation instead of a local copy. Sets document.title, a
 * `meta[name="description"]`, and a `link[rel="canonical"]`; on unmount restores
 * each previous value and REMOVES any tag this hook created (tracked per-tag via
 * whether it pre-existed), so a route cannot leave a stale canonical behind.
 *
 * Best-effort SEO: a crawler that doesn't execute JavaScript still sees
 * index.html's static "/" canonical — a real fix needs prerendering (#1423).
 */

import { useLayoutEffect } from 'react'

export function useDocumentHead(title: string, description: string, canonicalHref: string): void {
  useLayoutEffect(() => {
    const previousTitle = document.title
    document.title = title

    let descriptionTag = document.querySelector<HTMLMetaElement>('meta[name="description"]')
    const descriptionExisted = descriptionTag !== null
    const previousDescription = descriptionTag?.getAttribute('content') ?? null
    if (!descriptionTag) {
      descriptionTag = document.createElement('meta')
      descriptionTag.setAttribute('name', 'description')
      document.head.appendChild(descriptionTag)
    }
    descriptionTag.setAttribute('content', description)

    let canonicalTag = document.querySelector<HTMLLinkElement>('link[rel="canonical"]')
    const canonicalExisted = canonicalTag !== null
    const previousCanonical = canonicalTag?.getAttribute('href') ?? null
    if (!canonicalTag) {
      canonicalTag = document.createElement('link')
      canonicalTag.setAttribute('rel', 'canonical')
      document.head.appendChild(canonicalTag)
    }
    canonicalTag.setAttribute('href', canonicalHref)

    return () => {
      document.title = previousTitle

      if (descriptionExisted) {
        if (previousDescription !== null) descriptionTag?.setAttribute('content', previousDescription)
      } else {
        descriptionTag?.remove()
      }

      if (canonicalExisted) {
        if (previousCanonical !== null) canonicalTag?.setAttribute('href', previousCanonical)
      } else {
        canonicalTag?.remove()
      }
    }
  }, [title, description, canonicalHref])
}