package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const uci = `
# 行首注释
config mywanip 'main'
    option enabled '1'
    option interface "pppoe-wan"
    option listen :9377
    list ignored 'something'
    option hashval 'aaa#bbb'   # 注意：UCI 不支持行内注释，# 后内容属于值

config other 'second'
    option x 'y'
`
	sections, err := Parse(strings.NewReader(uci))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	main, ok := sections["mywanip.main"]
	if !ok {
		t.Fatalf("missing section mywanip.main, got %v", sections)
	}
	if main["enabled"] != "1" {
		t.Errorf("enabled = %q, want 1", main["enabled"])
	}
	if main["interface"] != "pppoe-wan" {
		t.Errorf("interface = %q, want pppoe-wan", main["interface"])
	}
	if main["listen"] != ":9377" {
		t.Errorf("listen = %q, want :9377", main["listen"])
	}
	if !strings.Contains(main["hashval"], "aaa#bbb") {
		t.Errorf("hashval = %q, want it to contain aaa#bbb (hash inside quotes is part of value)", main["hashval"])
	}
	if _, exists := main["ignored"]; exists {
		t.Errorf("list key should not be stored as option")
	}
	// 类型键与命名键指向同一段
	if sections["mywanip"]["interface"] != "pppoe-wan" {
		t.Errorf("type-key section should mirror named section")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"unterminated quote", "config mywanip 'main\n"},
		{"option outside section", "option enabled '1'\n"},
		{"option missing value", "config mywanip\noption enabled\n"},
		{"unknown statement", "config mywanip\nfoobar x y\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(tc.src)); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestParseCRLF(t *testing.T) {
	src := "config mywanip 'main'\r\n    option enabled '1'\r\n"
	sections, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if sections["mywanip.main"]["enabled"] != "1" {
		t.Errorf("enabled = %q, want 1", sections["mywanip.main"]["enabled"])
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatalf("expected error for missing file")
		}
	})

	t.Run("no mywanip section uses defaults", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "mywanip")
		if err := os.WriteFile(p, []byte("config other 'x'\n    option a 'b'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Enabled {
			t.Errorf("Enabled = true, want false")
		}
		if cfg.Interface != DefaultInterface {
			t.Errorf("Interface = %q, want %q", cfg.Interface, DefaultInterface)
		}
		if cfg.Port != DefaultPort {
			t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
		}
		if !cfg.BindIPv4 || !cfg.BindIPv6 {
			t.Errorf("default binds should both be true: %+v", cfg)
		}
	})

	t.Run("empty options use defaults", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "mywanip")
		if err := os.WriteFile(p, []byte("config mywanip 'main'\n    option enabled '0'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Interface != DefaultInterface || cfg.Port != DefaultPort || !cfg.BindIPv4 || !cfg.BindIPv6 {
			t.Errorf("defaults not applied: %+v", cfg)
		}
	})
}

func TestLoadValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mywanip")
	src := "config mywanip 'main'\n" +
		"    option enabled '1'\n" +
		"    option interface 'wan1'\n" +
		"    option port '8080'\n" +
		"    option bind_ipv4 '1'\n" +
		"    option bind_ipv6 '0'\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Enabled || cfg.Interface != "wan1" || cfg.Port != 8080 || !cfg.BindIPv4 || cfg.BindIPv6 {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"bad enabled", "config mywanip 'main'\n    option enabled 'maybe'\n"},
		{"port not a number", "config mywanip 'main'\n    option port 'abc'\n"},
		{"port out of range", "config mywanip 'main'\n    option port '99999'\n"},
		{"both binds disabled", "config mywanip 'main'\n    option bind_ipv4 '0'\n    option bind_ipv6 '0'\n"},
		{"bad bind value", "config mywanip 'main'\n    option bind_ipv4 'maybe'\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "mywanip")
			if err := os.WriteFile(p, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
