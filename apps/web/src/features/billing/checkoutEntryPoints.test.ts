import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'

describe('checkout entry points', () => {
  it('does not bootstrap Paddle or trust its browser events', () => {
    const app = readFileSync(new URL('../../App.tsx', import.meta.url), 'utf8')
    const html = readFileSync(new URL('../../../index.html', import.meta.url), 'utf8')
    const pricing = readFileSync(new URL('../pricing/PricingPage.tsx', import.meta.url), 'utf8')
    expect(app).not.toContain('Paddle.Initialize')
    expect(html).not.toContain('cdn.paddle.com/paddle')
    expect(pricing).not.toContain('usePaddleEvents')
  })
})
