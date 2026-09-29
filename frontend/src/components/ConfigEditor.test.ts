import { describe, expect, it } from 'vitest'
import { secretRanges } from './ConfigEditor'

function maskedValues(source: string): string[] {
  return secretRanges(source).map(({ from, to }) => source.slice(from, to))
}

describe('Markdown credential masking', () => {
  it.each(['p%40ss', 'p@ss', 'p:#@ss'])('masks the whole literal SOCKS password %s', (password) => {
    const source = '#socks池\n##pool\nuser:' + password + '@192.0.2.1:1080'
    expect(maskedValues(source)).toEqual([password])
  })

  it('masks inert legacy credentials including escaped quotes and newlines', () => {
    const recovery = JSON.stringify(['legacy@password', 'escaped"password\nvalue'])
    const source = '#主机\n##host\nu@192.0.2.1:22\n###待核对旧密码\n' + recovery + '\n#私钥\n#socks池'
    expect(maskedValues(source)).toEqual([recovery])
  })
  it('masks inline SSH/SOCKS passwords and optional password fields', () => {
    const source = `#主机
##host
uhome:"EXAMPLE_PASSWORD_2"@1.2.3.4:41122
###sudo密码
sudo-secret
###root密码
root-secret
#私钥
##key
###口令
key-secret
###私钥
-----BEGIN OPENSSH PRIVATE KEY-----
visible-key-body
-----END OPENSSH PRIVATE KEY-----
#socks池
##pool
user1:pass1@1.1.1.1:1080`
    expect(maskedValues(source)).toEqual([
      '"EXAMPLE_PASSWORD_2"',
      'sudo-secret',
      'root-secret',
      'key-secret',
      'pass1',
    ])
    expect(maskedValues(source)).not.toContain('visible-key-body')
  })

  it('does not treat host ports or headings as secrets', () => {
    const source = '#主机\n##host\nuser@10.1.1.1:22\n#socks池\n##pool\n10.1.1.2:1080'
    expect(secretRanges(source)).toEqual([])
  })
})
