// Package ipsource 枚举指定网络接口上的 IP 地址并按规则挑选。
//
// 过滤规则：
//   - IPv4：排除 loopback、链路本地(169.254/16)、未指定地址；
//     保留 RFC1918 与 CGNAT(100.64.0.0/10)——PPPoE 拿到运营商级 NAT
//     地址时接口上的真实地址就是 100.64.x.x，照常返回；保留私网地址也
//     便于双 NAT 诊断和本机 en0 联调。
//   - IPv6：只保留全球单播 GUA(2000::/3)，排除链路本地 fe80::/10、
//     ULA fc00::/7、IPv4-mapped ::ffff:x.x.x.x、组播、loopback。
//
// 已知限制：标准库无法区分 RFC4941 临时地址与稳定地址（两者在
// InterfaceAddrs 视角完全相同）。OpenWrt 上 pppoe-wan 通常只有一个
// GUA，实际影响很小。
package ipsource

import (
	"fmt"
	"net"
	"sort"
)

// Lookup 返回指定接口上的所有地址（同 net.InterfaceByName + Addrs）。
func Lookup(interfaceName string) ([]net.Addr, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %q: %w", interfaceName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("addrs of %q: %w", interfaceName, err)
	}
	return addrs, nil
}

// IPv4 返回接口上选中的 IPv4 地址，没有合法地址时返回错误。
func IPv4(interfaceName string) (net.IP, error) {
	addrs, err := Lookup(interfaceName)
	if err != nil {
		return nil, err
	}
	if ip := PickIPv4(addrs); ip != nil {
		return ip, nil
	}
	return nil, fmt.Errorf("no usable IPv4 address on %s", interfaceName)
}

// IPv6 返回接口上选中的 IPv6 全球单播地址，没有合法地址时返回错误。
func IPv6(interfaceName string) (net.IP, error) {
	addrs, err := Lookup(interfaceName)
	if err != nil {
		return nil, err
	}
	if ip := PickIPv6(addrs); ip != nil {
		return ip, nil
	}
	return nil, fmt.Errorf("no global IPv6 address on %s", interfaceName)
}

// PickIPv4 从地址列表中挑选合法 IPv4，多候选取字典序最小（确定性输出）。
func PickIPv4(addrs []net.Addr) net.IP {
	candidates := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if IsValidIPv4(ipnet.IP) {
			candidates = append(candidates, ipnet.IP.To4())
		}
	}
	return smallest(candidates)
}

// PickIPv6 从地址列表中挑选合法 IPv6 GUA，多候选取字典序最小。
func PickIPv6(addrs []net.Addr) net.IP {
	candidates := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if IsValidIPv6(ipnet.IP) {
			candidates = append(candidates, ipnet.IP.To16())
		}
	}
	return smallest(candidates)
}

// IsValidIPv4 判断是否为可对外返回的 IPv4 地址：
// 非 loopback、非链路本地、非未指定。私网/CGNAT 放行。
func IsValidIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	if v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsUnspecified() {
		return false
	}
	return true
}

// IsValidIPv6 判断是否为可对外返回的 IPv6 地址：仅全球单播 2000::/3。
func IsValidIPv6(ip net.IP) bool {
	// To4() 非 nil 说明是 v4 或 v4-mapped，排除。
	if ip.To4() != nil {
		return false
	}
	v6 := ip.To16()
	if v6 == nil {
		return false
	}
	// 2000::/3：首字节高 3 位为 001。
	if v6[0]&0xE0 != 0x20 {
		return false
	}
	return v6.IsGlobalUnicast()
}

// smallest 返回字典序最小的 IP，列表为空时返回 nil。
// 同一接口状态下多次请求输出确定，避免多地址时结果抖动。
func smallest(ips []net.IP) net.IP {
	if len(ips) == 0 {
		return nil
	}
	sort.Slice(ips, func(i, j int) bool {
		return ips[i].String() < ips[j].String()
	})
	return ips[0]
}
