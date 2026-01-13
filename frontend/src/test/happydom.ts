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
