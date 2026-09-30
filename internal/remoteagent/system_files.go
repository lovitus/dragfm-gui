package remoteagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SystemFiles uses the OS's OpenSSH server, or Linux POSIX file commands when
// that executable is absent, over task-owned SSH channels. It uploads no
// executable and does not depend on the remote CPU.
// The caller supplies a task-owned transport, not the browsing endpoint.
// Cancellation or unconfirmed shutdown may close that transport to unblock I/O.
type SystemFiles struct {
	Files             *endpoint.Remote
	remote            *endpoint.Remote
	transport         *systemFileTransport
	elevated          bool
	password          string
	passwordMu        sync.RWMutex
	sudoAuthenticated atomic.Bool
	mu                sync.Mutex
	partials          map[string]config.PartialRecord
	workspace         *Workspace
	journal           WorkspaceJournal
	closed            atomic.Bool
	once              sync.Once
	closeErr          error
}

func OpenSystemFiles(ctx context.Context, remote *endpoint.Remote, elevated bool, password string, journals ...WorkspaceJournal) (_ *SystemFiles, result error) {
	if remote == nil || strings.ContainsAny(password, "\r\n") {
		return nil, errors.New("invalid system file channel or multiline sudo credential")
	}
	if len(journals) > 1 {
		return nil, errors.New("system file channel requires at most one recovery journal")
	}
	var journal WorkspaceJournal
	var registrars []func(config.WorkspaceRecord) error
	if len(journals) == 1 && journals[0] != nil {
		journal = journals[0]
		registrars = append(registrars, func(record config.WorkspaceRecord) error { return journal(record, false) })
	}
	handshake, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := watchStartup(handshake, remote)
	defer stop()
	var uid bytes.Buffer
	if err := remote.Exec(handshake, "id -u", endpoint.ExecOptions{Stdout: &uid}); err != nil {
		return nil, fmt.Errorf("system file channel account: %w", err)
	}
	if strings.TrimSpace(uid.String()) == "0" {
		elevated = false
	}
	// Distribution-owned locations only; do not promote a user-controlled PATH
	// executable to root. Failure is explicit, not a fake ordinary-account view.
	var binary bytes.Buffer
	probe := "for p in /usr/lib/openssh/sftp-server /usr/libexec/openssh/sftp-server /usr/lib/ssh/sftp-server /usr/libexec/sftp-server; do if [ -x \"$p\" ]; then printf '%s' \"$p\"; exit 0; fi; done; exit 69"
	if err := remote.Exec(handshake, probe, endpoint.ExecOptions{Stdout: &binary}); err != nil {
		var exited *ssh.ExitError
		if !errors.As(err, &exited) || exited.ExitStatus() != 69 || errors.Is(err, endpoint.ErrCommandExitUnconfirmed) {
			return nil, fmt.Errorf("probe system OpenSSH sftp-server: %w", err)
		}
	}
	server := binary.String()
	switch server {
	case "", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server", "/usr/lib/ssh/sftp-server", "/usr/libexec/sftp-server":
	default:
		return nil, errors.New("unexpected system sftp-server path")
	}
	// The upload identity creates only a private marker/lease directory, never
	// a credential file or executable. SFTP takes its OWN inherited directory
	// lock AFTER sudo (sudo may close extra descriptors). Losing the controller
	// lease therefore cannot authorize deletion under a surviving server.
	work, err := NewWorkspace(handshake, remote, "/tmp", registrars...)
	if err != nil {
		return nil, transfer.PreserveSource(fmt.Errorf("prepare system filesystem lease: %w", err))
	}
	started := false
	defer func() {
		if result == nil {
			return
		}
		// Once Start was attempted, a missing handshake is not process-exit
		// evidence. Release our idle lease but retain the directory/record.
		closeErr := work.Close(!started)
		if !started && closeErr == nil && journal != nil {
			closeErr = journal(work.Record(), true)
		}
		if started || closeErr != nil {
			result = transfer.PreserveSource(errors.Join(result, closeErr))
		}
	}()
	s := &SystemFiles{remote: remote, elevated: elevated, password: password, workspace: work, journal: journal, partials: make(map[string]config.PartialRecord)}
	if server == "" {
		s.Files = remote.BorrowCommandFilesystem(s.Exec, s.fileVersion, s.trackPartial, s.setOwner)
		// Validate actual privilege and the required Linux command set before
		// exposing the view or saving a password. Do not infer authentication
		// from the earlier ordinary-account path probe.
		started = true
		err := s.Files.Exec(handshake, "for p in cat find stat readlink mkdir chmod chown touch rm mv sync; do command -v \"$p\" >/dev/null || exit 69; done", endpoint.ExecOptions{})
		if err != nil {
			closeErr := s.Files.Close()
			// A definite rejected command (e.g. incorrect sudo password) has
			// opened no file. The failure defer may reclaim its empty lease.
			started = errors.Is(err, endpoint.ErrCommandExitUnconfirmed) || closeErr != nil
			return nil, errors.Join(fmt.Errorf("POSIX filesystem handshake: %w", err), closeErr)
		}
		stop()
		if err := handshake.Err(); err != nil {
			return nil, errors.Join(err, s.Files.Close())
		}
		return s, nil
	}
	channel, err := remote.SSHClient().NewSession()
	if err != nil {
		return nil, err
	}
	input, err := channel.StdinPipe()
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	output, err := channel.StdoutPipe()
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	var diagnostic helperOutput
	channel.Stderr = &diagnostic
	command, prompt := sudoCommand("/bin/sh -c "+quotePOSIX(work.Command("exec "+quotePOSIX(server)+" -e")), elevated, password != "")
	started = true
	if err := channel.Start(command); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("start system SFTP: %w", err)
	}
	if _, err := io.WriteString(input, sudoInput(password, prompt)); err != nil {
		exitErr := waitStartupExit(channel, input, remote)
		started = exitErr != nil
		return nil, errors.Join(err, exitErr)
	}
	client, err := sftp.NewClientPipe(output, input)
	if err == nil && prompt != "" {
		err = diagnostic.waitFor(handshake, sudoResultMarker(prompt))
	}
	if err != nil {
		exitErr := waitStartupExit(channel, input, remote)
		if client != nil {
			_ = client.Close()
		}
		started = exitErr != nil
		return nil, errors.Join(fmt.Errorf("system SFTP handshake: %w: %s", err, diagnostic.String()), exitErr)
	}
	s.sudoAuthenticated.Store(prompt != "" && strings.Contains(diagnostic.String(), prompt))
	// OpenSSH REALPATH resolves symlinks. Unlike the embedded pkg/sftp server,
	// it needs no separate physical-path override. Identity still comes from SSH.
	s.transport = &systemFileTransport{client: client, channel: channel, remote: remote}
	s.Files = remote.BorrowFileChannel(client, s.transport, s.fileVersion, nil, s.trackPartial, s.setOwner, s.syncPaths)
	stop()
	if err := handshake.Err(); err != nil {
		// The failure defer owns the lease; do not close/remove it twice.
		return nil, errors.Join(err, s.Files.Close())
	}
	return s, nil
}

