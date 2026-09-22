// Arbitrates file-pane navigation against delayed PTY prompt events. A user
// request owns the path until its listing resolves and its cd is acknowledged.
export class CWDGate {
  private listing = false
  private target: string | null = null
  begin(): void { this.listing = true; this.target = null }
  resolve(path: string, previous: string): void { this.listing = false; this.target = path === previous ? null : path }
  clear(): void { this.listing = false; this.target = null }
  accept(path: string): boolean {
    if (this.listing || (this.target !== null && path !== this.target)) return false
    this.target = null
    return true
  }
}
