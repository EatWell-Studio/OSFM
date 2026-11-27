import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'
import '@/i18n'

// jsdom has no ResizeObserver, which Radix primitives under src/components/ui (the checkbox, for one) use to measure
// themselves: an inert stand-in. Not in the scripts' tests, which run without a DOM.
if (typeof window !== 'undefined' && typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe = () => undefined
    unobserve = () => undefined
    disconnect = () => undefined
  }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
