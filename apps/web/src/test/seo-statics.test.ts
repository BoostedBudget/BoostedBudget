/**
 * SEO statics tests
 *
 * Verifies the SEO-facing static files (robots.txt, sitemap.xml) and the
 * index.html metadata corrections landed by issue #1411. These are plain
 * filesystem assertions -- no XML parser dependency is introduced (see
 * apps/web/src/features/landing/LandingPage.css.test.ts for the pattern
 * this follows).
 *
 * Paths are resolved relative to this file (and therefore the repo), not
 * the process cwd, so the test works from any invocation directory.
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync, existsSync } from 'fs'
import { dirname, join } from 'path'
import { fileURLToPath } from 'url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const webRoot = join(currentDir, '..', '..') // apps/web

const robotsPath = join(webRoot, 'public', 'robots.txt')
const sitemapPath = join(webRoot, 'public', 'sitemap.xml')
const indexHtmlPath = join(webRoot, 'index.html')

describe('robots.txt', () => {
  it('exists and is non-empty', () => {
    expect(existsSync(robotsPath)).toBe(true)
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content.trim().length).toBeGreaterThan(0)
  })

  it('contains a Sitemap directive pointing at the production sitemap', () => {
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content).toMatch(/^Sitemap:\s*https:\/\/boostedbudget\.com\/sitemap\.xml\s*$/m)
  })

  it('disallows authenticated application areas (spot-check)', () => {
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content).toMatch(/^Disallow:\s*\/dashboard\s*$/m)
    expect(content).toMatch(/^Disallow:\s*\/admin\s*$/m)
  })
})

describe('sitemap.xml', () => {
  const content = readFileSync(sitemapPath, 'utf-8')

  it('exists and parses as XML with a urlset root', () => {
    expect(existsSync(sitemapPath)).toBe(true)
    expect(content).toMatch(/<urlset[^>]*>/)
    expect(content).toMatch(/<\/urlset>\s*$/)
  })

  const publicUrls = [
    'https://boostedbudget.com/',
    'https://boostedbudget.com/pricing',
    'https://boostedbudget.com/mcp',
    'https://boostedbudget.com/legal/privacy-policy',
    'https://boostedbudget.com/legal/terms',
    'https://boostedbudget.com/legal/cookie-policy',
  ]

  it.each(publicUrls)('contains a <loc> for %s', (url) => {
    expect(content).toContain(`<loc>${url}</loc>`)
  })

  it('contains no lastmod, changefreq, or priority elements', () => {
    expect(content).not.toMatch(/<lastmod>/)
    expect(content).not.toMatch(/<changefreq>/)
    expect(content).not.toMatch(/<priority>/)
  })

  it('contains no authenticated route', () => {
    expect(content).not.toContain('/dashboard')
  })
})

describe('index.html metadata corrections (#1411)', () => {
  const content = readFileSync(indexHtmlPath, 'utf-8')

  it('does not contain the stale "2-month" trial wording', () => {
    expect(content).not.toContain('2-month')
  })

  it('does not contain the stale priceValidUntil property', () => {
    expect(content).not.toContain('priceValidUntil')
  })

  it('does not overclaim "intelligent automation rules"', () => {
    expect(content).not.toContain('intelligent automation rules')
  })

  it('describes automation rules as user-defined', () => {
    expect(content).toContain('user-defined automation rules')
  })
})
