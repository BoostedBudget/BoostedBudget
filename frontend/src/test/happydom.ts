// Issue #575: Happy DOM setup for Bun tests
// This must be loaded FIRST before any other test setup
// URL config fixes BrowserRouter "Invalid URL" errors (happy-dom Discussion #690)
import { GlobalRegistrator } from '@happy-dom/global-registrator'

GlobalRegistrator.register({ url: 'http://localhost:3000', width: 1920, height: 1080 })
