/**
 * LandingPage CSS Tests
 *
 * CRITICAL: These tests validate the CSS rules that enable centering.
 * The landing page centering depends on specific CSS rules in LandingPage.css.
 * If these rules are accidentally removed, the page content will be left-aligned.
 *
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

// Read the CSS file at test time
const currentDir = dirname(fileURLToPath(import.meta.url))
const cssFilePath = join(currentDir, 'LandingPage.css')
const cssContent = readFileSync(cssFilePath, 'utf-8')

describe('LandingPage.css Centering Rules (CRITICAL)', () => {
  /**
   * The .landing-page .mx-auto rule is CRITICAL for centering.
   * Tailwind v4 doesn't generate the mx-auto utility in this project,
   * so we provide our own explicit rule.
   */
  it('should contain .landing-page .mx-auto rule for horizontal centering', () => {
    // Check for the selector
    expect(cssContent).toContain('.landing-page .mx-auto')

    // Check for margin-left: auto
    expect(cssContent).toMatch(/\.landing-page\s+\.mx-auto\s*\{[^}]*margin-left:\s*auto/s)

    // Check for margin-right: auto
    expect(cssContent).toMatch(/\.landing-page\s+\.mx-auto\s*\{[^}]*margin-right:\s*auto/s)
  })

  /**
   * The .landing-page class must exist to trigger the CSS :has() rule in index.css.
   */
  it('should contain .landing-page class definition', () => {
    expect(cssContent).toMatch(/\.landing-page\s*\{/)
  })

  /**
   * Smooth scrolling should be enabled for anchor link navigation.
   */
  it('should have scroll-behavior: smooth for anchor links', () => {
    expect(cssContent).toMatch(/scroll-behavior:\s*smooth/)
  })

  /**
   * The wave-divider styles should exist for section transitions.
   */
  it('should contain wave-divider styles', () => {
    expect(cssContent).toContain('.wave-divider')
  })

  /**
   * Accessibility: prefers-reduced-motion should be respected.
   */
  it('should respect prefers-reduced-motion for accessibility', () => {
    expect(cssContent).toContain('prefers-reduced-motion')
  })
})
