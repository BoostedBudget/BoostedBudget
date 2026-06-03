import { describe, it, expect } from 'bun:test'
import { echarts } from './echarts-setup'

function makeContainer(): HTMLElement {
  const el = document.createElement('div')
  el.style.width = '100px'
  el.style.height = '100px'
  document.body.appendChild(el)
  return el
}

function initChart(container: HTMLElement) {
  return echarts.init(container, null, { renderer: 'svg', width: 100, height: 100 })
}

describe('echarts-setup', () => {
  it('exports the echarts instance', () => {
    expect(echarts).toBeDefined()
    expect(typeof echarts.init).toBe('function')
    expect(typeof echarts.use).toBe('function')
  })

  it('has SVGRenderer registered (can init with svg renderer)', () => {
    const container = makeContainer()
    expect(() => {
      const chart = initChart(container)
      chart.dispose()
    }).not.toThrow()
    container.remove()
  })

  it('has LineChart registered (line series are renderable)', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a', 'b', 'c'] },
        yAxis: { type: 'value' },
        series: [{ type: 'line', data: [1, 2, 3] }],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has BarChart registered (bar series are renderable)', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a', 'b', 'c'] },
        yAxis: { type: 'value' },
        series: [{ type: 'bar', data: [1, 2, 3] }],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has PieChart registered (pie series are renderable)', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({ series: [{ type: 'pie', data: [{ value: 1, name: 'a' }] }] })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has TooltipComponent registered', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a', 'b', 'c'] },
        yAxis: { type: 'value' },
        tooltip: { trigger: 'axis' },
        series: [{ type: 'line', data: [1, 2, 3] }],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has LegendComponent registered', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a'] },
        yAxis: { type: 'value' },
        legend: { data: ['a', 'b'] },
        series: [
          { type: 'bar', name: 'a', data: [1] },
          { type: 'bar', name: 'b', data: [2] },
        ],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has MarkLineComponent registered', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a'] },
        yAxis: { type: 'value' },
        series: [{
          type: 'bar',
          data: [1],
          markLine: { data: [{ yAxis: 0 }] },
        }],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })

  it('has GridComponent registered', () => {
    const container = makeContainer()
    const chart = initChart(container)
    expect(() => {
      chart.setOption({
        xAxis: { type: 'category', data: ['a'] },
        yAxis: { type: 'value' },
        grid: { top: 10, right: 10, bottom: 40, left: 60 },
        series: [{ type: 'line', data: [1] }],
      })
    }).not.toThrow()
    chart.dispose()
    container.remove()
  })
})
