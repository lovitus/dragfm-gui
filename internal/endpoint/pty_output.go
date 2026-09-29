package endpoint

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// FilterCWDMarkers removes private OSC frames and accepts only this session's
// versioned notifications. Keep the decoder shared by the maintained web GUI
// and retained Fyne entry point so neither interprets an encoded path as text.
// A nonce prevents ordinary printed files from spoofing prompt readiness, not
// code already executing with access to the shell's own startup script.
func FilterCWDMarkers(source io.Reader, nonce string, update func(string)) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		prefix := []byte("\x1b]777;dragfm-cwd=")
		buffer := make([]byte, 32*1024)
		pending := make([]byte, 0, 32*1024)
		for {
			count, err := source.Read(buffer)
			if count > 0 {
				pending = append(pending, buffer[:count]...)
				for {
					start := bytes.Index(pending, prefix)
					if start < 0 {
						keep := matchingPrefixSuffix(pending, prefix)
						if len(pending) > keep {
							if _, writeErr := writer.Write(pending[:len(pending)-keep]); writeErr != nil {
								return
							}
							pending = append([]byte(nil), pending[len(pending)-keep:]...)
						}
						break
					}
					if start > 0 {
						if _, writeErr := writer.Write(pending[:start]); writeErr != nil {
							return
						}
						pending = pending[start:]
					}
					payload := pending[len(prefix):]
					end := bytes.IndexByte(payload, 7)
					// Recover from a printed, unterminated private OSC before the
					// real shell prompt. Encoded paths cannot contain ESC bytes.
					nested := bytes.Index(payload, prefix)
					if nested >= 0 && (end < 0 || nested < end) {
						pending = pending[len(prefix)+nested:]
						continue
					}
					if end < 0 {
						if len(pending) > 64*1024 {
							// Bound memory even for hostile/incomplete output, retaining
							// only a possible beginning of the next private frame.
							keep := matchingPrefixSuffix(pending, prefix)
							pending = append([]byte(nil), pending[len(pending)-keep:]...)
						}
						break
					}
					if nonce != "" {
						if encoded, ok := strings.CutPrefix(string(payload[:end]), "v1;"+nonce+";"); ok {
							decoded, decodeErr := base64.StdEncoding.Strict().DecodeString(encoded)
							directory := string(decoded)
							if decodeErr == nil && !strings.ContainsRune(directory, 0) && (path.IsAbs(directory) || filepath.IsAbs(directory)) {
								update(directory)
							}
						}
					}
					pending = pending[len(prefix)+end+1:]
				}
			}
			if err != nil {
				// An incomplete private frame is not visible shell output.
				if len(pending) > 0 && !bytes.HasPrefix(pending, prefix) {
					if _, writeErr := writer.Write(pending); writeErr != nil {
						return
					}
				}
				if !errors.Is(err, io.EOF) {
					_ = writer.CloseWithError(err)
				}
				return
			}
		}
	}()
	return reader
}

// Retaining only genuine partial prefixes avoids the previous delayed prompt
// tail bug: a complete prompt must not wait for another keystroke to flush.
func matchingPrefixSuffix(data, prefix []byte) int {
	for size := min(len(data), len(prefix)-1); size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], prefix[:size]) {
			return size
		}
	}
	return 0
}
