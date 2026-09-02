// mywanipd 是 OpenWrt 上的 WAN 口 IP 查询守护进程。
// 启动时读取一次 UCI 配置，之后通过 HTTP 实时返回指定接口的 IPv4/IPv6 地址。
// 配置变更后由 procd reload trigger 重启本进程生效（reload = restart）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mywanip/internal/config"
	"mywanip/internal/httpserver"
	"mywanip/internal/ipsource"
)

// version 通过 -ldflags "-X main.version=..." 注入（git describe）。
var version = "dev"

func main() {
	cfgPath := flag.String("config", "/etc/config/mywanip", "path to UCI config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("mywanipd " + version)
		os.Exit(0)
	}

	// 日志统一走 stdout：procd 会收进 logd，可用 logread 查看。
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags)

	if err := run(*cfgPath); err != nil {
		log.Fatalf("mywanipd: %v", err)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if !cfg.Enabled {
		log.Printf("mywanipd is disabled in %s (option enabled '0'); exiting", cfgPath)
		return nil
	}
	log.Printf("mywanipd %s starting: interface=%s port=%d bind_ipv4=%v bind_ipv6=%v",
		version, cfg.Interface, cfg.Port, cfg.BindIPv4, cfg.BindIPv6)

	srv := httpserver.New(
		cfg,
		version,
		func() (net.IP, error) { return ipsource.IPv4(cfg.Interface) },
		func() (net.IP, error) { return ipsource.IPv6(cfg.Interface) },
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Printf("mywanipd shutting down (signal received)")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
		return nil
	}
}
