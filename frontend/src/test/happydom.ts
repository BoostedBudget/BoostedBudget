// Issue #575: Happy DOM setup for Bun tests
// This must be loaded FIRST before any other test setup
// URL config fixes BrowserRouter "Invalid URL" errors (happy-dom Discussion #690)
import { GlobalRegistrator } from '@happy-dom/global-registrator'

GlobalRegistrator.register({ url: 'http://localhost:3000', width: 1920, height: 1080 })

// Fix: Make navigator.clipboard configurable so userEvent can replace it
// userEvent.setup() calls Object.defineProperty on navigator.clipboard
// which fails if it's not configurable (happy-dom sets it as non-configurable)
const originalClipboard = navigator.clipboard
Object.defineProperty(navigator, 'clipboard', {
  value: originalClipboard,
  configurable: true,
  writable: true,
})
