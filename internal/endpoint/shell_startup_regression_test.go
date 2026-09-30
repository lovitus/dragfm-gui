//go:build !windows

package endpoint

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the actual startup files through both PTY implementations. Exported
// variables alone cannot detect the old non-interactive login + exec bug.
func TestPTYRetainsInteractiveProfileState(t *testing.T) {
	for _, transport := range []string{"local", "ssh"} {
		for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
			t.Run(transport+"/"+filepath.Base(shell), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				write := func(name, body string) {
					t.Helper()
					if err := os.WriteFile(name, []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
				}
				command := ""
				var want []string
				if shell == "/bin/bash" {
					write(filepath.Join(home, ".bash_profile"), `case $- in *i*) DRAGFM_PROFILE_MODE=interactive;; *) DRAGFM_PROFILE_MODE=noninteractive;; esac
DRAGFM_PROFILE_LOCAL=retained
dragfm_profile_function(){ printf '\nFUNCTION=%s\n' retained; }
alias dragfm_profile_alias='printf "\nALIAS=%s\n" retained'
shopt -s extglob
. "$HOME/.bashrc"
`)
					write(filepath.Join(home, ".bashrc"), `DRAGFM_RC_COUNT=$((${DRAGFM_RC_COUNT:-0}+1))
PS1='profile-check> '
if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )); then
  PROMPT_COMMAND=('DRAGFM_PROMPT_STATUS=$?' 'DRAGFM_SECOND_PROMPT=retained')
else
  PROMPT_COMMAND='DRAGFM_PROMPT_STATUS=$?; DRAGFM_SECOND_PROMPT=retained'
fi
`)
					// Emit our own record boundary: Readline's bracketed-paste
					// mode legitimately places CSI + CR before command output.
					command = "false\nprintf '\\nENV=%s|%s|%s|%s|%s\\n' \"$DRAGFM_PROFILE_MODE\" \"$DRAGFM_PROFILE_LOCAL\" \"$DRAGFM_RC_COUNT\" \"$DRAGFM_PROMPT_STATUS\" \"$DRAGFM_SECOND_PROMPT\"\ndragfm_profile_function\ndragfm_profile_alias\nshopt -q extglob && printf '\\nOPTION=%s\\n' retained\n"
					want = []string{"ENV=interactive|retained|1|1|retained", "FUNCTION=retained", "ALIAS=retained", "OPTION=retained"}
				} else {
					original, redirected := filepath.Join(home, "original"), filepath.Join(home, "redirected")
					for _, directory := range []string{original, redirected} {
						if err := os.Mkdir(directory, 0700); err != nil {
							t.Fatal(err)
						}
					}
					t.Setenv("ZDOTDIR", original)
					// The disposable user's startup fixture does not request
					// Ubuntu's global completion scan. This is its documented
					// opt-out, not compinit -u or approval of insecure files.
					write(filepath.Join(original, ".zshenv"), "skip_global_compinit=1\nexport ZDOTDIR="+shellQuote(redirected)+"\n")
					write(filepath.Join(redirected, ".zprofile"), "DRAGFM_PROFILE_LOCAL=retained\n")
					write(filepath.Join(redirected, ".zshrc"), "DRAGFM_RC_COUNT=$((${DRAGFM_RC_COUNT:-0}+1))\nPROMPT='profile-check> '\n")
					write(filepath.Join(redirected, ".zlogin"), "DRAGFM_LOGIN_LOCAL=retained\n")
					command = "printf '\\nENV=%s|%s|%s\\n' \"$DRAGFM_PROFILE_LOCAL\" \"$DRAGFM_LOGIN_LOCAL\" \"$DRAGFM_RC_COUNT\"\nprintf '\\nZDOT=%s\\n' \"$ZDOTDIR\"\n"
					want = []string{"ENV=retained|retained|1", "ZDOT=" + redirected}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var provider PTYProvider = NewLocal()
				if transport == "ssh" {
					_, route := startIntegrationSSHServer(t, true)
					remote, err := DialSSH(ctx, "startup-fixture", "", route)
					if err != nil {
						t.Fatal(err)
					}
					defer remote.Close()
					provider = remote
				}
				session, err := provider.OpenPTY(ctx, home, shell, 24, 160)
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				output := collectPTYProbe(t, ctx, session, command)
				for _, value := range want {
					if !strings.Contains(output, "\r\n"+value+"\r\n") {
						t.Fatalf("missing %q in actual %s %s PTY: %q", value, transport, shell, output)
					}
				}
			})
		}
	}
}

func collectPTYProbe(t *testing.T, ctx context.Context, session PTYSession, command string) string {
	t.Helper()
	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var output []byte
		buffer := make([]byte, 4096)
		sent := false
		for len(output) < 128<<10 {
			count, err := session.Output().Read(buffer)
			output = append(output, buffer[:count]...)
			start := bytes.Index(output, []byte("\x1b]777;dragfm-cwd="))
			if !sent && start >= 0 && bytes.IndexByte(output[start:], 7) >= 0 {
				sent = true
				if _, err := io.WriteString(session.Input(), command+"printf '\\n__probe_%s__\\n' end\nexit\n"); err != nil {
					done <- result{string(output), err}
					return
				}
			}
			if bytes.Contains(output, []byte("\r\n__probe_end__\r\n")) || err != nil {
				done <- result{string(output), err}
				return
			}
		}
		done <- result{string(output), io.ErrShortBuffer}
	}()
	select {
	case result := <-done:
		if !strings.Contains(result.output, "\r\n__probe_end__\r\n") {
			t.Fatalf("PTY probe incomplete: %v; %q", result.err, result.output)
		}
		return result.output
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ""
	}
}
