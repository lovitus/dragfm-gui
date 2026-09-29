package webgui

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lovitus/dragfm-gui/internal/jobs"
)

// Command logs are plain text, not another terminal emulator. Publish complete
// records while the process is running. Stdout/stderr each own a decoder: an
// unrelated stderr write must not split a stdout password and defeat masking.
// Raw buffering is bounded by one record plus the longest configured secret.
type commandStream struct {
	mu         sync.Mutex
	secrets    []string
	pending    []byte
	line       []byte
	key        bool
	metadata   bool
	assignment string
	quote      byte
	escaped    bool
	omitted    bool
	closed     bool
	dst        io.Writer
}

var (
	keyStart         = regexp.MustCompile(`-----BEGIN (?:OPENSSH |RSA |EC |DSA |ENCRYPTED )?PRIVATE KEY-----`)
	keyEnd           = regexp.MustCompile(`-----END (?:OPENSSH |RSA |EC |DSA |ENCRYPTED )?PRIVATE KEY-----`)
	secretHeader     = regexp.MustCompile(`^###(?:sudo密码|root密码|口令|待核对旧密码)[ \t]*\r?\n$`)
	quotedCredential = regexp.MustCompile(`(?i)(?:\b(?:password|passwd|passphrase|sudo_password|root_password)\s*[:=]\s*|[[:alnum:]_.~%+-]+:)["']`)
	assignmentHeader = regexp.MustCompile(`(?i)\b(?:password|passwd|passphrase|sudo_password|root_password)\s*[:=]\s*$`)
)

func (s *commandStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	for len(p) > 0 && !s.omitted {
		length := min(len(p), 4096)
		s.pending = append(s.pending, p[:length]...)
		p = p[length:]
		s.consume(false)
	}
	return n, nil
}

// Choose the earliest full match OR a suffix that could become a secret when
// the next Write arrives. Prefer waiting over emitting an overlapping prefix.
// This handles multiline secrets without holding every short log line hostage
// to a fixed, maximum-secret-length lookbehind window.
func (s *commandStream) consume(final bool) {
	for len(s.pending) > 0 && !s.omitted {
		at, size, partial := len(s.pending), 0, false
		for _, secret := range s.secrets {
			if i := bytes.Index(s.pending, []byte(secret)); i >= 0 && (i < at || i == at && len(secret) > size) {
				at, size, partial = i, len(secret), false
			}
			start := max(0, len(s.pending)-len(secret)+1)
			for start < len(s.pending) {
				i := bytes.IndexByte(s.pending[start:], secret[0])
				if i < 0 {
					break
				}
				i += start
				if strings.HasPrefix(secret, string(s.pending[i:])) && i <= at {
					at, size, partial = i, len(s.pending)-i, true
					break
				}
				start = i + 1
			}
		}
		s.records(s.pending[:at])
		s.pending = s.pending[at:]
		if size == 0 {
			break
		}
		if partial && !final {
			break
		}
		s.records([]byte("***"))
		s.pending = s.pending[size:]
	}
	if s.omitted {
		s.pending = nil
	}
}

func (s *commandStream) records(p []byte) {
	for _, b := range p {
		if s.omitted {
			return
		}
		s.line = append(s.line, b)
		if b == '\n' {
			s.record(string(s.line))
			s.line = s.line[:0]
		} else if len(s.line) >= maxCommandOutput {
			// Never release a truncated record: it might contain a partial
			// quoted credential or the start of a private key. Keep draining
			// the process without logging the rest of this stream.
			_, _ = io.WriteString(s.dst, "\n[单行输出超过 64 KiB；此输出流后续内容已省略，命令继续执行]\n")
			s.line, s.omitted = nil, true
		}
	}
}

func (s *commandStream) record(line string) {
	if s.metadata {
		s.metadata = false
		return
	}
	if s.assignment != "" {
		line, s.assignment = s.assignment+line, ""
	}
	if !s.key && s.quote == 0 && assignmentHeader.MatchString(line) {
		// The existing assignment syntax permits whitespace/newlines after
		// '='. Keep that prefix until its value arrives, without growing on
		// arbitrary blank lines.
		s.assignment = strings.TrimRight(line, " \t\r\n")
		return
	}
	if !s.key && s.quote == 0 && secretHeader.MatchString(line) {
		s.metadata = true
		// Reserve the value's masked line in the same write as its header.
		// Otherwise concurrent stderr can land between them, and the final
		// snapshot redactor mistakes that public stderr line for the password.
		_, _ = io.WriteString(s.dst, line+"***\n")
		return
	}
	var safe strings.Builder
	for line != "" {
		if s.quote != 0 {
			end := s.closeQuote(line)
			if end < 0 {
				break
			}
			line = line[end:]
			continue
		}
		if s.key {
			end := keyEnd.FindStringIndex(line)
			if end == nil {
				break
			}
			line, s.key = line[end[1]:], false
			continue
		}
		key, quote := keyStart.FindStringIndex(line), quotedCredential.FindStringIndex(line)
		// Consume credential spans in order; a key-looking string inside a
		// quoted password must not leave both suppression states active.
		if key != nil && (quote == nil || key[0] < quote[0]) {
			safe.WriteString(line[:key[0]])
			safe.WriteString("[私钥已隐藏]")
			line, s.key = line[key[1]:], true
		} else if quote != nil {
			safe.WriteString(line[:quote[1]-1])
			safe.WriteString("***")
			s.quote, line = line[quote[1]-1], line[quote[1]:]
		} else {
			safe.WriteString(line)
			line = ""
		}
	}
	if (s.key || s.quote != 0) && safe.Len() > 0 {
		safe.WriteByte('\n')
	}
	if safe.Len() > 0 {
		_, _ = io.WriteString(s.dst, redact(safe.String()))
	}
}

// Return the byte after the closing quote; escaped quotes are not delimiters.
func (s *commandStream) closeQuote(value string) int {
	for i := 0; i < len(value); i++ {
		if s.escaped {
			s.escaped = false
			continue
		}
		if value[i] == '\\' {
			s.escaped = true
			continue
		}
		if value[i] == s.quote {
			s.quote = 0
			return i + 1
		}
	}
	return -1
}

func (s *commandStream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.consume(true)
	if len(s.line) > 0 {
		s.record(string(s.line))
	}
	s.pending, s.line, s.closed = nil, nil, true
	s.assignment = ""
}

// Coalesce fast writes, but flush a sparse write on a timer even if the process
// remains silent afterwards. The authoritative update is a replaceable bounded
// snapshot, so dropped UI events never create holes or duplicate output.
type liveCommandOutput struct {
	mu     sync.Mutex
	buffer commandOutput
	emit   func(jobs.Update)
	timer  *time.Timer
	closed bool
}

func (o *liveCommandOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := o.buffer.Write(p)
	if o.timer == nil {
		o.timer = time.AfterFunc(100*time.Millisecond, func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			if o.closed {
				return
			}
			o.publish()
			o.timer = nil
		})
	}
	return n, err
}

func (o *liveCommandOutput) publish() {
	o.emit(jobs.Update{Output: o.buffer.String(), Stage: "command", Message: "命令执行中", Indeterminate: true})
}

func (o *liveCommandOutput) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	if o.timer != nil {
		o.timer.Stop()
	}
	o.publish()
}

// Keep restart history useful without writing 500 full live transcripts into
// the vault. Only already-sanitized text reaches this function.
func historyOutput(value string) string {
	const limit = 16 * 1024
	if len(value) <= limit {
		return value
	}
	return "[历史仅保留最后 16 KiB]\n" + strings.ToValidUTF8(value[len(value)-limit:], "�")
}
