import { describe, it, expect } from 'bun:test'
import { bannerVariants, bannerCtaVariants } from './bannerVariants'

describe('bannerCtaVariants', () => {
  it('primary variant uses semantic primary tokens', () => {
    const className = bannerCtaVariants({ variant: 'primary' })
    expect(className).toContain('bg-primary')
    expect(className).toContain('text-primary-foreground')
  })

  it('warning variant is unchanged', () => {
    const className = bannerCtaVariants({ variant: 'warning' })
    expect(className).toContain('bg-warning')
    expect(className).toContain('text-warning-foreground')
    expect(className).toContain('hover:bg-warning/90')
  })
})

describe('bannerVariants', () => {
  it('info variant contains no raw palette literal', () => {
    const className = bannerVariants({ variant: 'info' })
    expect(className).not.toMatch(/\b(?:blue|red|green|amber|slate|gray|grey)-\d/)
  })
})
