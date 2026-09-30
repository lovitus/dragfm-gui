package agentservice

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/pkg/sftp"
)

// ServeFiles is a task-owned child of an authenticated helper installation.
// It never listens on a network port and never owns/removes that installation:
// the control helper outlives the file channel and owns partial-file cleanup.
// SSH provides authentication and encryption for this standard SFTP stream.
func ServeFiles(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	stream := &fileStream{ReadCloser: input, output: output}
	server, err := sftp.NewServer(stream)
	if err != nil {
		return err
	}
	defer server.Close()
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type fileStream struct {
	io.ReadCloser
	output io.Writer
}

func (s *fileStream) Write(p []byte) (int, error) { return s.output.Write(p) }

// pkg/sftp's standard server REALPATH is lexical filepath.Abs, not symlink
// resolution. Same-machine safety must resolve actual parent aliases under
// the privileged identity while preserving a final symlink as a copied entry.
func physicalParentPath(value string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", errors.New("physical path must be absolute")
	}
	directory := filepath.Dir(value)
	tail := []string{filepath.Base(value)}
	for {
		resolved, err := filepath.EvalSymlinks(directory)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || directory == filepath.Dir(directory) {
			return "", err
		}
		tail = append(tail, filepath.Base(directory))
		directory = filepath.Dir(directory)
	}
}