func (s *SystemFiles) SudoAuthenticated() bool { return s.sudoAuthenticated.Load() }

// Exec is only exposed to transfer-internal filesystem/tool operations. It is
// never installed on the browsing endpoint or used for an interactive shell.
func (s *SystemFiles) Exec(ctx context.Context, script string, options endpoint.ExecOptions) error {
	if s.closed.Load() {
		return errors.New("system file channel is closed")
	}
	if options.Directory != "" {
		script = "cd -- " + quotePOSIX(options.Directory) + " && " + script
		options.Directory = ""
	}
	return s.execute(ctx, s.workspace.Command(script), options)
}

// Only setup/cleanup uses this unleased executor. Ordinary commands enter
// through Exec and acquire their own shared lock, held across fork/exec.
func (s *SystemFiles) execute(ctx context.Context, script string, options endpoint.ExecOptions) error {
	s.passwordMu.RLock()
	password := s.password
	s.passwordMu.RUnlock()
	command, prompt := sudoCommand("/bin/sh -c "+quotePOSIX(script), s.elevated, password != "")
	input := io.Reader(strings.NewReader(sudoInput(password, prompt)))
	if options.Stdin != nil {
		input = io.MultiReader(input, options.Stdin)
	}
	options.Stdin = input
	var diagnostic helperOutput
	if options.Stderr != nil {
		options.Stderr = io.MultiWriter(&diagnostic, options.Stderr)
	} else {
		options.Stderr = &diagnostic
	}
	if err := s.remote.Exec(ctx, command, options); err != nil {
		return fmt.Errorf("system filesystem command: %w: %s", err, diagnostic.String())
	}
	// Exec joins stderr before returning. Both the actual sudo prompt and its
	// post-authentication delimiter must exist; NOPASSWD consumes the unused
	// password frame but never authenticates it.
	if prompt != "" && strings.Contains(diagnostic.String(), prompt) && strings.Contains(diagnostic.String(), sudoResultMarker(prompt)) {
		s.sudoAuthenticated.Store(true)
	}
	return nil
}

