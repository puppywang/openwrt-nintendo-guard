package nintendoguard

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestPolicy(t *testing.T) {
	for _, tc := range []struct {
		mode, host string
		want       bool
	}{
		{"updates", "SUN.p01.lp1.d4c.srv.nintendo.net.", true},
		{"updates", "atumn.p01.lp1.d4c.srv.nintendo.net", true},
		{"updates", "aqua.p01.lp1.d4c.srv.nintendo.net", true},
		{"updates", "sun.hac.lp1.d4c.nintendo.net", true},
		{"updates", "atum.p01.lp1.d4c.srv.nintendo.net", false},
		{"updates", "atum.hac.lp1.d4c.nintendo.net", false},
		{"updates", "sun.p01.lp1.d4c.srv.nintendo.net.example.org", false},
		{"all", "atum.p01.lp1.d4c.srv.nintendo.net", true},
		{"all", "accounts.nintendo.com", true},
		{"updates", "evilnintendo.net", false},
		{"all", "example.org", true},
		{"off", "sun.p01.lp1.d4c.srv.nintendo.net", false},
	} {
		if got := (Config{Mode: tc.mode}).Blocks(tc.host); got != tc.want {
			t.Errorf("%s %s = %v", tc.mode, tc.host, got)
		}
	}
}

func TestValidationAndRules(t *testing.T) {
	for _, mac := range []string{"", "00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff", "01:00:00:00:00:01", "02:aa:bb:cc:dd:01; flush ruleset"} {
		if _, err := (Config{Mode: "updates", MAC: mac}).Validate(); err == nil {
			t.Fatalf("accepted %q", mac)
		}
	}
	c, err := (Config{Mode: "updates", MAC: "02:AA:BB:CC:DD:01"}).Validate()
	if err != nil || c.MAC != "02:aa:bb:cc:dd:01" {
		t.Fatal(c, err)
	}
	if _, err := (Config{Mode: "bogus", MAC: c.MAC}).Validate(); err == nil {
		t.Fatal("accepted unknown mode")
	}
	rules := c.Rules()
	if !strings.Contains(rules, "priority -110") || strings.Count(rules, "ether saddr") != strings.Count(rules, c.MAC) || strings.Contains(rules, "flush ruleset") {
		t.Fatal(rules)
	}
	if !strings.Contains(rules, c.Fingerprint()) {
		t.Fatal("missing firewall identity")
	}
	c.Mode = "all"
	if !strings.Contains(c.Rules(), "ether saddr "+c.MAC+" counter reject") || !strings.Contains(c.Rules(), "th dport != { 53, 67, 5354, 5443 }") {
		t.Fatal("full isolation missing")
	}
}

func realHello(t *testing.T) []byte {
	t.Helper()
	a, b := net.Pipe()
	defer b.Close()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	b.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer a.Close()
		tls.Client(a, &tls.Config{ServerName: "sun.p01.lp1.d4c.srv.nintendo.net"}).Handshake()
	}()
	h := make([]byte, 5)
	if _, err := io.ReadFull(b, h); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, int(binary.BigEndian.Uint16(h[3:])))
	if _, err := io.ReadFull(b, payload); err != nil {
		t.Fatal(err)
	}
	b.Close()
	<-done
	return append(h, payload...)
}

func TestTLSFragmentationAndPreservation(t *testing.T) {
	original := realHello(t)
	for _, chunk := range []int{1, 3, 47, 16384} {
		var wire []byte
		for b := original[5:]; len(b) > 0; {
			n := chunk
			if n > len(b) {
				n = len(b)
			}
			wire = append(wire, 22, 3, 3, byte(n>>8), byte(n))
			wire = append(wire, b[:n]...)
			b = b[n:]
		}
		raw, host, err := ReadClientHello(bytes.NewReader(wire))
		if err != nil || host != "sun.p01.lp1.d4c.srv.nintendo.net" || !bytes.Equal(raw, wire) {
			t.Fatalf("chunk %d: %q %v", chunk, host, err)
		}
	}
}

func TestMalformedHello(t *testing.T) {
	for _, wire := range [][]byte{{22, 3, 3, 0, 0}, {23, 3, 3, 0, 1, 1}, {22, 3, 3, 255, 255}, {22, 3, 3, 0, 5, 1, 0, 0, 1, 0}} {
		if _, _, err := ReadClientHello(bytes.NewReader(wire)); err == nil {
			t.Fatalf("accepted %x", wire)
		}
	}
	// Minimal valid ClientHello framing containing the encrypted ClientHello extension.
	b := make([]byte, 34)
	b = append(b, 0, 0, 2, 0x13, 1, 1, 0, 0, 4, 0xfe, 0x0d, 0, 0)
	if _, err := parseHello(b); err == nil {
		t.Fatal("accepted ECH")
	}
}

func TestHTTPPreservation(t *testing.T) {
	head := "GET /download HTTP/1.1\r\nHost: Atum.p01.lp1.d4c.srv.nintendo.net:80\r\n\r\n"
	r := strings.NewReader(head + "BODY")
	raw, host, err := ReadHTTPHost(r)
	if err != nil || string(raw) != head || host != "atum.p01.lp1.d4c.srv.nintendo.net" {
		t.Fatal(host, err)
	}
	body, _ := io.ReadAll(r)
	if string(body) != "BODY" {
		t.Fatal("consumed payload")
	}
	if _, _, err := ReadHTTPHost(strings.NewReader("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com\r\n\r\n")); err == nil {
		t.Fatal("accepted tunnel")
	}
}

type response struct{ msg *dns.Msg }

func (r *response) LocalAddr() net.Addr         { return &net.UDPAddr{} }
func (r *response) RemoteAddr() net.Addr        { return &net.UDPAddr{} }
func (r *response) WriteMsg(m *dns.Msg) error   { r.msg = m; return nil }
func (r *response) Write(b []byte) (int, error) { return len(b), nil }
func (r *response) Close() error                { return nil }
func (r *response) TsigStatus() error           { return nil }
func (r *response) TsigTimersOnly(bool)         {}
func (r *response) Hijack()                     {}

func TestDNSBlockingAndRingBound(t *testing.T) {
	s := &Service{config: Config{Mode: "updates"}}
	for i := 0; i < 40; i++ {
		q := new(dns.Msg)
		q.SetQuestion("sun.p01.lp1.d4c.srv.nintendo.net.", dns.TypeAAAA)
		r := new(response)
		s.dns(r, q)
		if r.msg == nil || r.msg.Rcode != dns.RcodeNameError || r.msg.Id != q.Id {
			t.Fatal("DNS block reply invalid")
		}
	}
	if s.dnsBlocked != 40 || len(s.events) != 32 {
		t.Fatal("unbounded history")
	}
}

func FuzzClientHello(f *testing.F) {
	f.Add([]byte{22, 3, 3, 0, 5, 1, 0, 0, 1, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		ReadClientHello(bytes.NewReader(b))
	})
}
