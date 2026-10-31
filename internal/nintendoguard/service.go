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
}

type flow struct {
	host string
	conn *net.TCPConn
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
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
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
	if c.Mode == "off" {
		return err != nil && strings.Contains(string(b), "No such file")
	}
	return err == nil && strings.Contains(string(b), c.Fingerprint())
}

func (s *Service) current() Config { s.mu.RLock(); defer s.mu.RUnlock(); return s.config }

func (s *Service) blocked(proto, host, client string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if proto == "DNS" {
		s.dnsBlocked++
	} else {
		s.tlsBlocked++
	}
	s.events = append(s.events, Event{time.Now().Format(time.RFC3339), proto, host, client})
	if len(s.events) > 32 {
		s.events = s.events[len(s.events)-32:]
	}
}

func deviceIPs(mac string) []string {
	if mac == "" {
		return []string{}
	}
	seen := make(map[string]bool)
	b, _ := os.ReadFile("/tmp/dhcp.leases")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.EqualFold(f[1], mac) && net.ParseIP(f[2]).To4() != nil {
			expires, _ := strconv.ParseInt(f[0], 10, 64)
			if expires != 0 && expires < time.Now().Unix() {
				continue
			}
			seen[f[2]] = true
		}
	}
	b, _ = runInput("", "ip", "neigh", "show")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "lladdr" && net.ParseIP(f[0]) != nil {
				if strings.EqualFold(f[i+1], mac) {
					seen[f[0]] = true
				} else {
					delete(seen, f[0])
				}
			}
		}
	}
	var result []string
	for ip := range seen {
		result = append(result, ip)
	}
	return result
}

// Only invalidate this device's DNS/HTTP connections. Other clients and the
// router's existing acceleration service are never restarted.
func reconnect(mac string, all bool) string {
	if _, err := exec.LookPath("conntrack"); err != nil {
		return "conntrack unavailable: reconnect the Switch Wi-Fi to clear existing connections"
	}
	ips := deviceIPs(mac)
	for _, ip := range ips {
		family := "ipv4"
		if net.ParseIP(ip).To4() == nil {
			family = "ipv6"
		}
		if all || family == "ipv6" {
			// Retire existing flows, including fw4 software/hardware flow offload,
			// when entering full isolation. This remains scoped to the target IP.
			_, _ = runInput("", "conntrack", "-D", "-f", family, "-s", ip)
			_, _ = runInput("", "conntrack", "-D", "-f", family, "-d", ip)
			continue
		}
		for _, proto := range []string{"tcp", "udp"} {
			for _, port := range []string{"53", "80", "443", "853"} {
				_, _ = runInput("", "conntrack", "-D", "-f", "ipv4", "-s", ip, "-p", proto, "--dport", port)
			}
		}
	}
	return ""
}

// Cleanup is for uninstall after procd has stopped the daemon.
func Cleanup() error {
	c, err := ReadConfig()
	if err != nil {
		return err
	}
	full := c.Mode == "all"
	c.Mode = "off"
	if err = ApplyFirewall(c); err != nil {
		return err
	}
	if warning := reconnect(c.MAC, full); warning != "" {
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
	if old == c && firewallOK(c) {
		return nil
	}
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
	s.mu.Lock()
	for _, f := range s.flows {
		if c.Blocks(f.host) || old.MAC != c.MAC {
			f.conn.Close()
		}
	}
	s.mu.Unlock()
	warning := ""
	if old.Mode != c.Mode || old.MAC != c.MAC {
		warning = reconnect(c.MAC, old.Mode == "all" || c.Mode == "all")
	}
	if old.MAC != c.MAC {
		if oldWarning := reconnect(old.MAC, old.Mode == "all"); oldWarning != "" {
			warning = oldWarning
		}
	}
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
		"update_domains": UpdateDomains,
	}
	s.mu.RUnlock()
	r["device_ips"] = deviceIPs(c.MAC)
	return r
}

func (s *Service) dns(w dns.ResponseWriter, req *dns.Msg) {
	c := s.current()
	// With no redirection installed, the listener must not become an open resolver.
	if c.Mode == "off" {
		resp := new(dns.Msg)
		resp.SetRcode(req, dns.RcodeRefused)
		w.WriteMsg(resp)
		return
	}
	for _, q := range req.Question {
		if c.Blocks(q.Name) {
			s.blocked("DNS", strings.TrimSuffix(q.Name, "."), w.RemoteAddr().String())
			resp := new(dns.Msg)
			resp.SetRcode(req, dns.RcodeNameError)
			resp.RecursionAvailable = true
			w.WriteMsg(resp)
			return
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
	w.WriteMsg(resp)
}

func (s *Service) handleTCP(c *net.TCPConn) {
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
	s.mu.Lock()
	if s.config.Blocks(host) {
		s.mu.Unlock()
		s.blocked("HTTPS/HTTP", host, c.RemoteAddr().String())
		return
	}
	s.flows[c] = flow{host, c}
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
		err = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&c)
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
	tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: 5443})
	if err != nil {
		return err
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", ":5354")
	if err != nil {
		return err
	}
	defer udp.Close()
	dnsTCP, err := net.Listen("tcp4", ":5354")
	if err != nil {
		return err
	}
	defer dnsTCP.Close()
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
	if c.Mode != "off" {
		if warning := reconnect(c.MAC, c.Mode == "all"); warning != "" {
			s.lastError = warning
		}
	}
	errCh := make(chan error, 4)
	udpServer := &dns.Server{PacketConn: udp, Handler: dns.HandlerFunc(s.dns), MaxTCPQueries: 32}
	tcpServer := &dns.Server{Listener: dnsTCP, Handler: dns.HandlerFunc(s.dns), IdleTimeout: func() time.Duration { return 15 * time.Second }, MaxTCPQueries: 32}
	go func() { errCh <- udpServer.ActivateAndServe() }()
	go func() { errCh <- tcpServer.ActivateAndServe() }()
	api := &http.Server{Handler: http.HandlerFunc(s.api), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second}
	go func() { errCh <- api.Serve(control) }()
	go func() {
		for {
			conn, e := tcp.AcceptTCP()
			if e != nil {
				errCh <- e
				return
			}
			select {
			case s.limit <- struct{}{}:
				go s.handleTCP(conn)
			default:
				conn.Close()
			}
		}
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer udpServer.Shutdown()
	defer tcpServer.Shutdown()
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
