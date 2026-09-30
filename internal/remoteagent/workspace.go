package remoteagent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"golang.org/x/crypto/ssh"
)

// Workspace is a privately owned temporary directory. System-tool workspaces
// keep an SSH lease; helper installations acquire the kernel lease themselves.
// Version 2 promises directory-flock participation by EVERY writer. The SSH
// lease spans gaps between commands; each writer also inherits its own lock,
// so losing the controller cannot make a surviving pipeline look abandoned.
type Workspace struct {
	Directory                         string
	remote                            endpoint.Endpoint
	parentID, directoryID, markerHash string
	input                             *io.PipeWriter
	cancel                            context.CancelFunc
	done                              chan struct{}
	result                            error
	once                              sync.Once
	closeErr                          error
	record                            config.WorkspaceRecord
}

// A registrar durably saves the write-ahead intent before mkdir, then pins the
// actual inode before data transfer. Untracked use is limited to low-level
// callers; the GUI always supplies its vault transaction.
func NewWorkspace(ctx context.Context, remote endpoint.Endpoint, parent string, registrars ...func(config.WorkspaceRecord) error) (*Workspace, error) {
	w, err := createWorkspaceDirectory(ctx, remote, parent, registrars...)
	if err != nil {
		return nil, err
	}
	return w.startLease(ctx)
}

// Creation and journaling do not require the remote flock executable. Helpers
// use their own kernel lease once started; only system pipelines need an SSH
// shell to hold that lease between commands.
func createWorkspaceDirectory(ctx context.Context, remote endpoint.Endpoint, parent string, registrars ...func(config.WorkspaceRecord) error) (*Workspace, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(entropy[:])
	directory := path.Join(parent, ".dragfm-"+nonce)
	if !path.IsAbs(parent) || path.Clean(parent) != parent {
		return nil, errors.New("workspace parent must be absolute and clean")
	}
	var identity bytes.Buffer
	if err := remote.Exec(ctx, workspaceShell("set -e; cd -P -- "+quotePOSIX(parent)+"; command stat -Lc '%d:%i' -- .; id -u"), endpoint.ExecOptions{Stdout: &identity}); err != nil {
		return nil, err
	}
	fields := strings.Fields(identity.String())
	if len(fields) != 2 || !numericIdentity(fields[0], 2) {
		return nil, errors.New("invalid workspace parent identity")
	}
	uid, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return nil, err
	}
	owner := marker{Version: 2, Created: time.Now().UTC(), Nonce: nonce}
	data, _ := json.Marshal(owner)
	digest := sha256.Sum256(data)
	record := config.WorkspaceRecord{Path: directory, ParentID: fields[0], OwnerUID: uint32(uid), CreatedAt: owner.Created, MarkerSHA256: hex.EncodeToString(digest[:])}
	for _, register := range registrars {
		if err := register(record); err != nil {
			return nil, fmt.Errorf("record workspace intent before creation: %w", err)
		}
	}
	// Pin cwd before writing the marker; a changed parent/symlink must not
	// redirect a privileged absolute-path SFTP write. stdin contains only the
	// ownership marker, after any sudo frame has already been consumed.
	base := quotePOSIX(path.Base(directory))
	create := "set -e; set +x; export LC_ALL=C; umask 077; cd -P -- " + quotePOSIX(parent) + "; test \"$(command stat -Lc '%d:%i' -- .)\" = " + quotePOSIX(record.ParentID) + "; command mkdir -m 0700 -- " + base + `
directory_id=$(command stat -c '%d:%i:%u:%a' -- ` + base + `)
case "$directory_id" in *":$EUID:700") ;; *) exit 74;; esac
cd -P -- ` + base + `
test "$(command stat -Lc '%d:%i:%u:%a' -- .)" = "$directory_id"
set -C
command cat > .dragfm-owner-v1
printf '%s\n' "$directory_id"`
	var created bytes.Buffer
	if err := remote.Exec(ctx, workspaceShell(create), endpoint.ExecOptions{Stdin: bytes.NewReader(data), Stdout: &created}); err != nil {
		return nil, err
	}
	w, _, err := inspectWorkspace(ctx, remote, directory)
	if err != nil {
		// Do not erase a path whose ownership/identity could not be established.
		return nil, fmt.Errorf("inspect new workspace %q (retained): %w", directory, err)
	}
	if w.parentID != record.ParentID || w.markerHash != record.MarkerSHA256 || w.record.OwnerUID != record.OwnerUID || strings.TrimSpace(created.String()) != w.directoryID {
		return nil, errors.New("new workspace identity changed; intent retained")
	}
	for _, register := range registrars {
		if err := register(w.record); err != nil {
			return nil, fmt.Errorf("pin workspace inode before transfer (intent retained): %w", err)
		}
	}
	return w, nil
}

