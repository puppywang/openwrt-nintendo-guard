package nintendoguard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Config struct {
	Mode     string `json:"mode"`
	MAC      string `json:"mac"`
	Upstream string `json:"upstream"`
}

var UpdateDomains = []string{
	"sun.p01.lp1.d4c.srv.nintendo.net", "atumn.p01.lp1.d4c.srv.nintendo.net", "aqua.p01.lp1.d4c.srv.nintendo.net",
	"sun.hac.lp1.d4c.nintendo.net", "atumn.hac.lp1.d4c.nintendo.net", "aqua.hac.lp1.d4c.nintendo.net",
}

func (c Config) Validate() (Config, error) {
	if c.Mode != "off" && c.Mode != "updates" && c.Mode != "all" {
		return c, fmt.Errorf("mode must be off, updates or all")
	}
	if c.Upstream == "" {
		c.Upstream = "127.0.0.1:53"
	}
	host, port, err := net.SplitHostPort(c.Upstream)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || net.ParseIP(host) == nil || n < 1 || n > 65535 || n == 5354 {
		return c, fmt.Errorf("upstream must be an IP:port DNS server, not guard port 5354")
	}
	if c.Mode == "off" && c.MAC == "" {
		return c, nil
	}
	m, err := net.ParseMAC(c.MAC)
	if err != nil || len(m) != 6 || m[0]&1 != 0 || m.String() == "00:00:00:00:00:00" {
		return c, fmt.Errorf("a unicast device MAC address is required")
	}
	c.MAC = strings.ToLower(m.String())
	return c, nil
}

func (c Config) Blocks(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	var domains []string
	switch c.Mode {
	case "updates":
		domains = UpdateDomains
	case "all":
		// Full protection deliberately isolates this device from routed internet,
		// including cached IPs and non-HTTP game traffic, instead of guessing all
		// future Nintendo hostnames. The UI makes this broader effect explicit.
		return true
	default:
		return false
	}
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func (c Config) Fingerprint() string {
	s := sha256.Sum256([]byte(c.Mode + ":" + c.MAC))
	return "nintendo-control-v3-" + hex.EncodeToString(s[:8])
}

// A dedicated table survives normal fw4 reloads. The firewall include and the
// daemon watchdog recreate it if another tool flushes the complete ruleset.
func (c Config) Rules() string {
	if c.Mode == "off" {
		return "add table inet nintendo_control\ndelete table inet nintendo_control\n"
	}
	fullForward, fullInput := "", ""
	if c.Mode == "all" {
		fullForward = fmt.Sprintf("ether saddr %s counter reject", c.MAC)
		// Prevent the router's own proxy listeners from bypassing WAN isolation.
		// DHCP and guarded DNS/HTTP remain reachable. Same-LAN L2 traffic is not routed here.
		fullInput = fmt.Sprintf("ether saddr %s meta l4proto { tcp, udp } th dport != { 53, 67, 5354, 5443 } counter reject", c.MAC)
	}
	return fmt.Sprintf(`add table inet nintendo_control
delete table inet nintendo_control
table inet nintendo_control {
 comment "%s"
 chain prerouting {
  type nat hook prerouting priority -110; policy accept;
  ether saddr %s meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :5354
  ether saddr %s meta nfproto ipv4 tcp dport { 80, 443 } counter redirect to :5443
 }
 chain forward {
  type filter hook forward priority -5; policy accept;
  %s
  ether saddr %s meta nfproto ipv6 counter reject
  ether saddr %s tcp dport 853 counter reject with tcp reset
  ether saddr %s udp dport { 443, 853 } counter reject
 }
 chain input {
  type filter hook input priority -5; policy accept;
  %s
  iifname != "lo" ether saddr != %s tcp dport { 5354, 5443 } counter reject with tcp reset
  iifname != "lo" ether saddr != %s udp dport 5354 counter reject
 }
}
`, c.Fingerprint(), c.MAC, c.MAC, fullForward, c.MAC, c.MAC, c.MAC, fullInput, c.MAC, c.MAC)
}
