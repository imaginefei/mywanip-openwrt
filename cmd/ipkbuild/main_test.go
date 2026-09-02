package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteIPKStructure 验证 ipk 为 gzip+tar 格式（OpenWrt opkg 实际支持的格式），
// 外层 tar 含三个必需成员，debian-binary 内容为 "2.0"。
func TestWriteIPKStructure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ipk")
	control := []fileEntry{{"control", []byte("Package: x\n"), 0o644}}
	data := []fileEntry{{"usr/bin/x", []byte("bin"), 0o755}}
	if err := writeIPK(path, control, data); err != nil {
		t.Fatalf("writeIPK: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// gzip 魔数 1f 8b
	if raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatalf("ipk is not gzip: first bytes %x %x", raw[0], raw[1])
	}

	entries := readTarGz(t, raw)
	if string(entries["./debian-binary"]) != "2.0\n" {
		t.Errorf("debian-binary = %q, want 2.0\\n", entries["./debian-binary"])
	}
	if _, ok := entries["./control.tar.gz"]; !ok {
		t.Errorf("outer tar missing ./control.tar.gz")
	}
	if _, ok := entries["./data.tar.gz"]; !ok {
		t.Errorf("outer tar missing ./data.tar.gz")
	}

	// 内层 tar 文件名同样带 ./ 前缀
	innerControl := readTarGz(t, entries["./control.tar.gz"])
	if _, ok := innerControl["./control"]; !ok {
		t.Errorf("control.tar.gz missing ./control, got %v", keys(innerControl))
	}
	innerData := readTarGz(t, entries["./data.tar.gz"])
	bin, ok := innerData["./usr/bin/x"]
	if !ok {
		t.Fatalf("data.tar.gz missing ./usr/bin/x")
	}
	if string(bin) != "bin" {
		t.Errorf("binary content mismatch")
	}
}

func TestMakeTarGzEntries(t *testing.T) {
	files := []fileEntry{
		{"usr/bin/mywanipd", []byte("binary-bytes"), 0o755},
		{"etc/config/mywanip", []byte("config"), 0o644},
	}
	raw, err := makeTarGz(files)
	if err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(mustGzip(t, raw))
	seen := map[string]*tar.Header{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		seen[hdr.Name] = hdr
		if hdr.Uid != 0 || hdr.Gid != 0 || hdr.Uname != "root" {
			t.Errorf("%s: expected root/0 ownership, got uid=%d gid=%d uname=%s", hdr.Name, hdr.Uid, hdr.Gid, hdr.Uname)
		}
		if hdr.ModTime.Unix() != 0 {
			t.Errorf("%s: ModTime = %v, want epoch zero", hdr.Name, hdr.ModTime)
		}
	}

	bin, ok := seen["./usr/bin/mywanipd"]
	if !ok {
		t.Fatalf("./usr/bin/mywanipd missing from tar")
	}
	if bin.Mode != 0o755 {
		t.Errorf("binary mode = %o, want 755", bin.Mode)
	}
	cfg, ok := seen["./etc/config/mywanip"]
	if !ok {
		t.Fatalf("./etc/config/mywanip missing from tar")
	}
	if cfg.Mode != 0o644 {
		t.Errorf("config mode = %o, want 644", cfg.Mode)
	}
}

func TestDeterministic(t *testing.T) {
	files := []fileEntry{
		{"b.txt", []byte("second"), 0o644},
		{"a.txt", []byte("first"), 0o644},
	}
	first, err := makeTarGz(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := makeTarGz(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("tar.gz not deterministic across builds")
	}
}

// ---- 测试辅助 ----

func readTarGz(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(tr); err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = buf.Bytes()
	}
	return out
}

func mustGzip(t *testing.T, raw []byte) *gzip.Reader {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return gz
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
