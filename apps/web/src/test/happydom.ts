// Happy DOM setup - must load FIRST before other test setup
// URL config fixes BrowserRouter "Invalid URL" errors (happy-dom Discussion #690)
import { GlobalRegistrator } from '@happy-dom/global-registrator'

GlobalRegistrator.register({ url: 'http://localhost:3000', width: 1920, height: 1080 })

// Make navigator.clipboard configurable for userEvent.setup()
const originalClipboard = navigator.clipboard
Object.defineProperty(navigator, 'clipboard', {
  value: originalClipboard,
  configurable: true,
  writable: true,
})

// Happy DOM has no layout engine, so clientWidth/clientHeight return 0.
// @tanstack/virtual-core >=3.14.26 returns an empty range when the scroll container
// has outerSize === 0, so give virtual-scroll-container default viewport dimensions.
const origClientWidthDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
const origClientHeightDesc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight')
Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
  configurable: true,
  get() {
    const val = origClientWidthDesc?.get ? origClientWidthDesc.get.call(this) : 0
    if (val === 0 && this.dataset?.testid === 'virtual-scroll-container') {
      return 800
    }
    return val
  },
})
Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
  configurable: true,
  get() {
    const val = origClientHeightDesc?.get ? origClientHeightDesc.get.call(this) : 0
    if (val === 0 && this.dataset?.testid === 'virtual-scroll-container') {
      return 600
    }
    return val
  },
})
