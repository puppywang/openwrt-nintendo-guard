package nintendoguard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Config struct {
	// Mode and MAC are read only for compatibility with pre-0.2 configurations.
	Mode     string   `json:"mode,omitempty"`
	MAC      string   `json:"mac,omitempty"`
	Devices  []Device `json:"devices"`
	Upstream string   `json:"upstream"`
}

const MaxDevices = 64
const ConfigInputLimit = 65536

type Device struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	Mode string `json:"mode"`
}

var UpdateDomains = []string{
	"sun.p01.lp1.d4c.srv.nintendo.net", "atumn.p01.lp1.d4c.srv.nintendo.net", "aqua.p01.lp1.d4c.srv.nintendo.net",
	"sun.hac.lp1.d4c.nintendo.net", "atumn.hac.lp1.d4c.nintendo.net", "aqua.hac.lp1.d4c.nintendo.net",
}

func (c Config) Validate() (Config, error) {
	if c.Upstream == "" {
		c.Upstream = "127.0.0.1:53"
	}
	host, port, err := net.SplitHostPort(c.Upstream)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || net.ParseIP(host) == nil || n < 1 || n > 65535 || n == 5354 || n == 5355 {
		return c, fmt.Errorf("upstream must be an IP:port DNS server, not guard ports 5354/5355")
	}
	if c.Devices != nil && (c.Mode != "" || c.MAC != "") {
		return c, fmt.Errorf("use devices or legacy mode/mac, not both")
	}
	if c.Devices == nil {
		if c.Mode != "off" && c.Mode != "updates" && c.Mode != "all" {
			return c, fmt.Errorf("devices list is required")
		}
		c.Devices = []Device{}
		if c.MAC != "" || c.Mode != "off" {
			c.Devices = append(c.Devices, Device{Name: "Switch / Switch 2", MAC: c.MAC, Mode: c.Mode})
		}
	}
	if len(c.Devices) > MaxDevices {
		return c, fmt.Errorf("at most %d devices are supported", MaxDevices)
	}
	// Do not mutate a configuration that may still be used by another goroutine.
	devices := make([]Device, len(c.Devices))
	seen := make(map[string]bool)
	for i, d := range c.Devices {
		if d.Mode != "off" && d.Mode != "updates" && d.Mode != "all" {
			return c, fmt.Errorf("device %d: mode must be off, updates or all", i+1)
		}
		m, err := net.ParseMAC(strings.TrimSpace(d.MAC))
		if err != nil || len(m) != 6 || m[0]&1 != 0 || m.String() == "00:00:00:00:00:00" {
			return c, fmt.Errorf("device %d: a unicast device MAC address is required", i+1)
		}
		d.MAC = m.String()
		if seen[d.MAC] {
			return c, fmt.Errorf("duplicate device MAC: %s", d.MAC)
		}
		seen[d.MAC] = true
		d.Name = strings.TrimSpace(d.Name)
		if !utf8.ValidString(d.Name) || utf8.RuneCountInString(d.Name) > 64 || strings.ContainsAny(d.Name, "\r\n\x00") {
			return c, fmt.Errorf("device %d: name must be at most 64 characters on one line", i+1)
		}
		devices[i] = d
	}
	c.Devices, c.Mode, c.MAC = devices, "", ""
	return c, nil
}

func (d Device) Blocks(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	var domains []string
	switch d.Mode {
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
	var policies []string
	for _, d := range c.Devices {
		if d.Mode != "off" {
			policies = append(policies, d.MAC+":"+d.Mode)
		}
	}
	sort.Strings(policies)
	b, _ := json.Marshal(policies)
	s := sha256.Sum256(b)
	return "nintendo-control-v4-" + hex.EncodeToString(s[:8])
}

func (c Config) HasMode(mode string) bool {
	for _, d := range c.Devices {
		if d.Mode == mode {
			return true
		}
	}
	return false
}

func (c Config) Active() bool { return c.HasMode("updates") || c.HasMode("all") }

// A dedicated table survives normal fw4 reloads. The firewall include and the
// daemon watchdog recreate it if another tool flushes the complete ruleset.
func (c Config) Rules() string {
	if !c.Active() {
		return "add table inet nintendo_control\ndelete table inet nintendo_control\n"
	}
	var nat, forward, input strings.Builder
	var updates, all []string
	for _, d := range c.Devices {
		switch d.Mode {
		case "updates":
			updates = append(updates, d.MAC)
			fmt.Fprintf(&nat, "  ether saddr %s meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :5354\n", d.MAC)
			fmt.Fprintf(&nat, "  ether saddr %s meta nfproto ipv4 tcp dport { 80, 443 } counter redirect to :5443\n", d.MAC)
			fmt.Fprintf(&forward, "  ether saddr %s meta nfproto ipv6 counter reject\n  ether saddr %s tcp dport 853 counter reject with tcp reset\n  ether saddr %s udp dport { 443, 853 } counter reject\n", d.MAC, d.MAC, d.MAC)
		case "all":
			all = append(all, d.MAC)
			// A separate DNS listener keeps simultaneous modes independent of IP leases.
			fmt.Fprintf(&nat, "  ether saddr %s meta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 counter redirect to :5355\n", d.MAC)
			fmt.Fprintf(&forward, "  ether saddr %s counter reject\n", d.MAC)
			fmt.Fprintf(&input, "  ether saddr %s meta l4proto { tcp, udp } th dport != { 53, 67, 5355 } counter reject\n", d.MAC)
		}
	}
	for _, listener := range []struct {
		macs  []string
		ports string
	}{{updates, "{ 5354, 5443 }"}, {all, "5355"}} {
		match := ""
		if len(listener.macs) > 0 {
			match = "ether saddr != { " + strings.Join(listener.macs, ", ") + " } "
		}
		fmt.Fprintf(&input, "  iifname != \"lo\" %smeta l4proto { tcp, udp } th dport %s counter reject\n", match, listener.ports)
	}
	return fmt.Sprintf(`add table inet nintendo_control
delete table inet nintendo_control
table inet nintendo_control {
 comment "%s"
 chain prerouting {
  type nat hook prerouting priority -110; policy accept;
%s
 }
 chain forward {
  type filter hook forward priority -5; policy accept;
%s
 }
 chain input {
  type filter hook input priority -5; policy accept;
%s
 }
}
`, c.Fingerprint(), nat.String(), forward.String(), input.String())
}
