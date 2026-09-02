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
// 产物确定性：tar 头统一 uid/gid=0、root、文件时间戳取当前 git commit
// 时间（同一 commit 产物字节一致，文件日期又不显示为 1970）、文件名排序。
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
	// 文件时间戳用当前 commit 时间（可复现：同 commit 字节一致；
	// 又不会显示为 1970）。取不到 git 时退回零时间戳。
	modTime := time.Time{}
	if epoch, err := gitCommitEpoch(); err == nil {
		modTime = time.Unix(epoch, 0)
	}
	log.Printf("ipkbuild: version %s (file mtime %s)", ver, modTime.Format("2006-01-02"))

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
		if err := writeIPK(path, controlFiles, dataFiles, modTime); err != nil {
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
	if err := writeIPK(path, luciControl, luciData, modTime); err != nil {
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

	// stop 的输出/错误重定向：服务从未启动过时 procd 会回
	// "ubus call service delete ... Not found"，属于无害噪音。
	prerm := "#!/bin/sh\n" +
		"[ -x /etc/init.d/mywanipd ] && /etc/init.d/mywanipd stop >/dev/null 2>&1\n" +
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

// ---------- tar.gz 写入 ----------

// makeTarGz 把文件列表打成 tar.gz：root 属主、按名排序、统一 modTime。
// modTime 取当前 git commit 时间——同一 commit 产物字节一致（可复现），
// 文件日期又不会显示为 1970。tar 成员名统一加 "./" 前缀（与 OpenWrt
// 官方 ipk 一致）。必须为每个父目录写入目录条目——opkg 解压时不会自动
// 创建目录，缺失目录项会导致 wfopen: ... No such file or directory。
func makeTarGz(files []fileEntry, modTime time.Time) ([]byte, error) {
	sorted := make([]fileEntry, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 收集全部父目录（去重、排序），先写目录条目
	dirSet := make(map[string]bool)
	var dirs []string
	for _, f := range sorted {
		name := "./" + f.name
		for _, d := range parentDirs(name) {
			if !dirSet[d] {
				dirSet[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if err := writeTarHeader(tw, d, 0o755, tar.TypeDir, nil, modTime); err != nil {
			return nil, err
		}
	}

	for _, f := range sorted {
		if err := writeTarHeader(tw, "./"+f.name, f.mode, tar.TypeReg, f.data, modTime); err != nil {
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

// parentDirs 返回 "./a/b/c" 形式路径的全部父目录（含 "./"），
// 结果均以 '/' 结尾："./"、"./a/"、"./a/b/"。
func parentDirs(name string) []string {
	parts := strings.Split(name, "/") // 形如 [".", "www", ..., "file.js"]
	var dirs []string
	cur := parts[0]
	dirs = append(dirs, cur+"/") // "./"
	for i := 1; i < len(parts)-1; i++ {
		cur += "/" + parts[i]
		dirs = append(dirs, cur+"/")
	}
	return dirs
}

func writeTarHeader(tw *tar.Writer, name string, mode int64, typ byte, data []byte, modTime time.Time) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     mode,
		Size:     int64(len(data)),
		ModTime:  modTime,
		Uid:      0,
		Gid:      0,
		Uname:    "root",
		Gname:    "root",
		Typeflag: typ,
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if len(data) > 0 {
		_, err := tw.Write(data)
		return err
	}
	return nil
}

// writeIPK 生成 gzip+tar 格式 ipk（OpenWrt 24.10 opkg 支持的格式）：
// 外层 gzip(tar) 含 ./debian-binary、./data.tar.gz、./control.tar.gz。
func writeIPK(path string, controlFiles, dataFiles []fileEntry, modTime time.Time) error {
	controlTar, err := makeTarGz(controlFiles, modTime)
	if err != nil {
		return err
	}
	dataTar, err := makeTarGz(dataFiles, modTime)
	if err != nil {
		return err
	}
	outer := []fileEntry{
		{"debian-binary", []byte("2.0\n"), 0o644},
		{"data.tar.gz", dataTar, 0o644},
		{"control.tar.gz", controlTar, 0o644},
	}
	ipk, err := makeTarGz(outer, modTime)
	if err != nil {
		return err
	}
	return os.WriteFile(path, ipk, 0o644)
}

// gitCommitEpoch 返回 HEAD 的提交时间（Unix 秒），用作 tar 文件时间戳。
func gitCommitEpoch() (int64, error) {
	out, err := exec.Command("git", "show", "-s", "--format=%ct", "HEAD").Output()
	if err != nil {
		return 0, err
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, err
	}
	return epoch, nil
}

func gitVersion() string {
	if out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	return "dev"
}
