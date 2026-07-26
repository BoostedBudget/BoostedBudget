/**
 * McpTocRail — sticky table-of-contents rail for the docs half of /mcp (Issue #1411).
 *
 * Per task-P8-brief.md § "Accessibility": `<nav aria-label="On this page">` with a
 * `<ul>` of real anchors; active item tracked via IntersectionObserver (not scroll
 * math); collapses to a `<details>` "On this page" on mobile rather than
 * disappearing entirely.
 *
 * Exactly one `<nav aria-label="On this page">` exists in the DOM (the sticky
 * desktop rail) to avoid a duplicate landmark; the mobile disclosure reuses the
 * same link list without a second nav wrapper.
 */

import { useEffect, useState } from 'react'
import { cn } from '@/lib/utils'

export interface McpTocSection {
  id: string
  label: string
}

export interface McpTocRailProps {
  sections: McpTocSection[]
}

/** Tracks which section is currently in view via IntersectionObserver. */
function useActiveSection(sections: McpTocSection[]): string | null {
  const [activeId, setActiveId] = useState<string | null>(null)

  useEffect(() => {
    const targets = sections
      .map((s) => document.getElementById(s.id))
      .filter((el): el is HTMLElement => el !== null)

    if (targets.length === 0) return

    const observer = new IntersectionObserver(
      (entries) => {
        const intersecting = entries.filter((e) => e.isIntersecting)
        if (intersecting.length === 0) return
        // Prefer the entry closest to the top of the viewport.
        const closest = intersecting.reduce((a, b) =>
          a.boundingClientRect.top <= b.boundingClientRect.top ? a : b
        )
        setActiveId(closest.target.id)
      },
      { rootMargin: '-20% 0px -70% 0px' }
    )

    targets.forEach((el) => observer.observe(el))
    return () => observer.disconnect()
  }, [sections])

  return activeId
}

function TocLinkList({ sections, activeId }: { sections: McpTocSection[]; activeId: string | null }) {
  return (
    <ul className="space-y-1">
      {sections.map((section) => {
        const isActive = activeId === section.id
        return (
          <li key={section.id}>
            <a
              href={`#${section.id}`}
              aria-current={isActive ? 'true' : undefined}
              className={cn(
                'block border-l-2 py-1 pl-3 text-sm transition-colors',
                isActive
                  ? 'border-primary font-medium text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
            >
              {section.label}
            </a>
          </li>
        )
      })}
    </ul>
  )
}

export function McpTocRail({ sections }: McpTocRailProps) {
  const activeId = useActiveSection(sections)

  return (
    <>
      {/* Mobile: collapsible disclosure, hidden at the lg breakpoint and up. */}
      <details className="mb-6 rounded-lg border border-border bg-card p-4 lg:hidden">
        <summary className="cursor-pointer text-sm font-semibold text-foreground">
          On this page
        </summary>
        <div className="mt-3">
          <TocLinkList sections={sections} activeId={activeId} />
        </div>
      </details>

      {/* Desktop: sticky rail, hidden below the lg breakpoint. */}
      <nav
        aria-label="On this page"
        className="hidden shrink-0 lg:sticky lg:top-24 lg:block lg:w-48 lg:self-start"
      >
        <TocLinkList sections={sections} activeId={activeId} />
      </nav>
    </>
  )
}