func (w *Workspace) startLease(ctx context.Context) (*Workspace, error) {
	// This lifetime is ended by Close, AFTER transfer processes stop, not by
	// parent cancellation (which could release the lock before writers stop).
	life, cancel := context.WithCancel(context.Background())
	w.cancel, w.done = cancel, make(chan struct{})
	reader, input := io.Pipe()
	w.input = input
	ready := &workspaceReady{done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer reader.Close()
		var diagnostic helperOutput
		w.result = w.remote.Exec(life, workspaceShell(w.lockScript(false)+"\nprintf 'ready\\n'\nIFS= read -r unused || :\n"), endpoint.ExecOptions{Stdin: reader, Stdout: ready, Stderr: &diagnostic})
		if w.result != nil && diagnostic.String() != "" {
			w.result = fmt.Errorf("%w: %s", w.result, diagnostic.String())
		}
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var err error
	select {
	case <-ready.done:
		return w, nil
	case <-w.done:
		err = errors.Join(errors.New("workspace lease exited before readiness"), w.result)
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = errors.New("workspace lease startup timed out")
	}
	return nil, errors.Join(err, w.Close(false))
}

func (w *Workspace) Record() config.WorkspaceRecord { return w.record }

// The caller has already verified the connected SSH identity. This only
// recovers the exact journaled directory, never every similarly named child.
// false/nil means retained (recent or leased); true includes already absent.
func RecoverWorkspace(ctx context.Context, remote endpoint.Endpoint, saved config.WorkspaceRecord, olderThan time.Duration) (bool, error) {
	if olderThan < time.Hour || saved.CreatedAt.IsZero() {
		return false, errors.New("invalid recovery age")
	}
	if time.Since(saved.CreatedAt) < olderThan {
		return false, nil
	}
	w, _, err := inspectWorkspaceAs(ctx, remote, saved.Path, &saved)
	if errors.Is(err, fs.ErrNotExist) {
		// A missing MARKER is not a missing directory: preserve unmarked
		// data, including an interrupted marker write-ahead intent.
		if _, statErr := remote.Stat(ctx, saved.Path); errors.Is(statErr, fs.ErrNotExist) {
			// The helper may have finished cleanup before the controller could
			// retire its record. Never lose an extant external partial merely
			// because its installation was removed or replaced independently.
			for _, partial := range saved.Partials {
				if !validPartialPath(partial.Path) {
					return false, errors.New("invalid saved partial path; recovery record retained")
				}
				if _, err := remote.Stat(ctx, partial.Path); !errors.Is(err, fs.ErrNotExist) {
					return false, errors.Join(errors.New("helper lease directory is absent but a partial is not confirmed absent; retained"), err)
				}
			}
			return true, nil
		}
	}
	if err != nil {
		return false, err
	}
	if saved.ParentID != w.parentID || saved.MarkerSHA256 != w.markerHash || saved.OwnerUID != w.record.OwnerUID || !saved.CreatedAt.Equal(w.record.CreatedAt) || (saved.DirectoryID != "" && saved.DirectoryID != w.directoryID) {
		return false, errors.New("workspace no longer matches its vault ownership record; retained")
	}
	w.record.Partials = append([]config.PartialRecord(nil), saved.Partials...)
	if err := w.remove(ctx); errors.Is(err, errWorkspaceBusy) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

type workspaceReady struct {
	done chan struct{}
	once sync.Once
	tail string
}

func (r *workspaceReady) Write(p []byte) (int, error) {
	r.tail += string(p)
	if strings.Contains(r.tail, "ready\n") {
		r.once.Do(func() { close(r.done) })
	}
	if len(r.tail) > 64 {
		r.tail = r.tail[len(r.tail)-64:]
	}
	return len(p), nil
}

func workspaceShell(script string) string {
	return "unset BASH_ENV ENV; exec bash --noprofile --norc -c " + quotePOSIX(script)
}

// The lock stays on fd 9 across exec and fork. Commands must not close it.
// The parent is pinned by cwd and checked before any relative operation.
func (w *Workspace) lockScript(exclusive bool) string {
	mode := "-s"
	if exclusive {
		mode = "-x"
	}
	base := quotePOSIX(path.Base(w.Directory))
	return "set +x; set +a; set -e; export LC_ALL=C\ncd -P -- " + quotePOSIX(path.Dir(w.Directory)) + "\n" +
		"test \"$(command stat -Lc '%d:%i' -- .)\" = " + quotePOSIX(w.parentID) + "\n" +
		"test ! -L " + base + "\ntest -d " + base + "\nexec 9< " + base + "\n" +
		"command flock " + mode + " -n -E 73 9\n" +
		"test \"$(command stat -Lc '%d:%i:%u:%a' -- /proc/self/fd/9)\" = " + quotePOSIX(w.directoryID) + "\n" +
		"test \"$(command stat -c '%d:%i:%u:%a' -- " + base + ")\" = " + quotePOSIX(w.directoryID) + "\n" +
		"test \"$(command stat -c '%u:%a:%F' -- " + base + "/" + markerName + ")\" = " + quotePOSIX(strconv.FormatUint(uint64(w.record.OwnerUID), 10)+":600:regular file") + "\n" +
		"test \"$(command sha256sum < " + base + "/" + markerName + ")\" = " + quotePOSIX(w.markerHash+"  -") + "\n"
}

func (w *Workspace) Command(command string) string {
	return workspaceShell(w.lockScript(false) + "\n" + command)
}

// CommandPrefix lets an authenticated native transport append an argv vector
// without interpolating paths into the lease script. The caller must quote
// each argument and unset BASH_ENV/ENV before starting the outer Bash.
func (w *Workspace) CommandPrefix() []string {
	return []string{"bash", "--noprofile", "--norc", "-c", w.lockScript(false) + "\nexec \"$@\"", "dragfm"}
}

func (w *Workspace) remove(ctx context.Context) error {
	return w.removePartials(ctx, w.remote.Exec, w.record.Partials)
}

// System SFTP can create root-owned partials while its lease directory belongs
// to the SSH uploader. Cleanup uses the already-approved executor; it does not
// change ownership of the directory or confer privilege on generic recovery.
func (w *Workspace) removePartials(ctx context.Context, execute func(context.Context, string, endpoint.ExecOptions) error, records []config.PartialRecord) error {
	// rm never receives an unvalidated absolute path or a wildcard. A symlink
	// replacing the candidate fails the stat identity check; the pinned parent
	// prevents ancestor replacement from redirecting removal elsewhere.
	partials, err := partialCleanupScript(records)
	if err != nil {
		return err
	}
	// The installation's EXCLUSIVE lease stays on fd 9 throughout the
	// external-partial checks/removals and final installation removal.
	err = execute(ctx, workspaceShell(w.lockScript(true)+"\n"+partials+"\ncommand rm -rf --one-file-system -- "+quotePOSIX(path.Base(w.Directory))), endpoint.ExecOptions{})
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.ExitStatus() == 73 {
		return errWorkspaceBusy
	}
	return err
}

var errWorkspaceBusy = errors.New("workspace is still leased")
var errUnownedWorkspace = errors.New("not an owned leased workspace")

// remove must be false when process exit is unconfirmed. Releasing the
// controller lease is safe: each surviving writer holds its inherited lock.
func (w *Workspace) Close(remove bool) error {
	w.once.Do(func() {
		_ = w.input.Close()
		select {
		case <-w.done:
		case <-time.After(3 * time.Second):
			w.cancel()
			<-w.done // Remote.Exec cancellation itself has a bounded exit wait.
		}
		w.cancel()
		w.closeErr = w.result
		if remove && w.result == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w.closeErr = w.remove(ctx)
		}
		if w.closeErr != nil {
			w.closeErr = fmt.Errorf("workspace %q retained: %w", w.Directory, w.closeErr)
		}
	})
	return w.closeErr
}

func inspectWorkspace(ctx context.Context, remote endpoint.Endpoint, directory string) (*Workspace, marker, error) {
	return inspectWorkspaceAs(ctx, remote, directory, nil)
}

// Root may inspect an uploader-owned helper installation ONLY for an exact
// approved recovery record. An unjournaled scan still requires its own UID.
// RecoverWorkspace compares the complete saved identity before any deletion.
func inspectWorkspaceAs(ctx context.Context, remote endpoint.Endpoint, directory string, saved *config.WorkspaceRecord) (*Workspace, marker, error) {
	var owner marker
	base := path.Base(directory)
	nonce := strings.TrimPrefix(base, ".dragfm-")
	decoded, err := hex.DecodeString(nonce)
	if !path.IsAbs(directory) || path.Clean(directory) != directory || base != ".dragfm-"+nonce || err != nil || len(decoded) != 16 {
		return nil, owner, fmt.Errorf("%w: invalid path", errUnownedWorkspace)
	}
	entry, err := remote.Stat(ctx, directory)
	if err != nil {
		return nil, owner, err
	}
	if !entry.IsDir() || entry.Mode.Perm() != 0700 {
		return nil, owner, fmt.Errorf("%w: directory is not private", errUnownedWorkspace)
	}
	markPath := path.Join(directory, markerName)
	markInfo, err := remote.Stat(ctx, markPath)
	if err != nil {
		return nil, owner, err
	}
	if !markInfo.Mode.IsRegular() || markInfo.Mode.Perm() != 0600 || !entry.OwnerKnown || !markInfo.OwnerKnown || entry.UID != markInfo.UID {
		return nil, owner, fmt.Errorf("%w: ownership marker is not private", errUnownedWorkspace)
	}
	file, err := remote.Open(ctx, markPath)
	if err != nil {
		return nil, owner, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	_ = file.Close()
	if err != nil {
		return nil, owner, err
	}
	if len(data) > 4096 || json.Unmarshal(data, &owner) != nil || owner.Version != 2 || owner.Nonce != nonce || owner.Created.IsZero() || owner.Created.After(time.Now().Add(time.Minute)) {
		return nil, owner, fmt.Errorf("%w: marker is legacy or invalid", errUnownedWorkspace)
	}
	var output bytes.Buffer
	script := "set -e; cd -P -- " + quotePOSIX(path.Dir(directory)) + "; command stat -Lc '%d:%i' -- .; command stat -c '%d:%i:%u:%a' -- " + quotePOSIX(base) + "; id -u"
	if err := remote.Exec(ctx, workspaceShell(script), endpoint.ExecOptions{Stdout: &output}); err != nil {
		return nil, owner, err
	}
	lines := strings.Fields(output.String())
	ownerUID := strconv.FormatUint(uint64(entry.UID), 10)
	if len(lines) != 3 {
		return nil, owner, fmt.Errorf("%w: invalid execution identity", errUnownedWorkspace)
	}
	approvedRoot := saved != nil && saved.Elevated && saved.Path == directory && saved.OwnerUID == entry.UID && lines[2] == "0"
	if lines[2] != ownerUID && !approvedRoot {
		return nil, owner, fmt.Errorf("%w: belongs to another execution identity", errUnownedWorkspace)
	}
	if !numericIdentity(lines[0], 2) || !numericIdentity(lines[1], 4) || !strings.HasSuffix(lines[1], ":"+ownerUID+":700") {
		return nil, owner, fmt.Errorf("%w: invalid filesystem identity", errUnownedWorkspace)
	}
	digest := sha256.Sum256(data)
	record := config.WorkspaceRecord{Path: directory, ParentID: lines[0], DirectoryID: lines[1], MarkerSHA256: hex.EncodeToString(digest[:]), CreatedAt: owner.Created, OwnerUID: entry.UID}
	return &Workspace{Directory: directory, remote: remote, parentID: lines[0], directoryID: lines[1], markerHash: record.MarkerSHA256, record: record}, owner, nil
}

func numericIdentity(value string, count int) bool {
	parts := strings.Split(value, ":")
	if len(parts) != count {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// Only this explicitly scoped directory is scanned. This does not search a
// remote filesystem, follow symlinks, acquire sudo, or infer dead PIDs. Legacy
// v1 workspaces are retained because they did not promise a writer lease.
func CleanupWorkspaces(ctx context.Context, remote endpoint.Endpoint, directory string, olderThan time.Duration) error {
	if olderThan < time.Hour {
		return errors.New("invalid stale workspace age")
	}
	entries, err := remote.List(ctx, directory)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name, ".dragfm-") {
			continue
		}
		w, owner, err := inspectWorkspace(ctx, remote, path.Join(directory, entry.Name))
		if err != nil {
			if !errors.Is(err, errUnownedWorkspace) && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
				failures = append(failures, fmt.Errorf("inspect stale workspace %q: %w", entry.Name, err))
			}
			continue
		} // malformed/legacy/foreign paths are not ours to erase.
		if time.Since(owner.Created) < olderThan {
			continue
		}
		if err := w.remove(ctx); err != nil && !errors.Is(err, errWorkspaceBusy) {
			failures = append(failures, fmt.Errorf("stale workspace %q: %w", w.Directory, err))
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}
