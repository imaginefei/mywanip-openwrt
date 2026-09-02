package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestMakeArStructure(t *testing.T) {
	ipk := makeAr([]fileEntry{
		{"debian-binary", []byte("2.0\n"), 0o644},
		{"control.tar.gz", []byte("fake-control"), 0o644},
		{"data.tar.gz", []byte("fake-data"), 0o644},
	})

	if !bytes.HasPrefix(ipk, []byte("!<arch>\n")) {
		t.Fatalf("missing ar magic header")
	}
	// 成员顺序固定
	body := string(ipk[len("!<arch>\n"):])
	pos := strings.Index(body, "debian-binary")
	ctrlPos := strings.Index(body, "control.tar.gz")
	dataPos := strings.Index(body, "data.tar.gz")
	if !(pos < ctrlPos && ctrlPos < dataPos) {
		t.Fatalf("ar member order wrong: debian=%d control=%d data=%d", pos, ctrlPos, dataPos)
	}
	// 每个头 60 字节，以 "`\n" 结尾
	header := body[pos : pos+60]
	if !strings.HasSuffix(header, "`\n") {
		t.Fatalf("ar header not terminated by backtick-newline: %q", header)
	}
	// GNU 约定：名字字段必须以 '/' 结尾（opkg/libarchive 靠它截断名字）
	if !strings.HasPrefix(header, "debian-binary/") {
		t.Fatalf("ar name field must be slash-terminated (GNU convention): %q", header[:16])
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

	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
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

	bin, ok := seen["usr/bin/mywanipd"]
	if !ok {
		t.Fatalf("usr/bin/mywanipd missing from tar")
	}
	if bin.Mode != 0o755 {
		t.Errorf("binary mode = %o, want 755", bin.Mode)
	}
	cfg, ok := seen["etc/config/mywanip"]
	if !ok {
		t.Fatalf("etc/config/mywanip missing from tar")
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

	ar1 := makeAr([]fileEntry{{"data.tar.gz", first, 0o644}})
	ar2 := makeAr([]fileEntry{{"data.tar.gz", second, 0o644}})
	if !bytes.Equal(ar1, ar2) {
		t.Fatalf("ar not deterministic across builds")
	}
}
