from pathlib import Path

def edit(file, before, after):
 p=Path(file); s=p.read_text()
 assert s.count(before)==1, (file, before[:100], s.count(before))
 p.write_text(s.replace(before,after))

p=Path('internal/endpoint/ssh.go'); s=p.read_text()
s=s.replace('func (r *Remote) Open(_ context.Context,', 'func (r *Remote) open(_ context.Context,')
s=s.replace('func (r *Remote) CreateAtomic(_ context.Context,', 'func (r *Remote) createAtomic(_ context.Context,')
s=s.replace('func (r *Remote) AvailableBytes(_ context.Context,', 'func (r *Remote) AvailableBytes(ctx context.Context,')
import re
for name, zero in {'AvailableBytes':'-1, ', 'Home':'"", ', 'List':'nil, ', 'Stat':'Entry{}, ', 'Readlink':'"", ', 'MkdirAll':'', 'Symlink':'', 'Chmod':'', 'Chtimes':'', 'Remove':'', 'Rename':''}.items():
 pattern=r'(func \(r \*Remote\) '+name+r'\([^\n]+\{\n)'
 addition='\tif err := ctx.Err(); err != nil { return '+zero+'err }\n\tstopIO := r.watchIO(ctx); defer stopIO()\n'
 s,count=re.subn(pattern,lambda m:m[0]+addition,s)
 assert count==1,(name,count)
# NewSession itself can hang on an unresponsive peer before Exec's select.
a=s.index('func (r *Remote) Exec('); b=s.index('func (r *Remote) OpenPTY(',a)
part=s[a:b]
old='\tsession, err := r.client.NewSession()'
assert part.count(old)==1
part=part.replace(old,'\tstopOpen := r.watchIO(ctx)\n\tsession, err := r.client.NewSession()\n\tstopOpen()')
s=s[:a]+part+s[b:]
p.write_text(s)
edit('internal/transfer/transfer.go', '\tif sourcePath == targetPath {', '\tsourcePath, targetPath, err = physicalOperationPaths(ctx, operation, sourcePath, targetPath)\n\tif err != nil { return err }\n\tif sourcePath == targetPath {')
edit('internal/webgui/lifecycle.go', '\tfor _, key := range a.document.Keys {', '\tadd(string(a.password))\n\tfor _, key := range a.document.Keys {')
# Median is calculated by the caller using three requests, not nine nested dials.
p=Path('internal/agentservice/service.go');s=p.read_text();a=s.index('func tcpProbe(');b=s.index('func runSCP(',a)
s=s[:a]+'''func tcpProbe(address string) (string, error) {
 if address == "" { return "", errors.New("probe address is empty") }
 started := time.Now()
 connection, err := net.DialTimeout("tcp", address, 2*time.Second)
 if err != nil { return "", err }
 _ = connection.Close()
 return strconv.FormatInt(time.Since(started).Milliseconds(), 10), nil
}

'''+s[b:];p.write_text(s)
edit('build/ci/sshd.Dockerfile', 'ncat sudo iproute2', 'ncat sudo iptables iproute2')
edit('build/ci/ssh-integration.sh', 'HostedSSHCommandCancellationLatency)', 'HostedSSHCommandCancellationLatency|HostedReviewedRouteFailureRecovery|HostedReviewedFailedRelayCacheReprobes)')
# Python on Windows must not use the ANSI code page or translate marker LF to CRLF.
p=Path('build/ci/portable-smoke.py');s=p.read_text()
s=s.replace('.read_text()', ".read_text(encoding='utf-8')")
s=s.replace("(root / 'SMOKE_ONLY').write_text('dragfm-native-smoke-v1\\n')", "(root / 'SMOKE_ONLY').write_bytes(b'dragfm-native-smoke-v1\\n')")
s=s.replace("json.dumps(plan) + ';\\n' + script)", "json.dumps(plan) + ';\\n' + script, encoding='utf-8', newline='\\n')")
p.write_text(s)
