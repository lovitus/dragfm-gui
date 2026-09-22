import type { SVGProps } from 'react'

export type IconName = 'folder' | 'file' | 'link' | 'up' | 'chevron' | 'refresh' | 'trash' | 'hash' | 'settings' | 'lock' | 'terminal' | 'copy' | 'move' | 'close' | 'check' | 'alert' | 'stop'

const paths: Record<IconName, React.ReactNode> = {
  folder: <path d="M3.5 5.5h5l1.7 2h10.3v10.8a1.7 1.7 0 0 1-1.7 1.7H5.2a1.7 1.7 0 0 1-1.7-1.7V5.5Z" />,
  file: <path d="M6 3.5h7l5 5V20H6V3.5Zm7 0v5h5" />,
  link: <><path d="M9.5 14.5 14.5 9.5"/><path d="M7.3 16.7 5.8 18.2a3 3 0 0 1-4.2-4.2l3.2-3.2A3 3 0 0 1 9 10.7M14.7 7.3l1.5-1.5a3 3 0 1 1 4.2 4.2l-3.2 3.2a3 3 0 0 1-4.2.1"/></>,
  up: <path d="m5 14 7-7 7 7M12 7v13" />,
  chevron: <path d="m9 5 7 7-7 7" />,
  refresh: <><path d="M19 8V4l-2 2a8 8 0 1 0 2.3 8"/><path d="M19 4h-4"/></>,
  trash: <><path d="M5 7h14M9 7V4h6v3M7 7l1 13h8l1-13M10 10v7M14 10v7"/></>,
  hash: <path d="M9 3 7 21M17 3l-2 18M4 9h16M3 15h16" />,
  settings: <><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-1.6v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/></>,
  lock: <><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></>,
  terminal: <path d="m5 7 4 4-4 4M11 17h8" />,
  copy: <><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></>,
  move: <path d="M4 12h16M14 6l6 6-6 6" />,
  close: <path d="m6 6 12 12M18 6 6 18" />,
  check: <path d="m5 12 4 4L19 6" />,
  alert: <><path d="M12 3 2.5 20h19L12 3Z"/><path d="M12 9v5M12 17.5v.1"/></>,
  stop: <rect x="6" y="6" width="12" height="12" rx="1" />,
}

export default function Icon({ name, ...props }: { name: IconName } & SVGProps<SVGSVGElement>) {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" {...props}>{paths[name]}</svg>
}
