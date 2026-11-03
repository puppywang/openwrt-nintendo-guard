package nintendoguard

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestNintendoDomains(t *testing.T) {
	d := Device{Mode: "nintendo"}
	seen := make(map[string]bool)
	for _, domain := range NintendoDomains {
		if seen[domain] {
			t.Fatalf("duplicate domain %s", domain)
		}
		seen[domain] = true
		for _, host := range []string{domain, "service." + domain, strings.ToUpper(domain) + "."} {
			if !d.Blocks(host) {
				t.Fatalf("not blocked: %s", host)
			}
		}
		for _, host := range []string{"not" + domain, domain + ".example.org"} {
			if d.Blocks(host) {
				t.Fatalf("suffix boundary ignored: %s", host)
			}
		}
	}
	for _, host := range []string{"home.example.org", "github.com", "example.org", "example.amazonaws.com", "s3.us-east-1.amazonaws.com", "playstation.com", "google-analytics.com"} {
		if d.Blocks(host) {
			t.Fatalf("unrelated domain blocked: %s", host)
		}
	}
	for _, host := range ConnectionTestDomains {
		if d.Blocks(host) || connectionTest("not"+host) || !d.Blocks("not"+host) {
			t.Fatal("test exception too broad", host)
		}
	}
	// NintendoClients documents 9 current Switch 1/2 checks. The three
	// plain HTTP connectivity hosts are locally emulated; speed tests, IPv6
	// check and real HTTPS APIs are exact-host upstream exceptions.
	if len(ConnectionTestDomains) != 3 || len(PassThroughTestDomains) != 7 {
		t.Fatal("connection test inventory incomplete")
	}
	for _, host := range PassThroughTestDomains {
		if d.Blocks(host) || connectionTest(host) || !passThroughTest(host) || !testEndpoint(strings.ToUpper(host)+".") {
			t.Fatal("missing authentic upstream test exception", host)
		}
		for _, neighbor := range []string{"fake" + host, "sub." + host} {
			if !d.Blocks(neighbor) || testEndpoint(neighbor) {
				t.Fatal("connection test exception too broad", neighbor)
			}
		}
		if d.Blocks(host+".example.org") || testEndpoint(host+".example.org") {
			t.Fatal("hostname boundary violation", host)
		}
	}
	for _, host := range []string{"accounts.nintendo.com", "sun.p01.lp1.d4c.srv.nintendo.net", "api2.p01.lp1.ctest.srv.nintendo.net", "ctest-dl-notallow.p01.lp1.ctest.srv.nintendo.net"} {
		if !d.Blocks(host) {
			t.Fatal("unrelated Nintendo service accidentally allowed", host)
		}
	}
	if (Device{Mode: "updates"}).Blocks("ctest.p01.ctest.srv.nintendo.net") {
		t.Fatal("connection test blocked in updates-only mode")
	}
	c, err := (Config{Devices: []Device{{MAC: "02:aa:bb:cc:dd:01", Mode: "nintendo"}}}).Validate()
	if err != nil || !c.Active() {
		t.Fatal(c, err)
	}
	rules := c.Rules()
	if !strings.Contains(rules, "redirect to :5356") || !strings.Contains(rules, "redirect to :5444") || strings.Contains(rules, "ether saddr 02:aa:bb:cc:dd:01 counter reject") {
		t.Fatal(rules)
	}
}

type localDNSResponse struct{ response }

func (*localDNSResponse) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 5356}
}

func TestNintendoDNSReplies(t *testing.T) {
	s := &Service{config: Config{Devices: []Device{{Mode: "nintendo"}}}}
	for _, typ := range []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeHTTPS} {
		q := new(dns.Msg)
		q.SetQuestion("accounts.nintendo.com.", typ)
		w := new(localDNSResponse)
		s.dnsMode("nintendo", w, q)
		if w.msg.Rcode != dns.RcodeSuccess {
			t.Fatal(w.msg)
		}
		switch typ {
		case dns.TypeA:
			if len(w.msg.Answer) != 1 || !w.msg.Answer[0].(*dns.A).A.Equal(net.IPv4zero) {
				t.Fatal(w.msg)
			}
		case dns.TypeAAAA:
			if len(w.msg.Answer) != 1 || !w.msg.Answer[0].(*dns.AAAA).AAAA.Equal(net.IPv6zero) {
				t.Fatal(w.msg)
			}
		default:
			if len(w.msg.Answer) != 0 {
				t.Fatal("unsupported type forwarded", w.msg)
			}
		}
	}
	for _, host := range ConnectionTestDomains {
		q := new(dns.Msg)
		q.SetQuestion(host+".", dns.TypeA)
		w := new(localDNSResponse)
		s.dnsMode("nintendo", w, q)
		if len(w.msg.Answer) != 1 || !w.msg.Answer[0].(*dns.A).A.Equal(net.ParseIP("192.0.2.1")) {
			t.Fatal(w.msg)
		}
	}
	if s.dnsBlocked != 3 {
		t.Fatal("connection-test emulation counted as blocking")
	}
	q := new(dns.Msg)
	w := new(localDNSResponse)
	s.dnsMode("nintendo", w, q)
	if w.msg.Rcode != dns.RcodeFormatError {
		t.Fatal("invalid query accepted")
	}
}

func TestConnectionTestHTTP(t *testing.T) {
	if raw := connectionTestHTTP("ctest.p01.ctest.srv.nintendo.net", []byte("POST /1m HTTP/1.1\r\n\r\n")); !bytes.HasPrefix(raw, []byte("HTTP/1.1 405 Method Not Allowed")) {
		t.Fatal("local connectivity handler must not impersonate a speed-test upload")
	}
	for _, host := range ConnectionTestDomains {
		for _, method := range []string{"GET", "HEAD"} {
			req, _ := http.NewRequest(method, "http://"+host+"/", nil)
			raw := connectionTestHTTP(host, []byte(method+" / HTTP/1.1\r\n\r\n"))
			resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || resp.Header.Get("X-Organization") != "Nintendo" || resp.Header.Get("Cache-Control") != "max-age=0, no-cache, no-store" || resp.Header.Get("Pragma") != "no-cache" || resp.Header.Get("Date") == "" || resp.Header.Get("Expires") == "" {
				t.Fatal(resp, err)
			}
			if host != "conntest.nintendowifi.net" && (resp.Header.Get("Content-Type") != "text/plain" || resp.ContentLength != 2) {
				t.Fatal("connection test expected text/plain ok", resp)
			}
			if method == "GET" && len(body) == 0 {
				t.Fatal("empty connection-test response")
			}
			if method == "GET" && host == "ctest.p01.ctest.srv.nintendo.net" && string(body) != "ok" {
				t.Fatalf("Switch 2 expects an exact 'ok' response, got %q", body)
			}
			if method == "HEAD" && len(body) != 0 {
				t.Fatal("HEAD returned a body")
			}
		}
	}
}
