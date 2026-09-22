declare module '*.css'

interface Window {
  go?: {
    webgui?: {
      App?: Record<string, (...args: unknown[]) => Promise<unknown>>
    }
  }
  runtime?: {
    EventsOn?: (name: string, callback: (...args: unknown[]) => void) => () => void
    EventsOff?: (name: string) => void
  }
}
