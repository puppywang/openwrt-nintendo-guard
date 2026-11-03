package nintendoguard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var ConfigPath = envPath("NINTENDO_GUARD_CONFIG", "/etc/nintendo-control.json")
var SocketPath = envPath("NINTENDO_GUARD_SOCKET", "/var/run/nintendo-control.sock")

func envPath(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type Event struct {
	Time     string `json:"time"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Client   string `json:"client"`
	MAC      string `json:"mac,omitempty"`
}

type flow struct {
	conn *net.TCPConn
	mac  string
}

type Service struct {
	mu                              sync.RWMutex
	applyMu                         sync.Mutex
	config                          Config
	events                          []Event
	dnsBlocked, tlsBlocked, allowed uint64
	flows                           map[*net.TCPConn]flow
	lastError                       string
	started                         time.Time
	limit                           chan struct{}
}

func ReadConfig() (Config, error) {
	b, err := os.ReadFile(ConfigPath)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c.Validate()
}

func runInput(input string, command string, args ...string) ([]byte, error) {
	return runInputContext(context.Background(), input, command, args...)
}

func runInputContext(parent context.Context, input string, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("%s: %w: %s", command, err, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func ApplyFirewall(c Config) error {
	var err error
	if c, err = c.Validate(); err != nil {
		return err
	}
	_, err = runInput(c.Rules(), "nft", "-f", "-")
	return err
}

func firewallOK(c Config) bool {
	b, err := runInput("", "nft", "list", "table", "inet", "nintendo_control")
	if !c.Active() {
		return err != nil && strings.Contains(string(b), "No such file")
	}
	return err == nil && strings.Contains(string(b), c.Fingerprint())
}

func (s *Service) current() Config { s.mu.RLock(); defer s.mu.RUnlock(); return s.config }

func (s *Service) blocked(proto, host, client string) {
	mac := clientMAC(client)
	s.mu.Lock()
	defer s.mu.Unlock()
	if proto == "DNS" {
		s.dnsBlocked++
	} else {
		s.tlsBlocked++
	}
	s.events = append(s.events, Event{time.Now().Format(time.RFC3339), proto, host, client, mac})
	if len(s.events) > 32 {
		s.events = s.events[len(s.events)-32:]
	}
}

// Listener selection and firewall policy never rely on this display inventory.
func deviceAddresses() map[string][]string {
	owners := make(map[string]string)
	b, _ := os.ReadFile("/tmp/dhcp.leases")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && net.ParseIP(f[2]).To4() != nil {
			expires, _ := strconv.ParseInt(f[0], 10, 64)
			if expires != 0 && expires < time.Now().Unix() {
				continue
			}
			owners[f[2]] = strings.ToLower(f[1])
		}
	}
	b, _ = runInput("", "ip", "neigh", "show")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "lladdr" && net.ParseIP(f[0]) != nil {
				owners[f[0]] = strings.ToLower(f[i+1])
			}
		}
	}
	result := make(map[string][]string)
	for ip, mac := range owners {
		result[mac] = append(result[mac], ip)
	}
	for _, ips := range result {
		sort.Strings(ips)
	}
	return result
}

func clientMAC(client string) string {
	ip, _, err := net.SplitHostPort(client)
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile("/proc/net/arp")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == ip && f[2] != "0x0" {
			return strings.ToLower(f[3])
		}
	}
	return ""
}

// Only invalidate this device's DNS/HTTP connections. Other clients and the
// router's existing acceleration service are never restarted.
func reconnect(ctx context.Context, ips []string, all bool) string {
	if _, err := exec.LookPath("conntrack"); err != nil {
		return "conntrack unavailable: reconnect the Switch Wi-Fi to clear existing connections"
	}
	for _, ip := range ips {
		if ctx.Err() != nil {
			return "connection cleanup timed out: reconnect changed devices' Wi-Fi"
		}
		family := "ipv4"
		if net.ParseIP(ip).To4() == nil {
			family = "ipv6"
		}
		if all || family == "ipv6" {
			// Retire existing flows, including fw4 software/hardware flow offload,
			// when entering full isolation. This remains scoped to the target IP.
			_, _ = runInputContext(ctx, "", "conntrack", "-D", "-f", family, "-s", ip)
			_, _ = runInputContext(ctx, "", "conntrack", "-D", "-f", family, "-d", ip)
			continue
		}
		for _, proto := range []string{"tcp", "udp"} {
			for _, port := range []string{"53", "80", "443", "853"} {
				_, _ = runInputContext(ctx, "", "conntrack", "-D", "-f", "ipv4", "-s", ip, "-p", proto, "--dport", port)
			}
		}
	}
	if ctx.Err() != nil {
		return "connection cleanup timed out: reconnect changed devices' Wi-Fi"
	}
	return ""
}

func changedDevices(old, c Config) map[string]bool {
	before, after := make(map[string]string), make(map[string]string)
	for _, d := range old.Devices {
		before[d.MAC] = d.Mode
	}
	for _, d := range c.Devices {
		after[d.MAC] = d.Mode
	}
	changed := make(map[string]bool)
	for mac, mode := range before {
		if mode != after[mac] && (mode != "off" || (after[mac] != "" && after[mac] != "off")) {
			changed[mac] = mode == "all" || after[mac] == "all"
		}
	}
	for mac, mode := range after {
		if mode != before[mac] && mode != "off" {
			changed[mac] = mode == "all" || before[mac] == "all"
		}
	}
	return changed
}

func reconnectChanged(changed map[string]bool, addresses map[string][]string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	warning := ""
	for mac, full := range changed {
		if w := reconnect(ctx, addresses[mac], full); w != "" {
			warning = w
		}
		if ctx.Err() != nil {
			break
		}
	}
	return warning
}

// Cleanup is for uninstall after procd has stopped the daemon.
func Cleanup() error {
	c, err := ReadConfig()
	if err != nil {
		return err
	}
	off := Config{Devices: []Device{}, Upstream: c.Upstream}
	if err = ApplyFirewall(off); err != nil {
		return err
	}
	if warning := reconnectChanged(changedDevices(c, off), deviceAddresses()); warning != "" {
		return fmt.Errorf("%s", warning)
	}
	return nil
}

func (s *Service) set(c Config) error {
	var err error
	if c, err = c.Validate(); err != nil {
		return err
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	old := s.current()
	// Always persist a save, including an unchanged legacy configuration: this
	// is the point at which its on-disk format becomes a devices list.
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp, err := os.CreateTemp(filepath.Dir(ConfigPath), ".nintendo-control-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// Publish the new application policy first so newly redirected connections
	// can never pass through the previous, weaker policy.
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
	if err = ApplyFirewall(c); err != nil {
		s.mu.Lock()
		s.config = old
		s.mu.Unlock()
		return err
	}
	if err = os.Rename(tmpName, ConfigPath); err != nil {
		s.mu.Lock()
		s.config = old
		s.mu.Unlock()
		rollbackErr := ApplyFirewall(old)
		return fmt.Errorf("save failed: %v; firewall rollback: %v", err, rollbackErr)
	}
	changed := changedDevices(old, c)
	addresses := map[string][]string{}
	if len(changed) > 0 {
		addresses = deviceAddresses()
	}
	s.mu.Lock()
	for _, f := range s.flows {
		_, affected := changed[f.mac]
		ip, _, _ := net.SplitHostPort(f.conn.RemoteAddr().String())
		for mac := range changed {
			affected = affected || slices.Contains(addresses[mac], ip)
		}
		if affected {
			f.conn.Close()
		}
	}
	s.mu.Unlock()
	warning := reconnectChanged(changed, addresses)
	s.mu.Lock()
	s.lastError = warning
	s.mu.Unlock()
	return nil
}

func (s *Service) status() map[string]any {
	c := s.current()
	ok := firewallOK(c)
	s.mu.RLock()
	r := map[string]any{
		"config": c, "running": true, "firewall_ok": ok,
		"dns_blocked": s.dnsBlocked, "https_blocked": s.tlsBlocked, "allowed": s.allowed,
		"active_connections": len(s.flows), "recent": append([]Event{}, s.events...),
		"error": s.lastError, "started": s.started.Format(time.RFC3339),
		"update_domains":   UpdateDomains,
		"nintendo_domains": NintendoDomains, "connection_test_domains": ConnectionTestDomains,
		"pass_through_test_domains": PassThroughTestDomains,
	}
	s.mu.RUnlock()
	addresses := deviceAddresses()
	devices := make([]map[string]any, 0, len(c.Devices))
	for _, d := range c.Devices {
		ips := addresses[d.MAC]
		if ips == nil {
			ips = []string{}
		}
		devices = append(devices, map[string]any{"name": d.Name, "mac": d.MAC, "mode": d.Mode, "ips": ips})
	}
	r["devices"] = devices
	r["max_devices"] = MaxDevices
	return r
}

func (s *Service) dns(w dns.ResponseWriter, req *dns.Msg) {
	s.dnsMode("updates", w, req)
}

func (s *Service) dnsMode(mode string, w dns.ResponseWriter, req *dns.Msg) {
	c := s.current()
	if len(req.Question) != 1 {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeFormatError)
		w.WriteMsg(resp)
		return
	}
	// With no redirection installed, the listener must not become an open resolver.
	if !c.HasMode(mode) {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeRefused)
		w.WriteMsg(resp)
		return
	}
	for _, q := range req.Question {
		if (Device{Mode: mode}).Blocks(q.Name) {
			s.blocked("DNS", strings.TrimSuffix(q.Name, "."), w.RemoteAddr().String())
			if mode == "nintendo" {
				w.WriteMsg(nintendoReply(req, net.IPv4zero, true))
				return
			}
			resp := new(dns.Msg)
			resp.SetRcode(req, dns.RcodeNameError)
			resp.RecursionAvailable = true
			w.WriteMsg(resp)
			return
		}
	}
	if mode == "nintendo" {
		for _, q := range req.Question {
			if connectionTest(q.Name) {
				ip := routerIPv4(w)
				if ip == nil {
					resp := new(dns.Msg)
					resp.SetRcode(req, dns.RcodeServerFailure)
					w.WriteMsg(resp)
				} else {
					w.WriteMsg(nintendoReply(req, ip, false))
				}
				return
			}
		}
	}
	client := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	resp, _, err := client.Exchange(req, c.Upstream)
	if err == nil && resp.Truncated {
		client.Net = "tcp"
		resp, _, err = client.Exchange(req, c.Upstream)
	}
	if err != nil {
		resp = new(dns.Msg)
		resp.SetRcode(req, dns.RcodeServerFailure)
	}
	// For an explicitly approved test hostname, retain its genuine upstream
	// DNS chain even if an intermediate CDN alias is under Nintendo's domain.
	// Generic aliases pointing TO Nintendo services still remain blocked.
	if err == nil && mode == "nintendo" && !testEndpoint(req.Question[0].Name) {
		for _, rr := range resp.Answer {
			target := ""
			switch alias := rr.(type) {
			case *dns.CNAME:
				target = alias.Target
			case *dns.DNAME:
				target = alias.Target
			}
			if target != "" && ((Device{Mode: mode}).Blocks(target) || connectionTest(target)) {
				s.blocked("DNS", strings.TrimSuffix(req.Question[0].Name, "."), w.RemoteAddr().String())
				resp = nintendoReply(req, net.IPv4zero, true)
				break
			}
		}
	}
	w.WriteMsg(resp)
}

func (s *Service) handleTCP(c *net.TCPConn, listenerMode string) {
	defer func() { c.Close(); <-s.limit }()
	dest, err := OriginalDestination(c)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Now().Add(8 * time.Second))
	var raw []byte
	var host string
	if strings.HasSuffix(dest, ":443") {
		raw, host, err = ReadClientHello(c)
	} else {
		raw, host, err = ReadHTTPHost(c)
	}
	if err != nil || host == "" {
		s.blocked("UNREADABLE", "无法识别域名的连接", c.RemoteAddr().String())
		return
	}
	mac := clientMAC(c.RemoteAddr().String())
	s.mu.Lock()
	// The destination port selects policy independently of address inventory.
	// A known device changing modes during ClientHello must reconnect first.
	mode := listenerMode
	for _, d := range s.config.Devices {
		if d.MAC == mac {
			mode = d.Mode
		}
	}
	if !s.config.HasMode(listenerMode) || mode != listenerMode || (Device{Mode: listenerMode}).Blocks(host) {
		s.mu.Unlock()
		s.blocked("HTTPS/HTTP", host, c.RemoteAddr().String())
		return
	}
	if listenerMode == "nintendo" && connectionTest(host) {
		s.mu.Unlock()
		// This is a local HTTP emulation, never a TLS MITM or an upstream dial.
		if strings.HasSuffix(dest, ":80") {
			c.SetWriteDeadline(time.Now().Add(3 * time.Second))
			c.Write(connectionTestHTTP(host, raw))
		}
		return
	}
	s.flows[c] = flow{conn: c, mac: mac}
	s.allowed++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.flows, c); s.mu.Unlock() }()
	c.SetReadDeadline(time.Time{})
	upstream, err := net.DialTimeout("tcp4", dest, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	up := upstream.(*net.TCPConn)
	if _, err = up.Write(raw); err != nil {
		return
	}
	// TCP-to-TCP copies retain Go's splice fast path on Linux.
	done := make(chan struct{})
	go func() { io.Copy(up, c); up.CloseWrite(); close(done) }()
	io.Copy(c, up)
	c.Close()
	up.Close()
	<-done
}

func (s *Service) api(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var err error
	switch r.URL.Path {
	case "/status":
	case "/set":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		var c Config
		err = json.NewDecoder(io.LimitReader(r.Body, ConfigInputLimit)).Decode(&c)
		if err == nil {
			err = s.set(c)
		}
	case "/reapply":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		s.applyMu.Lock()
		err = ApplyFirewall(s.current())
		s.applyMu.Unlock()
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(s.status())
}

func Serve(ctx context.Context) error {
	c, err := ReadConfig()
	if err != nil {
		return err
	}
	s := &Service{config: c, flows: make(map[*net.TCPConn]flow), started: time.Now(), limit: make(chan struct{}, 64)}
	// Bind every listener before changing firewall rules, so a port conflict
	// leaves the installed policy intact and no partial listener set running.
	var dnsServers []*dns.Server
	var proxies []struct {
		listener *net.TCPListener
		mode     string
	}
	defer func() {
		for _, server := range dnsServers {
			if server.PacketConn != nil {
				server.PacketConn.Close()
			}
			if server.Listener != nil {
				server.Listener.Close()
			}
		}
		for _, proxy := range proxies {
			proxy.listener.Close()
		}
	}()
	for _, binding := range policyListeners {
		handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) { s.dnsMode(binding.mode, w, req) })
		udp, e := net.ListenPacket("udp4", fmt.Sprintf(":%d", binding.dnsPort))
		if e != nil {
			return e
		}
		dnsServers = append(dnsServers, &dns.Server{PacketConn: udp, Handler: handler, MaxTCPQueries: 32})
		tcp, e := net.Listen("tcp4", fmt.Sprintf(":%d", binding.dnsPort))
		if e != nil {
			return e
		}
		dnsServers = append(dnsServers, &dns.Server{Listener: tcp, Handler: handler, IdleTimeout: func() time.Duration { return 15 * time.Second }, MaxTCPQueries: 32})
		if binding.tcpPort != 0 {
			listener, e := net.ListenTCP("tcp4", &net.TCPAddr{Port: binding.tcpPort})
			if e != nil {
				return e
			}
			proxies = append(proxies, struct {
				listener *net.TCPListener
				mode     string
			}{listener, binding.mode})
		}
	}
	// procd guarantees one instance. Refuse to remove any non-socket path.
	if st, e := os.Lstat(SocketPath); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to remove non-socket %s", SocketPath)
		}
		if e = os.Remove(SocketPath); e != nil {
			return e
		}
	}
	control, err := net.Listen("unix", SocketPath)
	if err != nil {
		return err
	}
	defer control.Close()
	if err = os.Chmod(SocketPath, 0600); err != nil {
		return err
	}
	if err = ApplyFirewall(c); err != nil {
		return err
	}
	if c.Active() {
		if warning := reconnectChanged(changedDevices(Config{}, c), deviceAddresses()); warning != "" {
			s.lastError = warning
		}
	}
	errCh := make(chan error, len(dnsServers)+len(proxies)+1)
	for _, server := range dnsServers {
		go func() { errCh <- server.ActivateAndServe() }()
		defer server.Shutdown()
	}
	api := &http.Server{Handler: http.HandlerFunc(s.api), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second}
	go func() { errCh <- api.Serve(control) }()
	for _, proxy := range proxies {
		go func() {
			for {
				conn, e := proxy.listener.AcceptTCP()
				if e != nil {
					errCh <- e
					return
				}
				select {
				case s.limit <- struct{}{}:
					go s.handleTCP(conn, proxy.mode)
				default:
					conn.Close()
				}
			}
		}()
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer api.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err = <-errCh:
			return err
		case <-ticker.C:
			s.applyMu.Lock()
			if !firewallOK(s.current()) {
				err = ApplyFirewall(s.current())
				if err != nil {
					log.Print(err)
					s.mu.Lock()
					s.lastError = err.Error()
					s.mu.Unlock()
				}
			}
			s.applyMu.Unlock()
		}
	}
}

func Call(path string, body io.Reader) ([]byte, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", SocketPath)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 55 * time.Second}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, "http://localhost/"+path, body)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 131072))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return b, fmt.Errorf("control request failed: %s", b)
	}
	return b, nil
}
