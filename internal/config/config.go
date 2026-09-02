// Package config 解析 mywanipd 的 UCI 配置文件（/etc/config/mywanip）。
//
// 只实现 UCI 语法的所需子集：
//   - config <type> ['<name>']
//   - option <key> <value>
//   - list   <key> <value>   （解析但本程序不用）
//   - 空行与行首 '#' 注释；值可用单/双引号或不加引号
//
// 注意：UCI 不支持行内注释，引号内的 '#' 是值的一部分。
package config

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	// ConfigType 是 UCI 配置中的 section 类型名。
	ConfigType = "mywanip"
	// DefaultInterface 是未配置时读取的接口名（OpenWrt 上 PPPoE 拨号设备）。
	DefaultInterface = "pppoe-wan"
	// DefaultListen 是未配置时的监听地址；host 留空在 Go 里绑定通配 [::]，
	// Linux 默认双栈，一个 socket 同时接受 IPv4/IPv6 连接。
	DefaultListen = ":9377"
)

// Config 是 mywanipd 的运行配置。
type Config struct {
	// Enabled 为 false 时守护进程不启动服务（OpenWrt 惯例：装包默认不启用）。
	Enabled bool
	// Interface 是要读取地址的网络接口名，如 pppoe-wan。
	Interface string
	// Listen 是 HTTP 监听地址，host:port 形式（如 :9377、0.0.0.0:8080、[::1]:9377）。
	Listen string
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Enabled:   false,
		Interface: DefaultInterface,
		Listen:    DefaultListen,
	}
}

// Validate 校验配置字段合法性。
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Interface) == "" {
		return fmt.Errorf("interface must not be empty")
	}
	host, portStr, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen %q (use host:port, e.g. :9377 or [::]:9377; IPv6 address must be bracketed): %w", c.Listen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid listen %q: port must be 1-65535", c.Listen)
	}
	// host 为空（通配）或合法 IP 均可；net.ParseIP 对空串返回 nil，需单独放行。
	if host != "" && net.ParseIP(host) == nil {
		return fmt.Errorf("invalid listen %q: host must be an IP address or empty", c.Listen)
	}
	return nil
}

// Load 从 path 读取 UCI 配置并填充 Config（缺项用默认值）。
// 文件不存在返回错误；文件存在但没有 config mywanip 段时返回默认配置。
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	sections, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg := Default()
	opts, ok := sections[ConfigType]
	if !ok {
		// 没有 mywanip 段：容忍，用默认值（调用方负责打日志）。
		return cfg, nil
	}

	if v, present := opts["enabled"]; present {
		b, err := parseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid option enabled=%q: %w", v, err)
		}
		cfg.Enabled = b
	}
	if v, present := opts["interface"]; present && strings.TrimSpace(v) != "" {
		cfg.Interface = v
	}
	if v, present := opts["listen"]; present && strings.TrimSpace(v) != "" {
		cfg.Listen = v
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Parse 解析 UCI 流，返回 map[sectionKey]map[option]value。
// sectionKey 为 "<type>"（该类型的第一个段）或 "<type>.<name>"。
func Parse(r io.Reader) (map[string]map[string]string, error) {
	sections := make(map[string]map[string]string)
	var current map[string]string

	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields, err := tokenize(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "config":
			if len(fields) < 2 {
				return nil, fmt.Errorf("line %d: config requires a type", lineNo)
			}
			typ := fields[1]
			opts := make(map[string]string)
			if _, exists := sections[typ]; !exists {
				sections[typ] = opts // 同类型多段时保留第一个
			} else {
				opts = sections[typ]
			}
			if len(fields) >= 3 {
				sections[typ+"."+fields[2]] = opts
			}
			current = opts
		case "option":
			if current == nil {
				return nil, fmt.Errorf("line %d: option outside of a section", lineNo)
			}
			if len(fields) < 2 {
				return nil, fmt.Errorf("line %d: option requires a key", lineNo)
			}
			if len(fields) < 3 {
				return nil, fmt.Errorf("line %d: option %q requires a value", lineNo, fields[1])
			}
			current[fields[1]] = fields[2]
		case "list":
			// list 语法本程序不使用，识别但忽略值。
			if current == nil {
				return nil, fmt.Errorf("line %d: list outside of a section", lineNo)
			}
			if len(fields) < 3 {
				return nil, fmt.Errorf("line %d: list requires a key and a value", lineNo)
			}
		default:
			return nil, fmt.Errorf("line %d: unknown statement %q", lineNo, fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

// tokenize 按 UCI 规则切分一行：空白分隔，单/双引号包裹的值作为一个 token
// （引号内空白保留、引号本身去除）。
func tokenize(line string) ([]string, error) {
	var fields []string
	var sb strings.Builder
	var inQuote byte
	started := false

	flush := func() {
		if started {
			fields = append(fields, sb.String())
			sb.Reset()
			started = false
		}
	}

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			} else {
				sb.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			inQuote = ch
			started = true
		case ch == ' ' || ch == '\t':
			flush()
		default:
			sb.WriteByte(ch)
			started = true
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote (%c)", inQuote)
	}
	flush()
	return fields, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("expected 0/1 (or true/false), got %q", v)
	}
}
