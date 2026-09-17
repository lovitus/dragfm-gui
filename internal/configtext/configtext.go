package configtext

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/lovitus/dragfm-gui/internal/config"
)

// Markdown is the single, user-facing configuration format. It intentionally
// contains no blank lines: Markdown headings name the three collections and
// each level-two heading names one entry.
func Markdown(document config.Document) string {
	lines := []string{"#主机"}
	for _, host := range document.Hosts {
		lines = append(lines, "##"+host.Name)
		spec := strings.TrimSpace(host.RouteSpec)
		if host.Shell != "" {
			spec += "," + host.Shell
		}
		lines = append(lines, spec)
		if host.SudoPassword != "" {
			lines = append(lines, "###sudo密码", host.SudoPassword)
		}
		if host.RootUser != "" {
			lines = append(lines, "###root用户", host.RootUser)
		}
		if host.RootPassword != "" {
			lines = append(lines, "###root密码", host.RootPassword)
		}
		if host.DefaultSOCKSID != "" {
			name := host.DefaultSOCKSID
			if proxy := document.SOCKSByID(host.DefaultSOCKSID); proxy != nil {
				name = proxy.Name
			}
			lines = append(lines, "###默认socks", name)
		}
		if host.Disabled {
			lines = append(lines, "###禁用", "true")
		}
	}
	lines = append(lines, "#私钥")
	for _, key := range document.Keys {
		lines = append(lines, "##"+key.Name)
		if key.Passphrase != "" {
			lines = append(lines, "###口令", key.Passphrase, "###私钥")
		}
		lines = append(lines, strings.Split(strings.TrimSpace(key.PEM), "\n")...)
	}
	lines = append(lines, "#socks池")
	for _, proxy := range document.SOCKS {
		lines = append(lines, "##"+proxy.Name, strings.TrimSpace(proxy.Spec))
		if proxy.Disabled {
			lines = append(lines, "###禁用", "true")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

type markdownSection int

const (
	sectionNone markdownSection = iota
	sectionHosts
	sectionKeys
	sectionSOCKS
)

// ParseMarkdown reads the compact heading-based format rendered by Markdown.
// Empty lines are ignored so pasting the user's naturally spaced Markdown is
// accepted, while the next render always removes those empty lines.
func ParseMarkdown(text string, old config.Document) ([]config.Host, []config.PrivateKey, []config.SOCKSProxy, error) {
	var hosts []config.Host
	var keys []config.PrivateKey
	var proxies []config.SOCKSProxy
	section := sectionNone
	sectionSeen := map[markdownSection]int{}
	seenNames := map[markdownSection]map[string]int{
		sectionHosts: {}, sectionKeys: {}, sectionSOCKS: {},
	}
	type pendingEntry struct {
		section markdownSection
		name    string
		line    int
		body    []string
	}
	var pending *pendingEntry

	flush := func() error {
		if pending == nil {
			return nil
		}
		body := make([]string, 0, len(pending.body))
		for _, line := range pending.body {
			if strings.TrimSpace(line) != "" {
				body = append(body, line)
			}
		}
		if len(body) == 0 {
			return fmt.Errorf("第 %d 行：%q 缺少配置内容", pending.line, pending.name)
		}
		primary, metadata, err := splitMarkdownEntry(body)
		if err != nil {
			return fmt.Errorf("第 %d 行：%q: %w", pending.line, pending.name, err)
		}
		switch pending.section {
		case sectionHosts:
			if len(primary) != 1 {
				return fmt.Errorf("第 %d 行：主机 %q 的 FlySSH 路由必须独占一行", pending.line, pending.name)
			}
			if err := requireMetadata(metadata, "sudo密码", "root用户", "root密码", "默认socks", "禁用"); err != nil {
				return fmt.Errorf("第 %d 行：主机 %q: %w", pending.line, pending.name, err)
			}
			spec, shell := splitHostShell(strings.TrimSpace(primary[0]))
			if spec == "" {
				return fmt.Errorf("第 %d 行：主机 %q 的 FlySSH 路由为空", pending.line, pending.name)
			}
			host := config.Host{ID: fmt.Sprintf("host-line-%d", pending.line), Name: pending.name, RouteSpec: spec, Shell: shell}
			if existing, ok := old.HostByName(pending.name); ok {
				host.ID = existing.ID
				host.SudoPassword, host.RootUser, host.RootPassword = existing.SudoPassword, existing.RootUser, existing.RootPassword
				host.LastDirectory, host.Disabled = existing.LastDirectory, existing.Disabled
				if existing.RouteSpec == spec {
					host.HopFingerprints = append([]string(nil), existing.HopFingerprints...)
					host.HopPasswords = append([]string(nil), existing.HopPasswords...)
				}
			}
			if value, ok := metadataValue(metadata, "sudo密码"); ok {
				host.SudoPassword = value
			}
			if value, ok := metadataValue(metadata, "root用户"); ok {
				host.RootUser = value
			}
			if value, ok := metadataValue(metadata, "root密码"); ok {
				host.RootPassword = value
			}
			if value, ok := metadataValue(metadata, "默认socks"); ok {
				// SaveConfigTexts resolves this user-facing name to a stable ID.
				host.DefaultSOCKSID = value
			}
			if value, ok := metadataValue(metadata, "禁用"); ok {
				host.Disabled, err = parseMarkdownBool(value)
				if err != nil {
					return fmt.Errorf("第 %d 行：主机 %q 的禁用状态: %w", pending.line, pending.name, err)
				}
			}
			hosts = append(hosts, host)
		case sectionKeys:
			if err := requireMetadata(metadata, "口令", "私钥"); err != nil {
				return fmt.Errorf("第 %d 行：私钥 %q: %w", pending.line, pending.name, err)
			}
			pemLines := primary
			if explicit, ok := metadata["私钥"]; ok {
				if len(primary) != 0 {
					return fmt.Errorf("第 %d 行：私钥 %q 使用 ###私钥 后，## 名称下不能再有未标记正文", pending.line, pending.name)
				}
				pemLines = explicit
			}
			pem := strings.TrimSpace(strings.Join(pemLines, "\n"))
			if pem == "" {
				return fmt.Errorf("第 %d 行：私钥 %q 的正文为空", pending.line, pending.name)
			}
			key := config.PrivateKey{ID: fmt.Sprintf("key-line-%d", pending.line), Name: pending.name, PEM: pem + "\n"}
			for _, existing := range old.Keys {
				if existing.Name == pending.name {
					key.ID, key.Passphrase, key.Fingerprint = existing.ID, existing.Passphrase, existing.Fingerprint
					break
				}
			}
			if value, ok := metadataValue(metadata, "口令"); ok {
				key.Passphrase = value
			}
			keys = append(keys, key)
		case sectionSOCKS:
			if err := requireMetadata(metadata, "禁用"); err != nil {
				return fmt.Errorf("第 %d 行：SOCKS %q: %w", pending.line, pending.name, err)
			}
			if len(primary) != 1 {
				return fmt.Errorf("第 %d 行：SOCKS %q 必须独占一行", pending.line, pending.name)
			}
			spec := strings.TrimSpace(primary[0])
			proxy := config.SOCKSProxy{ID: fmt.Sprintf("socks-line-%d", pending.line), Name: pending.name, Spec: spec}
			for _, existing := range old.SOCKS {
				if existing.Name == pending.name {
					proxy.ID, proxy.LastRTT, proxy.LastSuccess, proxy.Disabled = existing.ID, existing.LastRTT, existing.LastSuccess, existing.Disabled
					break
				}
			}
			if value, ok := metadataValue(metadata, "禁用"); ok {
				proxy.Disabled, err = parseMarkdownBool(value)
				if err != nil {
					return fmt.Errorf("第 %d 行：SOCKS %q 的禁用状态: %w", pending.line, pending.name, err)
				}
			}
			proxies = append(proxies, proxy)
		}
		pending = nil
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index, original := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(original)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "###") {
			if pending == nil {
				return nil, nil, nil, fmt.Errorf("第 %d 行：### 字段必须写在 ## 条目下", lineNumber)
			}
			pending.body = append(pending.body, trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			if err := flush(); err != nil {
				return nil, nil, nil, err
			}
			if section == sectionNone {
				return nil, nil, nil, fmt.Errorf("第 %d 行：条目必须写在 #主机、#私钥 或 #socks池 下", lineNumber)
			}
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			if name == "" {
				return nil, nil, nil, fmt.Errorf("第 %d 行：## 后必须填写名称", lineNumber)
			}
			if previous := seenNames[section][name]; previous != 0 {
				return nil, nil, nil, fmt.Errorf("第 %d 行：%q 与第 %d 行重名", lineNumber, name, previous)
			}
			seenNames[section][name] = lineNumber
			pending = &pendingEntry{section: section, name: name, line: lineNumber}
			continue
		}
		if trimmed == "#主机" || trimmed == "#私钥" || trimmed == "#socks池" {
			if err := flush(); err != nil {
				return nil, nil, nil, err
			}
			switch trimmed {
			case "#主机":
				section = sectionHosts
			case "#私钥":
				section = sectionKeys
			case "#socks池":
				section = sectionSOCKS
			}
			if previous := sectionSeen[section]; previous != 0 {
				return nil, nil, nil, fmt.Errorf("第 %d 行：该分区已在第 %d 行出现", lineNumber, previous)
			}
			sectionSeen[section] = lineNumber
			continue
		}
		if pending == nil {
			return nil, nil, nil, fmt.Errorf("第 %d 行：配置内容前必须先写 ##名称", lineNumber)
		}
		pending.body = append(pending.body, original)
	}
	if err := flush(); err != nil {
		return nil, nil, nil, err
	}
	for required, title := range map[markdownSection]string{
		sectionHosts: "#主机", sectionKeys: "#私钥", sectionSOCKS: "#socks池",
	} {
		if sectionSeen[required] == 0 {
			return nil, nil, nil, fmt.Errorf("缺少必需分区 %s", title)
		}
	}
	return hosts, keys, proxies, nil
}

func splitMarkdownEntry(lines []string) ([]string, map[string][]string, error) {
	var primary []string
	metadata := make(map[string][]string)
	current := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "###") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "###"))
			if name == "" {
				return nil, nil, errors.New("### 后必须填写字段名")
			}
			if _, exists := metadata[name]; exists {
				return nil, nil, fmt.Errorf("字段 %q 重复", name)
			}
			metadata[name] = nil
			current = name
			continue
		}
		if current == "" {
			primary = append(primary, line)
		} else {
			metadata[current] = append(metadata[current], line)
		}
	}
	for name, values := range metadata {
		if len(values) == 0 {
			return nil, nil, fmt.Errorf("字段 %q 缺少内容", name)
		}
	}
	return primary, metadata, nil
}

