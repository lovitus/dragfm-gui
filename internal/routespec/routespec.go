package routespec

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/flyssh/flyssh/pkg/cli"
	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
)

type KeyLookup func(string) (*config.PrivateKey, bool)

// MatchingHostPrefix compares server addresses along the route, not passwords
// or key-slot names. Editing a login credential must not erase an existing
// server pin and silently turn the next connection back into TOFU.
func MatchingHostPrefix(before, after string) int {
	identityOnly := func(string) (*config.PrivateKey, bool) { return &config.PrivateKey{}, true }
	left, err := ParseSSH(before, identityOnly)
	if err != nil {
		return 0
	}
	right, err := ParseSSH(after, identityOnly)
	if err != nil {
		return 0
	}
	count := 0
	for count < len(left) && count < len(right) && left[count].Host == right[count].Host && left[count].Port == right[count].Port {
		count++
	}
	return count
}

func ParseSSH(value string, lookup KeyLookup) ([]connector.Hop, error) {
	arguments, err := splitArguments(value)
	if err != nil {
		return nil, err
	}
	var hopValues []string
	var keySlots []string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--keys":
			if index+1 >= len(arguments) {
				return nil, errors.New("--keys 缺少参数")
			}
			index++
			keySlots = strings.Split(unquote(arguments[index]), ",")
		case strings.HasPrefix(argument, "--keys="):
			keySlots = strings.Split(unquote(strings.TrimPrefix(argument, "--keys=")), ",")
		case strings.HasPrefix(argument, "-"):
			return nil, errors.New("路由仅支持 --keys 选项")
		default:
			hopValues = append(hopValues, argument)
		}
	}
	if len(hopValues) == 0 {
		return nil, errors.New("SSH 路由至少需要一个 user@host")
	}
	if len(keySlots) > len(hopValues) {
		for _, extra := range keySlots[len(hopValues):] {
			if strings.TrimSpace(extra) != "" {
				return nil, errors.New("--keys 的位置数超过 SSH 跳数")
			}
		}
	}
	hops := make([]connector.Hop, 0, len(hopValues))
	for index, raw := range hopValues {
		parsed, err := cli.ParseHopSpec(raw)
		if err != nil {
			// Upstream errors may contain the complete, not-yet-saved secret.
			return nil, fmt.Errorf("第 %d 跳: 应为 user[:password]@host[:port]", index+1)
		}
		credentials := connector.Credentials{UseAgent: true, Password: parsed.Password}
		if index < len(keySlots) {
			name := strings.TrimSpace(keySlots[index])
			if name != "" {
				if lookup == nil {
					return nil, fmt.Errorf("第 %d 跳指定了私钥 %q，但没有保险库", index+1, name)
				}
				key, ok := lookup(name)
				if !ok {
					return nil, fmt.Errorf("第 %d 跳的保险库私钥 %q 不存在", index+1, name)
				}
				credentials.PrivateKeys = []connector.PrivateKey{{PEM: []byte(key.PEM), Passphrase: []byte(key.Passphrase)}}
			}
		}
		hops = append(hops, connector.Hop{Host: parsed.Host, Port: parsed.Port, User: parsed.User, Credentials: credentials})
	}
	return hops, nil
}

func ParseSOCKS(value string) (connector.SOCKS5, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return connector.SOCKS5{}, errors.New("SOCKS 配置不能为空")
	}
	badFormat := errors.New("SOCKS 格式应为 user:pass@host:port；IPv6 地址需写为 [地址]:端口")
	result := connector.SOCKS5{Address: value}
	if strings.HasPrefix(value, "socks5://") {
		// Explicit URLs retain the percent-escaped syntax used by v1 vaults.
		// Never return url.Error: its URL field includes unsaved credentials.
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
			return connector.SOCKS5{}, badFormat
		}
		result.Address = parsed.Host
		if parsed.User != nil {
			result.Username = parsed.User.Username()
			result.Password, _ = parsed.User.Password()
		}
	} else if at := strings.LastIndexByte(value, '@'); at >= 0 {
		credentials := value[:at]
		user, password, ok := strings.Cut(credentials, ":")
		if !ok || user == "" {
			return connector.SOCKS5{}, badFormat
		}
		result.Username, result.Password, result.Address = user, password, value[at+1:]
	}
	host, port, err := net.SplitHostPort(result.Address)
	if err != nil || host == "" || strings.ContainsAny(host, " /?#@\t\r\n") {
		return connector.SOCKS5{}, badFormat
	}
	for _, char := range port {
		if char < '0' || char > '9' {
			return connector.SOCKS5{}, badFormat
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return connector.SOCKS5{}, errors.New("SOCKS 端口必须在 1–65535 范围内")
	}
	result.Address = net.JoinHostPort(host, strconv.Itoa(number))
	return result, nil
}

// WithPasswords replaces only named hop passwords, keeping --keys positions
// and all other hop text intact. This makes saved credentials visible and
// removable in the user's one-line FlySSH route instead of a hidden overlay.
func WithPasswords(value string, replacements map[int]string) (string, error) {
	arguments, err := splitArguments(value)
	if err != nil {
		return "", err
	}
	hop := 0
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--keys" {
			index++
			if index >= len(arguments) {
				return "", errors.New("--keys 缺少参数")
			}
			continue
		}
		if strings.HasPrefix(argument, "--keys=") {
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return "", errors.New("路由仅支持 --keys 选项")
		}
		if password, ok := replacements[hop]; ok {
			parsed, err := cli.ParseHopSpec(argument)
			if err != nil {
				return "", fmt.Errorf("第 %d 跳: 应为 user[:password]@host[:port]", hop+1)
			}
			user := parsed.User
			if strings.HasPrefix(user, "-") || strings.IndexFunc(user, func(char rune) bool {
				return !unicode.IsLetter(char) && !unicode.IsDigit(char) && !strings.ContainsRune("_.-", char)
			}) >= 0 {
				user = quoteCredential(user)
			}
			arguments[index] = user + ":" + quoteCredential(password) + "@" + net.JoinHostPort(parsed.Host, strconv.Itoa(parsed.Port))
		}
		hop++
	}
	for index := range replacements {
		if index < 0 || index >= hop {
			return "", errors.New("密码对应的 SSH 跳点不存在")
		}
	}
	return strings.Join(arguments, " "), nil
}

func quoteCredential(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `'`, `\'`).Replace(value) + `"`
}

func splitArguments(value string) ([]string, error) {
	var result []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			result = append(result, current.String())
			current.Reset()
		}
	}
	for _, char := range value {
		if escaped {
			current.WriteRune('\\')
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(char)
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			current.WriteRune(char)
			continue
		}
		if unicode.IsSpace(char) {
			flush()
			continue
		}
		current.WriteRune(char)
	}
	if escaped {
		return nil, errors.New("路由末尾不能是反斜线")
	}
	if quote != 0 {
		return nil, errors.New("路由中存在未闭合的引号")
	}
	flush()
	return result, nil
}

func unquote(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}
