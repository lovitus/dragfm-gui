package routespec

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/flyssh/flyssh/pkg/cli"
	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
)

type KeyLookup func(string) (*config.PrivateKey, bool)

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
			return nil, fmt.Errorf("暂不支持路由选项 %q", argument)
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
			return nil, fmt.Errorf("第 %d 跳: %w", index+1, err)
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
	parsed, err := url.Parse("socks5://" + value)
	if err != nil || parsed.Host == "" {
		return connector.SOCKS5{}, fmt.Errorf("SOCKS 格式应为 user:pass@ip:port: %w", err)
	}
	result := connector.SOCKS5{Address: parsed.Host}
	if parsed.User != nil {
		result.Username = parsed.User.Username()
		result.Password, _ = parsed.User.Password()
	}
	if !strings.Contains(parsed.Host, ":") {
		return connector.SOCKS5{}, errors.New("SOCKS 地址缺少端口")
	}
	return result, nil
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
