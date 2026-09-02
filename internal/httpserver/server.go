// Package httpserver 提供 mywanipd 的 HTTP 接口：
//
//	GET /ipv4  → 纯文本 IPv4（无地址 503）
//	GET /ipv6  → 纯文本 IPv6 GUA（无地址 503）
//	GET /      → JSON 汇总，恒 200，缺失项为空串
//	OPTIONS *  → 204 + CORS 预检响应
//	其他方法   → 405；其他路径 → 404
//
// CORS 放开为 *：LuCI 页面跨端口直连本服务展示状态，
// 返回内容只是本机 IP（LAN 内只读信息），无需鉴权。
package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"

	"mywanip/internal/config"
)

// setV6Only 在 socket 上置 IPV6_V6ONLY=1：仅勾选 IPv6 时，
// 监听 [::] 不接受 IPv4-mapped 连接，也不会与 IPv4 监听冲突。
func setV6Only(_, _ string, c syscall.RawConn) error {
	var setErr error
	if err := c.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
	}); err != nil {
		return err
	}
	return setErr
}

// IPFunc 返回某个 IP 版本的地址，失败返回 error。
type IPFunc func() (net.IP, error)

// Server 是 mywanipd 的 HTTP 服务。
type Server struct {
	httpServer *http.Server
	handler    http.Handler
	cfg        *config.Config
	version    string
	ipv4       IPFunc
	ipv6       IPFunc
}

// ServeHTTP 便于测试直接用 httptest 驱动，无需真正监听端口。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// New 装配路由与取址函数。ipv4/ipv6 通常绑定 ipsource.IPv4/IPv6，
// 测试时可注入桩函数。
func New(cfg *config.Config, version string, ipv4, ipv6 IPFunc) *Server {
	s := &Server{
		cfg:     cfg,
		version: version,
		ipv4:    ipv4,
		ipv6:    ipv6,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/ipv4", s.handleIPv4)
	mux.HandleFunc("/ipv6", s.handleIPv6)
	s.handler = mux

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// ListenAndServe 阻塞运行直到服务关闭。
func (s *Server) ListenAndServe() error {
	ln, err := s.buildListener()
	if err != nil {
		return err
	}
	log.Printf("mywanipd %s listening on :%d (ipv4=%v ipv6=%v)",
		s.version, s.cfg.Port, s.cfg.BindIPv4, s.cfg.BindIPv6)
	err = s.httpServer.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// buildListener 按绑定开关创建监听器：
//   - v4+v6：绑定 :port（Linux 双栈单 socket）
//   - 仅 v4：tcp4 绑定 0.0.0.0
//   - 仅 v6：tcp6 绑定 [::] 并置 IPV6_V6ONLY（避免占用 v4 端口）
func (s *Server) buildListener() (net.Listener, error) {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	switch {
	case s.cfg.BindIPv4 && s.cfg.BindIPv6:
		return net.Listen("tcp", addr)
	case s.cfg.BindIPv4:
		return net.Listen("tcp4", "0.0.0.0"+addr)
	case s.cfg.BindIPv6:
		lc := net.ListenConfig{Control: setV6Only}
		return lc.Listen(context.Background(), "tcp6", "[::]"+addr)
	default:
		return nil, fmt.Errorf("no bind address enabled (enable bind_ipv4 and/or bind_ipv6)")
	}
}

// Shutdown 优雅关闭，超时由 ctx 控制。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.writeCORS(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}
	if !s.allowMethod(w, r, http.MethodGet) {
		return
	}

	// 汇总接口恒 200：LuCI fetch 最友好，取不到的字段为空串。
	resp := map[string]string{"ipv4": "", "ipv6": ""}
	if ip, err := s.ipv4(); err == nil {
		resp["ipv4"] = ip.String()
	} else {
		log.Printf("get ipv4: %v", err)
	}
	if ip, err := s.ipv6(); err == nil {
		resp["ipv6"] = ip.String()
	} else {
		log.Printf("get ipv6: %v", err)
	}

	s.writeCORS(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleIPv4(w http.ResponseWriter, r *http.Request) {
	s.handleText(w, r, s.ipv4)
}

func (s *Server) handleIPv6(w http.ResponseWriter, r *http.Request) {
	s.handleText(w, r, s.ipv6)
}

func (s *Server) handleText(w http.ResponseWriter, r *http.Request, fn IPFunc) {
	if !s.allowMethod(w, r, http.MethodGet) {
		return
	}
	ip, err := fn()
	s.writeCORS(w)
	if err != nil {
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(err.Error() + "\n"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(ip.String() + "\n"))
}

// allowMethod 处理 GET 之外的方法：OPTIONS 返回预检，其余 405。
func (s *Server) allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	s.writeCORS(w)
	switch r.Method {
	case method:
		return true
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return false
	default:
		w.Header().Set("Allow", "GET, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
}

func (s *Server) writeCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Max-Age", "86400")
	h.Set("Server", "mywanipd/"+s.version)
}