func (s *SystemFiles) fileVersion(ctx context.Context, target string) (uint64, uint64, error) {
	var output bytes.Buffer
	if err := s.Exec(ctx, "command stat -c '%d %i' -- "+quotePOSIX(target), endpoint.ExecOptions{Stdout: &output}); err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(output.String())
	if len(fields) != 2 {
		return 0, 0, errors.New("system stat returned no file identity")
	}
	device, deviceErr := strconv.ParseUint(fields[0], 10, 64)
	inode, inodeErr := strconv.ParseUint(fields[1], 10, 64)
	if err := errors.Join(deviceErr, inodeErr); err != nil || inode == 0 {
		return 0, 0, errors.Join(err, errors.New("system stat returned invalid file identity"))
	}
	return device, inode, nil
}

func (s *SystemFiles) setOwner(ctx context.Context, target string, uid, gid uint32) error {
	return s.Exec(ctx, "command chown -h "+strconv.FormatUint(uint64(uid), 10)+":"+strconv.FormatUint(uint64(gid), 10)+" -- "+quotePOSIX(target), endpoint.ExecOptions{})
}

func (s *SystemFiles) syncPaths(ctx context.Context, paths []string) error {
	return endpoint.SyncPathsWithExec(ctx, s.Exec, paths)
}