func requireMetadata(metadata map[string][]string, allowed ...string) error {
	valid := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		valid[name] = true
	}
	for name := range metadata {
		if !valid[name] {
			return fmt.Errorf("未知字段 %q", name)
		}
	}
	return nil
}

func metadataValue(metadata map[string][]string, name string) (string, bool) {
	values, ok := metadata[name]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(strings.Join(values, "\n")), true
}

func parseMarkdownBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "是":
		return true, nil
	case "false", "no", "0", "否":
		return false, nil
	default:
		return false, fmt.Errorf("应为 true/false，实际为 %q", value)
	}
}

func splitHostShell(value string) (string, string) {
	comma := strings.LastIndex(value, ",")
	if comma < 0 {
		return strings.TrimSpace(value), ""
	}
	candidate := strings.TrimSpace(value[comma+1:])
	if !strings.HasPrefix(candidate, "/") || strings.ContainsAny(candidate, " \t") {
		return strings.TrimSpace(value), ""
	}
	return strings.TrimSpace(value[:comma]), candidate
}

// SSHRoutes renders exactly one FlySSH route per physical line. A route is a
// GUI endpoint; whether it contains one hop or many is deliberately opaque to
// this text format.
func SSHRoutes(document config.Document) string {
	lines := []string{
		"# 每个物理行就是一台 SSH 主机；直接填写一跳或多跳 FlySSH 路由。",
		"# 脱敏示例（保留 # 时不会连接）：",
		`# uhome:"EXAMPLE_PASSWORD_2"@192.0.2.10:41122  uhome:"EXAMPLE_PASSWORD_3"@192.0.2.20:41122  uhome@192.0.2.30:41122 --keys ",,prikeyname1"`,
		"# --keys 的逗号与跳数对齐；prikeyname1 是 vault 中的 key 名。",
	}
	for _, host := range document.Hosts {
		if host.RouteSpec == "" {
			continue
		}
		prefix := ""
		if host.Disabled {
			prefix = "disabled "
		}
		lines = append(lines, prefix+host.RouteSpec)
	}
	return strings.Join(lines, "\n") + "\n"
}

