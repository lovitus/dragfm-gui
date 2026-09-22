from pathlib import Path

def edit(file,before,after):
 p=Path(file);s=p.read_text();assert s.count(before)==1,(file,before[:100],s.count(before));p.write_text(s.replace(before,after))

edit('internal/endpoint/ssh.go','func (r *Remote) Identity(ctx context.Context)', 'func (r *Remote) identityViaCommand(ctx context.Context)')
edit('internal/endpoint/ssh.go','func (r *Remote) FileVersion(ctx context.Context, target string) (uint64, uint64, error) {','func (r *Remote) FileVersion(ctx context.Context, target string) (uint64, uint64, error) {\n ctx, cancel := context.WithTimeout(ctx, 10*time.Second); defer cancel()')
edit('internal/transfer/preflight.go','func probeCapabilities(ctx context.Context, target endpoint.Endpoint, versionPath, spacePath string) (EndpointCapabilities, error) {','func probeCapabilities(ctx context.Context, target endpoint.Endpoint, versionPath, spacePath string) (EndpointCapabilities, error) {\n ctx, cancel := context.WithTimeout(ctx, 15*time.Second); defer cancel()')
edit('internal/endpoint/physical_path.go','if err == nil && resolved != "" {','if err == nil && !path.IsAbs(resolved) { return "", errors.New("physical path resolver returned no absolute directory") }\n\t\tif err == nil {')
edit('build/build-wails-macos.sh','(cd "$output_dir" && shasum', 'codesign --force --sign - "$output_dir/dragfm-gui-wails-darwin-${DRAGFM_MAC_ARCH:-arm64}"\n\n(cd "$output_dir" && shasum')
# Native failure evidence belongs only to generated disposable fixture processes.
edit('build/ci/native-smoke.py','PrintMotd no\nSubsystem', 'PrintMotd no\nLogLevel DEBUG2\nSubsystem')
edit('build/ci/native-smoke.py',"                raise RuntimeError(f'{phase}: native acceptance failed')", "                with (evidence / f'{phase}-fixture-processes.log').open('wb') as processes:\n                    subprocess.run(['ps', '-axo', 'pid,ppid,pgid,state,etime,command'], stdout=processes, stderr=subprocess.STDOUT, timeout=5, check=False)\n                raise RuntimeError(f'{phase}: native acceptance failed')")
edit('.github/workflows/platforms.yml', '      TARGET_ARCH: ${{ matrix.goarch }}', "      TARGET_ARCH: ${{ matrix.goarch }}\n      PYTHONUTF8: '1'")
edit('.github/workflows/platforms.yml', '      - name: Native window, local filesystem, PTY and process-restart acceptance', "      - name: Windows file-identity and child-process cancellation regressions\n        if: runner.os == 'Windows'\n        run: go test -mod=vendor -count=1 -timeout=2m -run 'TestWindowsFileIdentity|TestLocalCommandCancellation' ./internal/endpoint\n      - name: Native window, local filesystem, PTY and process-restart acceptance")
