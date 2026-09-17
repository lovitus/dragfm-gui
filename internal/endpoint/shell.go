package endpoint

import (
	"path/filepath"
	"strings"
)

func posixCWDHook() string {
	return `__dragfm_emit_cwd(){ printf '\033]777;dragfm-cwd=%s\007' "$PWD"; }
PS1='$(__dragfm_emit_cwd)'"${PS1-\$ }"
`
}

func bashCWDHook() string {
	return `__dragfm_emit_cwd(){ printf '\033]777;dragfm-cwd=%s\007' "$PWD"; }
PROMPT_COMMAND="__dragfm_emit_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
`
}

func zshCWDHook() string {
	return `__dragfm_emit_cwd(){ printf '\033]777;dragfm-cwd=%s\007' "$PWD"; }
autoload -Uz add-zsh-hook
add-zsh-hook precmd __dragfm_emit_cwd
`
}

func fishCWDHook() string {
	return `function __dragfm_emit_cwd; printf '\033]777;dragfm-cwd=%s\007' $PWD; end; function __dragfm_prompt --on-event fish_prompt; __dragfm_emit_cwd; end`
}

func cwdBootstrap(shell string) string {
	if strings.EqualFold(filepath.Base(strings.TrimSpace(shell)), "fish") {
		return fishCWDHook() + "; stty echo; __dragfm_emit_cwd\n"
	}
	return `__dragfm_emit_cwd(){ printf '\033]777;dragfm-cwd=%s\007' "$PWD"; }; ` +
		`if [ -n "${ZSH_VERSION-}" ]; then autoload -Uz add-zsh-hook; add-zsh-hook precmd __dragfm_emit_cwd; ` +
		`elif [ -n "${BASH_VERSION-}" ]; then PROMPT_COMMAND="__dragfm_emit_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"; ` +
		`else PS1='$(__dragfm_emit_cwd)'"${PS1-\$ }"; fi; ` +
		`stty echo; __dragfm_emit_cwd` + "\n"
}