func (s *SystemFiles) trackPartial(ctx context.Context, target string, track bool) error {
	if !validPartialPath(target) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return errors.New("system file channel is closed")
	}
	next := make(map[string]config.PartialRecord, len(s.partials)+1)
	for key, value := range s.partials {
		next[key] = value
	}
	if !track {
		for partial := range next {
			if partial == target || strings.HasPrefix(partial, strings.TrimSuffix(target, "/")+"/") {
				delete(next, partial)
			}
		}
	} else {
		record, tracked := next[target]
		_, statErr := s.Files.Stat(ctx, target)
		if !tracked {
			if !errors.Is(statErr, fs.ErrNotExist) {
				if statErr == nil {
					return fs.ErrExist
				}
				return statErr
			}
			physical, err := s.Files.PhysicalPath(ctx, target)
			if err != nil {
				return err
			}
			device, inode, err := s.fileVersion(ctx, path.Dir(physical))
			if err != nil {
				return err
			}
			record = config.PartialRecord{Path: target, ParentID: fmt.Sprintf("%d:%d", device, inode)}
		} else if statErr == nil {
			physical, err := s.Files.PhysicalPath(ctx, target)
			if err != nil {
				return transfer.PreserveSource(err)
			}
			device, inode, err := s.fileVersion(ctx, path.Dir(physical))
			if err != nil || fmt.Sprintf("%d:%d", device, inode) != record.ParentID {
				return transfer.PreserveSource(errors.Join(errors.New("system partial parent changed; retained"), err))
			}
			device, inode, err = s.fileVersion(ctx, target)
			if err != nil {
				return transfer.PreserveSource(err)
			}
			actual := fmt.Sprintf("%d:%d", device, inode)
			if record.FileID != "" && record.FileID != actual {
				return transfer.PreserveSource(errors.New("system partial inode changed; retained"))
			}
			record.FileID = actual
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		next[target] = record
	}
	// A pinned private staged root covers its descendant file partials. The
	// map retains every identity for replacement checks without saving the
	// entire vault for each file inside that staged root.
	var records []config.PartialRecord
	for _, record := range next {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return len(records[i].Path) < len(records[j].Path) })
	roots := make([]config.PartialRecord, 0, len(records))
	for _, record := range records {
		covered := false
		for _, root := range roots {
			if strings.HasPrefix(record.Path, root.Path+"/") {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, record)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	if !slices.Equal(s.workspace.record.Partials, roots) {
		record := s.workspace.Record()
		record.Partials = roots
		if s.journal != nil {
			if err := s.journal(record, false); err != nil {
				return transfer.PreserveSource(err)
			}
		}
		s.workspace.record.Partials = roots
	}
	s.partials = next
	return nil
}

func (s *SystemFiles) Close() error {
	s.once.Do(func() {
		s.closed.Store(true)
		defer func() {
			s.passwordMu.Lock()
			s.password = ""
			s.passwordMu.Unlock()
		}()
		filesErr := s.Files.Close()
		// EOF/Wait stops sftp-server; the POSIX view cancels and joins its
		// commands. Neither path may erase a partial on unconfirmed exit.
		leaseErr := s.workspace.Close(false)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := errors.Join(filesErr, leaseErr); err != nil {
			s.closeErr = transfer.PreserveSource(err)
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.workspace.removePartials(cleanup, s.execute, s.workspace.record.Partials); err != nil {
			s.closeErr = transfer.PreserveSource(fmt.Errorf("owned system partial cleanup: %w", err))
			return
		}
		if s.journal != nil {
			s.closeErr = s.journal(s.workspace.Record(), true)
		}
		s.partials = nil
	})
	return s.closeErr
}

type systemFileTransport struct {
	client  *sftp.Client
	channel *ssh.Session
	remote  *endpoint.Remote
	once    sync.Once
	err     error
}

// A rejected executable/sudo handshake can be cleaned up only after an actual
// exit status. This is also used by helper installation: no-exec fallback must
// not leak a new installation for every definite policy rejection. The caller
// still takes the directory's exclusive lease before removing anything.
func waitStartupExit(channel *ssh.Session, input io.Closer, remote *endpoint.Remote) error {
	done := make(chan error, 1)
	go func() {
		_ = input.Close()
		done <- channel.Wait()
	}()
	select {
	case err := <-done:
		var exited *ssh.ExitError
		if err != nil && (!errors.As(err, &exited) || exited.Signal() != "") {
			_ = remote.Close()
			return errors.Join(endpoint.ErrCommandExitUnconfirmed, err)
		}
		_ = channel.Close()
		return nil
	case <-time.After(3 * time.Second):
		_ = remote.Close() // Unblocks local Wait; does not prove remote exit.
		return endpoint.ErrCommandExitUnconfirmed
	}
}

func (s *systemFileTransport) Close() error {
	s.once.Do(func() {
		done := make(chan error, 1)
		go func() {
			_ = s.client.Close() // Sends stdin EOF and drains the SFTP reader.
			done <- s.channel.Wait()
		}()
		select {
		case err := <-done:
			var exited *ssh.ExitError
			if err != nil && (!errors.As(err, &exited) || exited.Signal() != "") {
				s.err = errors.Join(endpoint.ErrCommandExitUnconfirmed, fmt.Errorf("system SFTP exit unconfirmed; partial cleanup deferred: %w", err))
			}
		case <-time.After(3 * time.Second):
			s.err = errors.Join(endpoint.ErrCommandExitUnconfirmed, errors.New("system SFTP did not exit; partial cleanup deferred"))
		}
		if s.err != nil {
			_ = s.remote.Close()
		}
		_ = s.channel.Close()
	})
	return s.err
}
