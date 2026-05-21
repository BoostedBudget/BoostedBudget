import { describe, it, expect, mock } from 'bun:test'
import React from 'react'
import { render, screen } from '@testing-library/react'

// Mock echarts-for-react/lib/core before importing EChartsChart
mock.module('echarts-for-react/lib/core', () => {
  const MockReactEChartsCore = React.forwardRef<
    unknown,
    {
      option: unknown
      theme?: string
      opts?: { renderer: string }
      style?: React.CSSProperties
      className?: string
      notMerge?: boolean
      lazyUpdate?: boolean
      onEvents?: Record<string, unknown>
    }
  >(({ option, theme, opts, style, className, notMerge, lazyUpdate, onEvents }, _ref) => (
    <div
      data-testid="echarts-core"
      data-theme={theme}
      data-renderer={opts?.renderer}
      data-not-merge={String(notMerge)}
      data-lazy-update={String(lazyUpdate)}
      data-has-events={onEvents ? 'true' : 'false'}
      style={style}
      className={className}
    >
      {JSON.stringify(option)}
    </div>
  ))
  MockReactEChartsCore.displayName = 'MockReactEChartsCore'
  return { default: MockReactEChartsCore }
})

import { EChartsChart } from './EChartsChart'

describe('EChartsChart', () => {
  it('renders without crashing', () => {
    render(<EChartsChart option={{ series: [] }} />)
    expect(screen.getByTestId('echarts-core')).toBeDefined()
  })

  it('uses SVG renderer', () => {
    render(<EChartsChart option={{ series: [] }} />)
    expect(screen.getByTestId('echarts-core').dataset.renderer).toBe('svg')
  })

  it('uses bb-light theme when isDark is false', () => {
    render(<EChartsChart option={{ series: [] }} isDark={false} />)
    expect(screen.getByTestId('echarts-core').dataset.theme).toBe('bb-light')
  })

  it('uses bb-dark theme when isDark is true', () => {
    render(<EChartsChart option={{ series: [] }} isDark={true} />)
    expect(screen.getByTestId('echarts-core').dataset.theme).toBe('bb-dark')
  })

  it('defaults notMerge to false', () => {
    render(<EChartsChart option={{ series: [] }} />)
    expect(screen.getByTestId('echarts-core').dataset.notMerge).toBe('false')
  })

  it('passes custom style prop', () => {
    render(<EChartsChart option={{ series: [] }} style={{ width: '200px' }} />)
    expect(screen.getByTestId('echarts-core').style.width).toBe('200px')
  })

  it('passes className prop', () => {
    render(<EChartsChart option={{ series: [] }} className="test-class" />)
    expect(screen.getByTestId('echarts-core').className).toBe('test-class')
  })

  it('passes onEvents to underlying chart', () => {
    const handler = mock(() => {})
    render(<EChartsChart option={{ series: [] }} onEvents={{ click: handler }} />)
    expect(screen.getByTestId('echarts-core').dataset.hasEvents).toBe('true')
  })

  it('has a displayName for debugging', () => {
    expect(EChartsChart.displayName).toBe('EChartsChart')
  })

  it('wraps chart in a data-ph-mask container', () => {
    const { container } = render(<EChartsChart option={{ series: [] }} />)
    const wrapper = container.firstChild as HTMLElement
    expect(wrapper.dataset.phMask).toBeDefined()
  })

  it('has role="img" on the container', () => {
    render(<EChartsChart option={{ series: [] }} />)
    expect(screen.getByRole('img')).toBeDefined()
  })

  it('uses default aria-label when ariaLabel prop omitted', () => {
    render(<EChartsChart option={{ series: [] }} />)
    expect(screen.getByRole('img').getAttribute('aria-label')).toBe('Financial chart')
  })

  it('uses custom ariaLabel prop when provided', () => {
    render(<EChartsChart option={{ series: [] }} ariaLabel="Balance history chart" />)
    expect(screen.getByRole('img').getAttribute('aria-label')).toBe('Balance history chart')
  })

  it('updates theme when isDark changes', () => {
    const { rerender } = render(<EChartsChart option={{ series: [] }} isDark={false} />)
    expect(screen.getByTestId('echarts-core').dataset.theme).toBe('bb-light')
    rerender(<EChartsChart option={{ series: [] }} isDark={true} />)
    expect(screen.getByTestId('echarts-core').dataset.theme).toBe('bb-dark')
  })
})