func ParseSSHRoutes(text string, old config.Document) ([]config.Host, error) {
	var hosts []config.Host
	seen := make(map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for line := 1; scanner.Scan(); line++ {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		disabled := false
		if strings.HasPrefix(raw, "disabled ") {
			disabled, raw = true, strings.TrimSpace(strings.TrimPrefix(raw, "disabled "))
		}
		// Read the previous combined format, but never render it again.
		legacyName := ""
		if strings.HasPrefix(raw, "ssh ") {
			if equal := strings.Index(raw, " = "); equal > len("ssh ") {
				legacyName = strings.TrimSpace(raw[len("ssh "):equal])
				raw = strings.TrimSpace(raw[equal+3:])
			}
		}
		if raw == "" {
			return nil, fmt.Errorf("第 %d 行：SSH 路由不能为空", line)
		}
		if previous := seen[raw]; previous != 0 {
			return nil, fmt.Errorf("第 %d 行：与第 %d 行的 SSH 路由重复", line, previous)
		}
		seen[raw] = line
		host := config.Host{ID: fmt.Sprintf("host-line-%d", line), Name: legacyName, RouteSpec: raw, Disabled: disabled}
		for _, existing := range old.Hosts {
			if existing.RouteSpec == raw || (legacyName != "" && existing.Name == legacyName) {
				host.ID, host.Name = existing.ID, existing.Name
				host.Shell, host.SudoPassword = existing.Shell, existing.SudoPassword
				host.RootUser, host.RootPassword = existing.RootUser, existing.RootPassword
				host.LastDirectory = existing.LastDirectory
				if existing.RouteSpec == raw {
					host.HopFingerprints = append([]string(nil), existing.HopFingerprints...)
					host.HopPasswords = append([]string(nil), existing.HopPasswords...)
				}
				break
			}
		}
		hosts = append(hosts, host)
	}
	return hosts, scanner.Err()
}

func SOCKSList(document config.Document) string {
	lines := []string{
		"# 每个物理行是一条 SOCKS5 代理，直接填写 user:pass@ip:port。",
		"# 脱敏示例（保留 # 时不会使用）：",
		"# user:pass@ip:port",
	}
	for _, proxy := range document.SOCKS {
		if proxy.Spec == "" {
			continue
		}
		prefix := ""
		if proxy.Disabled {
			prefix = "disabled "
		}
		lines = append(lines, prefix+proxy.Spec)
	}
	return strings.Join(lines, "\n") + "\n"
}

func ParseSOCKSList(text string, old config.Document) ([]config.SOCKSProxy, error) {
	var proxies []config.SOCKSProxy
	seen := make(map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for line := 1; scanner.Scan(); line++ {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		disabled := false
		if strings.HasPrefix(raw, "disabled ") {
			disabled, raw = true, strings.TrimSpace(strings.TrimPrefix(raw, "disabled "))
		}
		legacyName := ""
		if strings.HasPrefix(raw, "socks ") {
			if equal := strings.Index(raw, " = "); equal > len("socks ") {
				legacyName = strings.TrimSpace(raw[len("socks "):equal])
				raw = strings.TrimSpace(raw[equal+3:])
			}
		}
		if raw == "" {
			return nil, fmt.Errorf("第 %d 行：SOCKS 配置不能为空", line)
		}
		if previous := seen[raw]; previous != 0 {
			return nil, fmt.Errorf("第 %d 行：与第 %d 行的 SOCKS 配置重复", line, previous)
		}
		seen[raw] = line
		proxy := config.SOCKSProxy{ID: fmt.Sprintf("socks-line-%d", line), Name: legacyName, Spec: raw, Disabled: disabled}
		for _, existing := range old.SOCKS {
			if existing.Spec == raw || (legacyName != "" && existing.Name == legacyName) {
				proxy.ID, proxy.Name, proxy.LastRTT = existing.ID, existing.Name, existing.LastRTT
				break
			}
		}
		proxies = append(proxies, proxy)
	}
	return proxies, scanner.Err()
}

// Connections is deliberately line-oriented: every visible line is one
// independently editable connection and long FlySSH routes never soft-wrap.
func Connections(document config.Document) string {
	var lines []string
	lines = append(lines,
		"# 每个物理行是一条连接；长路由不会软换行。SSH 主机也可作为已知跳板。",
		"# 下面是可直接照着修改的脱敏示例；保留行首 # 时不会成为真实配置。",
		`# ssh 深层主机 = uhome:"EXAMPLE_PASSWORD_2"@192.0.2.10:41122  uhome:"EXAMPLE_PASSWORD_3"@192.0.2.20:41122  uhome@192.0.2.30:41122 --keys ",,prikeyname1"`,
		"# socks 示例代理 = user:pass@ip:port",
		"# disabled ssh 暂停使用 = user@192.0.2.10:22",
		"# --keys 的逗号位置与跳数对齐；prikeyname1 是保险库 key 名，不是文件路径。",
	)
	for _, host := range document.Hosts {
		prefix := ""
		if host.Disabled {
			prefix = "disabled "
		}
		lines = append(lines, prefix+"ssh "+host.Name+" = "+host.RouteSpec)
	}
	for _, proxy := range document.SOCKS {
		prefix := ""
		if proxy.Disabled {
			prefix = "disabled "
		}
		lines = append(lines, prefix+"socks "+proxy.Name+" = "+proxy.Spec)
	}
	return strings.Join(lines, "\n") + "\n"
}

func ParseConnections(text string, old config.Document) ([]config.Host, []config.SOCKSProxy, error) {
	hosts := make([]config.Host, 0)
	proxies := make([]config.SOCKSProxy, 0)
	seen := make(map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for line := 1; scanner.Scan(); line++ {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		disabled := false
		if strings.HasPrefix(raw, "disabled ") {
			disabled, raw = true, strings.TrimSpace(strings.TrimPrefix(raw, "disabled "))
		}
		kindEnd := strings.IndexByte(raw, ' ')
		equal := strings.Index(raw, " = ")
		if kindEnd < 0 || equal < 0 || equal <= kindEnd+1 {
			return nil, nil, fmt.Errorf("第 %d 行：应为 ssh 名称 = 路由，或 socks 名称 = 配置", line)
		}
		kind := strings.TrimSpace(raw[:kindEnd])
		name := strings.TrimSpace(raw[kindEnd+1 : equal])
		spec := strings.TrimSpace(raw[equal+3:])
		if name == "" || spec == "" {
			return nil, nil, fmt.Errorf("第 %d 行：名称和配置均不能为空", line)
		}
		key := kind + "\x00" + name
		if previous := seen[key]; previous != 0 {
			return nil, nil, fmt.Errorf("第 %d 行：%s %q 与第 %d 行重复", line, kind, name, previous)
		}
		seen[key] = line
		switch kind {
		case "ssh":
			host := config.Host{Name: name, RouteSpec: spec, Disabled: disabled}
			if existing, ok := old.HostByName(name); ok {
				host.ID = existing.ID
				host.Shell = existing.Shell
				host.SudoPassword = existing.SudoPassword
				host.RootUser = existing.RootUser
				host.RootPassword = existing.RootPassword
				host.LastDirectory = existing.LastDirectory
				if existing.RouteSpec == spec {
					host.HopFingerprints = append([]string(nil), existing.HopFingerprints...)
					host.HopPasswords = append([]string(nil), existing.HopPasswords...)
				}
			}
			if host.ID == "" {
				host.ID = fmt.Sprintf("host-line-%d", line)
			}
			hosts = append(hosts, host)
		case "socks":
			proxy := config.SOCKSProxy{Name: name, Spec: spec, Disabled: disabled}
			for _, existing := range old.SOCKS {
				if existing.Name == name {
					proxy.ID, proxy.LastRTT = existing.ID, existing.LastRTT
					break
				}
			}
			if proxy.ID == "" {
				proxy.ID = fmt.Sprintf("socks-line-%d", line)
			}
			proxies = append(proxies, proxy)
		default:
			return nil, nil, fmt.Errorf("第 %d 行：未知配置类型 %q", line, kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return hosts, proxies, nil
}

func Keys(document config.Document) string {
	var output strings.Builder
	output.WriteString("# 每个 key 块保存一把保险库私钥；endkey 必须独占一行。\n")
	output.WriteString("# 脱敏示例（需要使用时删除每行开头的 # 和一个空格）：\n")
	output.WriteString("# key prikeyname1\n# passphrase 可选的私钥口令\n# -----BEGIN OPENSSH PRIVATE KEY-----\n# ...\n# -----END OPENSSH PRIVATE KEY-----\n# endkey\n")
	for _, key := range document.Keys {
		fmt.Fprintf(&output, "key %s\n", key.Name)
		if key.Passphrase != "" {
			fmt.Fprintf(&output, "passphrase %s\n", key.Passphrase)
		}
		output.WriteString(strings.TrimRight(key.PEM, "\n"))
		output.WriteString("\nendkey\n\n")
	}
	return output.String()
}

func ParseKeys(text string, old config.Document) ([]config.PrivateKey, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var result []config.PrivateKey
	seen := make(map[string]int)
	for index := 0; index < len(lines); {
		raw := strings.TrimSpace(lines[index])
		line := index + 1
		if raw == "" || strings.HasPrefix(raw, "#") {
			index++
			continue
		}
		if !strings.HasPrefix(raw, "key ") || strings.TrimSpace(strings.TrimPrefix(raw, "key ")) == "" {
			return nil, fmt.Errorf("第 %d 行：应以 key 名称 开始私钥块", line)
		}
		name := strings.TrimSpace(strings.TrimPrefix(raw, "key "))
		if previous := seen[name]; previous != 0 {
			return nil, fmt.Errorf("第 %d 行：私钥 %q 与第 %d 行重复", line, name, previous)
		}
		seen[name] = line
		index++
		passphrase := ""
		if index < len(lines) && strings.HasPrefix(lines[index], "passphrase ") {
			passphrase = strings.TrimPrefix(lines[index], "passphrase ")
			index++
		}
		start := index
		for index < len(lines) && strings.TrimSpace(lines[index]) != "endkey" {
			index++
		}
		if index >= len(lines) {
			return nil, fmt.Errorf("第 %d 行：私钥 %q 缺少 endkey", line, name)
		}
		pem := strings.TrimSpace(strings.Join(lines[start:index], "\n"))
		if pem == "" {
			return nil, fmt.Errorf("第 %d 行：私钥 %q 的 PEM 正文为空", line, name)
		}
		key := config.PrivateKey{Name: name, PEM: pem + "\n", Passphrase: passphrase}
		for _, existing := range old.Keys {
			if existing.Name == name {
				key.ID, key.Fingerprint = existing.ID, existing.Fingerprint
				break
			}
		}
		if key.ID == "" {
			key.ID = fmt.Sprintf("key-line-%d", line)
		}
		result = append(result, key)
		index++
	}
	return result, nil
}
