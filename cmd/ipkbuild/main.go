// ipkbuild 是纯 Go 的 ipk 打包工具：不依赖 Docker/SDK，
// 在 macOS 上可直接生成 opkg 可安装的 .ipk 文件。
//
// OpenWrt 24.10（iStoreOS）的 opkg 使用 gzip+tar 格式的 ipk
// （经典 ipkg 格式，与官方仓库一致），而非 Debian 的 ar 格式——
// ar 格式在该 opkg 上会报 "Malformed package file"。
// 结构：gzip(tar)，外层 tar 含三个成员（文件名带 ./ 前缀）：
//
//	./debian-binary   内容 "2.0\n"
//	./data.tar.gz     安装到目标系统的文件树
//	./control.tar.gz  control / conffiles / prerm
//
// 产物确定性：tar 头统一 uid/gid=0、root、零时间戳、文件名排序；
// gzip 头不携带额外时间信息。
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fileEntry 是要打进 tar 的一个文件。
type fileEntry struct {
	name string // 包内路径，无前导 '/'
	data []byte
	mode int64 // 权限位，如 0o755 / 0o644
}

// target 是 GOARCH 到 OpenWrt/opkg 架构名的映射。
type target struct {
	goarch   string
	opkgArch string
}

var targets = []target{
	{"amd64", "x86_64"},
	{"arm64", "aarch64_generic"},
	{"mips", "mips_24kc"},
	{"mipsle", "mipsel_24kc"},
	{"arm", "arm_cortex-a7"},
}

const (
	pkgDaemon  = "mywanipd"
	pkgLuCI    = "luci-app-mywanip"
	pkgRelease = "1" // ipk release 号
)

func main() {
	distDir := flag.String("dist", "dist", "directory with cross-compiled binaries (dist/<goarch>/mywanipd)")
	deployDir := flag.String("deploy", "deploy/openwrt", "deploy directory with OpenWrt integration files")
	outDir := flag.String("out", "release", "output directory for .ipk files")
	version := flag.String("version", "", "package version (default: git describe, fallback dev)")
	maintainer := flag.String("maintainer", "mywanipd", "Maintainer field in control")
	flag.Parse()

	ver := *version
	if ver == "" {
		ver = gitVersion()
	}
	ver = strings.TrimPrefix(ver, "v")
	log.Printf("ipkbuild: version %s", ver)

	out := filepath.Join(*outDir, ver)
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}

	// 1) mywanipd：每个架构一个包
	for _, t := range targets {
		dataFiles, err := daemonDataFiles(*distDir, *deployDir, t.goarch)
		if err != nil {
			log.Fatalf("daemon data for %s: %v", t.opkgArch, err)
		}
		controlFiles := daemonControlFiles(ver, t.opkgArch, *maintainer, dataFiles)

		path := filepath.Join(out, fmt.Sprintf("%s_%s-%s_%s.ipk", pkgDaemon, ver, pkgRelease, t.opkgArch))
		if err := writeIPK(path, controlFiles, dataFiles); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		log.Printf("built %s", path)
	}

	// 2) luci-app-mywanip：架构无关，一个包
	luciData, err := luciDataFiles(*deployDir)
	if err != nil {
		log.Fatalf("luci data: %v", err)
	}
	luciControl := luciControlFiles(ver, *maintainer, luciData)
	path := filepath.Join(out, fmt.Sprintf("%s_%s-%s_all.ipk", pkgLuCI, ver, pkgRelease))
	if err := writeIPK(path, luciControl, luciData); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	log.Printf("built %s", path)
}

// ---------- 文件清单组装 ----------

func daemonDataFiles(distDir, deployDir, goarch string) ([]fileEntry, error) {
	binPath := filepath.Join(distDir, goarch, pkgDaemon)
	bin, err := os.ReadFile(binPath)
	if err != nil {
		return nil, fmt.Errorf("read binary %s (run scripts/build.sh first): %w", binPath, err)
	}
	initScript, err := os.ReadFile(filepath.Join(deployDir, pkgDaemon, "files", "mywanipd.init"))
	if err != nil {
		return nil, err
	}
	uciDefault, err := os.ReadFile(filepath.Join(deployDir, pkgDaemon, "files", "mywanipd.config"))
	if err != nil {
		return nil, err
	}

	return []fileEntry{
		{"usr/bin/mywanipd", bin, 0o755},
		{"etc/init.d/mywanipd", initScript, 0o755},
		{"etc/config/mywanip", uciDefault, 0o644},
	}, nil
}

