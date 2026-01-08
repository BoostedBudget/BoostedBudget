// Issue #575: Happy DOM setup for Bun tests
// This must be loaded FIRST before any other test setup
import { GlobalRegistrator } from '@happy-dom/global-registrator'

GlobalRegistrator.register()
