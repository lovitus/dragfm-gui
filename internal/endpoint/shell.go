package endpoint

// bashStartup runs the login files in the interactive process itself. A
// non-interactive `bash -lc 'exec bash -i'` loses aliases, functions and shell
// options, and takes the wrong branch of profiles that inspect $-.
// Bash ignores --rcfile in login mode, so (like VS Code's integration) we
// reproduce its documented profile order with --rcfile, not a second shell.
// This loads the login environment; it does not set Bash's readonly login_shell
// flag. In particular we do not pretend to run .bash_logout on exit.
func bashStartup(nonce string) string {
	return `if [ -r /etc/profile ]; then . /etc/profile; fi
for __dragfm_profile in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
  if [ -r "$__dragfm_profile" ]; then
    . "$__dragfm_profile"
    break
  fi
done
unset __dragfm_profile
` + bashCWDHook(nonce)
}

// zshStartup interposes only during startup. Each user file sees the user's
// ZDOTDIR (including changes made by an earlier file), and the final shell and
// its children never inherit the temporary wrapper directory.
func zshStartup(file, directory, nonce string) string {
	body := `if [[ $__DRAGFM_ZDOTDIR_SET = 1 ]]; then
  export ZDOTDIR=$__DRAGFM_ZDOTDIR
else
  unset ZDOTDIR
fi
` + "if [[ -r ${ZDOTDIR-$HOME}/" + file + " ]]; then source \"${ZDOTDIR-$HOME}/" + file + "\"; fi\n"
	if file == ".zlogin" {
		return body + "unset __DRAGFM_ZDOTDIR __DRAGFM_ZDOTDIR_SET\n" + zshCWDHook(nonce)
	}
	return body + `__DRAGFM_ZDOTDIR_SET=${+ZDOTDIR}
__DRAGFM_ZDOTDIR=${ZDOTDIR-}
` + "export ZDOTDIR=" + shellQuote(directory) + "\n"
}

// Base64 has the same no-argument encoder on Linux and macOS. Its line wraps
// remain inside the filtered frame (Go's decoder accepts CR/LF). No raw path
// bytes can terminate the OSC, and no platform-specific flags are needed.
func posixCWDHook(nonce string) string {
	return `__dragfm_emit_cwd(){ printf '\033]777;dragfm-cwd=v1;` + nonce + `;'; printf '%s' "$PWD" | command base64; printf '\007'; }
PS1='$(__dragfm_emit_cwd)'"${PS1-\$ }"
`
}

func bashCWDHook(nonce string) string {
	return `__dragfm_emit_cwd(){ local __dragfm_status=$?; builtin printf '\033]777;dragfm-cwd=v1;` + nonce + `;'; builtin printf '%s' "$PWD" | command base64; builtin printf '\007'; return "$__dragfm_status"; }
if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )); then
  PROMPT_COMMAND+=(__dragfm_emit_cwd)
else
  PROMPT_COMMAND="${PROMPT_COMMAND-}"$'\n''__dragfm_emit_cwd'
fi
`
}

func zshCWDHook(nonce string) string {
	return `__dragfm_emit_cwd(){ builtin printf '\033]777;dragfm-cwd=v1;` + nonce + `;'; builtin printf '%s' "$PWD" | command base64; builtin printf '\007'; }
autoload -Uz add-zsh-hook
add-zsh-hook precmd __dragfm_emit_cwd
`
}

func fishCWDHook(nonce string) string {
	return `function __dragfm_emit_cwd; set -l dragfm_status $status; printf '\033]777;dragfm-cwd=v1;` + nonce + `;'; printf '%s' "$PWD" | command base64; printf '\007'; return $dragfm_status; end; function __dragfm_prompt --on-event fish_prompt; __dragfm_emit_cwd; end`
}
