package ipsource

import (
	"net"
	"testing"
)

// cidr 构造一个模拟 iface.Addrs() 返回值的 *net.IPNet：
// IP 是带主机位的接口地址（ParseCIDR 默认会掩码掉主机位，这里还原）。
func cidr(s string) net.Addr {
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	ipnet.IP = ip
	return ipnet
}

func TestIsValidIPv4(t *testing.T) {
	keep := []string{
		"203.0.113.7/32",   // 公网
		"100.64.0.1/10",    // CGNAT（运营商级 NAT，PPPoE 常见，放行）
		"192.168.1.100/24", // RFC1918（双 NAT 诊断 / 本机联调，放行）
		"10.0.0.1/8",       // RFC1918
		"172.16.0.1/12",    // RFC1918
	}
	drop := []string{
		"127.0.0.1/8",    // loopback
		"169.254.1.1/16", // 链路本地
		"0.0.0.0/0",      // 未指定
	}
	for _, s := range keep {
		if !IsValidIPv4(cidr(s).(*net.IPNet).IP) {
			t.Errorf("IsValidIPv4(%s) = false, want true", s)
		}
	}
	for _, s := range drop {
		if IsValidIPv4(cidr(s).(*net.IPNet).IP) {
			t.Errorf("IsValidIPv4(%s) = true, want false", s)
		}
	}
}

func TestIsValidIPv6(t *testing.T) {
	keep := []string{
		"2001:db8::1234/64",  // GUA
		"2408:8207::abcd/64", // 国内运营商常见 GUA
	}
	drop := []string{
		"fe80::1234/64",      // 链路本地
		"fd12:3456::1/64",    // ULA
		"fc00::1/7",          // ULA
		"::ffff:1.2.3.4/128", // v4-mapped
		"ff02::1/128",        // 组播
		"::1/128",            // loopback
		"::/128",             // 未指定
	}
	for _, s := range keep {
		if !IsValidIPv6(cidr(s).(*net.IPNet).IP) {
			t.Errorf("IsValidIPv6(%s) = false, want true", s)
		}
	}
	for _, s := range drop {
		if IsValidIPv6(cidr(s).(*net.IPNet).IP) {
			t.Errorf("IsValidIPv6(%s) = true, want false", s)
		}
	}
}

func TestPickDeterministic(t *testing.T) {
	addrs := []net.Addr{
		cidr("fe80::1/64"),
		cidr("2001:db8::9/64"),
		cidr("2001:db8::1/64"), // 字典序更小，应被选中
		cidr("169.254.0.1/16"),
		cidr("100.64.5.6/10"),
		cidr("100.64.1.2/10"), // 字典序更小，应被选中
	}
	if got := PickIPv4(addrs); got.String() != "100.64.1.2" {
		t.Errorf("PickIPv4 = %s, want 100.64.1.2", got)
	}
	if got := PickIPv6(addrs); got.String() != "2001:db8::1" {
		t.Errorf("PickIPv6 = %s, want 2001:db8::1", got)
	}
}

func TestPickEmpty(t *testing.T) {
	if PickIPv4(nil) != nil {
		t.Errorf("PickIPv4(nil) != nil")
	}
	if PickIPv6([]net.Addr{cidr("fe80::1/64")}) != nil {
		t.Errorf("PickIPv6 with only link-local should be nil")
	}
}

func TestIPv4MissingInterface(t *testing.T) {
	if _, err := IPv4("no-such-interface-xyz"); err == nil {
		t.Errorf("expected error for nonexistent interface")
	}
}