func daemonControlFiles(version, arch, maintainer string, data []fileEntry) []fileEntry {
	control := strings.Join([]string{
		"Package: " + pkgDaemon,
		"Version: " + version + "-" + pkgRelease,
		"Architecture: " + arch,
		"Maintainer: " + maintainer,
		"Section: net",
		"Priority: optional",
		"Depends: libc",
		"Installed-Size: " + strconv.Itoa(totalSize(data)/1024+1),
		"Description: Expose the WAN interface (pppoe-wan) IPv4/IPv6 addresses via HTTP",
		"",
	}, "\n")

	prerm := "#!/bin/sh\n" +
		"[ -x /etc/init.d/mywanipd ] && /etc/init.d/mywanipd stop\n" +
		"exit 0\n"

	return []fileEntry{
		{"control", []byte(control), 0o644},
		{"conffiles", []byte("/etc/config/mywanip\n"), 0o644},
		{"prerm", []byte(prerm), 0o755},
	}
}

// luciDataFiles 遍历 files/ 目录生成运行时文件树
// （usr/share/luci/menu.d、usr/share/rpcd/acl.d、www/luci-static/...）。
func luciDataFiles(deployDir string) ([]fileEntry, error) {
	root := filepath.Join(deployDir, pkgLuCI, "files")
	var files []fileEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 跳过 macOS/Finder 垃圾文件（.DS_Store 等）和任何隐藏文件
		if strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, fileEntry{filepath.ToSlash(rel), data, 0o644})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no LuCI files found under %s", root)
	}
	return files, nil
}

func luciControlFiles(version, maintainer string, data []fileEntry) []fileEntry {
	control := strings.Join([]string{
		"Package: " + pkgLuCI,
		"Version: " + version + "-" + pkgRelease,
		"Architecture: all",
		"Maintainer: " + maintainer,
		"Section: luci",
		"Priority: optional",
		"Depends: luci-base, " + pkgDaemon,
		"Installed-Size: " + strconv.Itoa(totalSize(data)/1024+1),
		"Description: LuCI configuration page for mywanipd (Services menu)",
		"",
	}, "\n")
	return []fileEntry{{"control", []byte(control), 0o644}}
}

func totalSize(files []fileEntry) int {
	n := 0
	for _, f := range files {
		n += len(f.data)
	}
	return n
}

// ---------- tar.gz / ar 写入 ----------

// makeTarGz 把文件列表打成确定性 tar.gz：零时间戳、root 属主、按名排序。
// tar 成员名统一加 "./" 前缀（与 OpenWrt 官方 ipk 一致）。
func makeTarGz(files []fileEntry) ([]byte, error) {
	sorted := make([]fileEntry, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, f := range sorted {
		name := f.name
		if !strings.HasPrefix(name, "./") {
			name = "./" + name
		}
		hdr := &tar.Header{
			Name:    name,
			Mode:    f.mode,
			Size:    int64(len(f.data)),
			ModTime: time.Time{}, // 零时间戳，保证产物可复现
			Uid:     0,
			Gid:     0,
			Uname:   "root",
			Gname:   "root",
			Format:  tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeIPK 生成 gzip+tar 格式 ipk（OpenWrt 24.10 opkg 支持的格式）：
// 外层 gzip(tar) 含 ./debian-binary、./data.tar.gz、./control.tar.gz。
func writeIPK(path string, controlFiles, dataFiles []fileEntry) error {
	controlTar, err := makeTarGz(controlFiles)
	if err != nil {
		return err
	}
	dataTar, err := makeTarGz(dataFiles)
	if err != nil {
		return err
	}
	outer := []fileEntry{
		{"debian-binary", []byte("2.0\n"), 0o644},
		{"data.tar.gz", dataTar, 0o644},
		{"control.tar.gz", controlTar, 0o644},
	}
	ipk, err := makeTarGz(outer)
	if err != nil {
		return err
	}
	return os.WriteFile(path, ipk, 0o644)
}

func gitVersion() string {
	if out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	return "dev"
}
