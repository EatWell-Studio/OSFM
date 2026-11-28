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

// jsdom does not lay out, so it has no scrollIntoView (the card scrolls to a shared recommendation): an inert stand-in
// that tests can spy on.
if (typeof window !== 'undefined' && typeof Element.prototype.scrollIntoView !== 'function') {
  Element.prototype.scrollIntoView = () => undefined
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
