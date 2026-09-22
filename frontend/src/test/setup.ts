import '@testing-library/jest-dom/vitest'
// jsdom does not implement layout; native acceptance covers real geometry.
Object.defineProperty(Range.prototype, 'getClientRects', { configurable: true, value: () => [] })
Object.defineProperty(Range.prototype, 'getBoundingClientRect', { configurable: true, value: () => new DOMRect() })

Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  configurable: true,
  value: () => null,
})

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

Object.defineProperty(globalThis, 'ResizeObserver', { configurable: true, value: TestResizeObserver })

// jsdom does not implement Range layout. Native acceptance checks real geometry.
Object.defineProperty(Range.prototype, 'getClientRects', { configurable: true, value: () => [] })
Object.defineProperty(Range.prototype, 'getBoundingClientRect', { configurable: true, value: () => new DOMRect() })
