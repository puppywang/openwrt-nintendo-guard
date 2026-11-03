package nintendoguard

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// A real upstream may return a Nintendo-owned intermediate CNAME. The nine
// documented test hosts must keep the genuine answer, but arbitrary aliases
// to Nintendo services must still be blocked.
func TestDocumentedTestAliasForwarding(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(q)
		h := dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}
		reply.Answer = []dns.RR{&dns.CNAME{Hdr: h, Target: "example.nintendo.net."}}
		_ = w.WriteMsg(reply)
	})}
	go func() { _ = server.ActivateAndServe() }()
	defer server.Shutdown()

	s := &Service{config: Config{Devices: []Device{{Mode: "nintendo"}}, Upstream: pc.LocalAddr().String()}}
	for _, host := range append([]string{"ordinary.example.org"}, PassThroughTestDomains...) {
		q := new(dns.Msg)
		q.SetQuestion(dns.Fqdn(host), dns.TypeA)
		w := new(localDNSResponse)
		s.dnsMode("nintendo", w, q)
		if w.msg == nil || w.msg.Rcode != dns.RcodeSuccess {
			t.Fatalf("%s: %v", host, w.msg)
		}
		if host == "ordinary.example.org" {
			if len(w.msg.Answer) != 1 || w.msg.Answer[0].(*dns.A).A.String() != "0.0.0.0" {
				t.Fatalf("generic alias bypassed block: %v", w.msg)
			}
		} else {
			if len(w.msg.Answer) != 1 {
				t.Fatalf("documented test alias blocked: %s: %v", host, w.msg)
			}
			if _, ok := w.msg.Answer[0].(*dns.CNAME); !ok {
				t.Fatalf("documented test alias rewritten: %s: %v", host, w.msg)
			}
		}
	}
	if s.dnsBlocked != 1 {
		t.Fatalf("unexpected number blocked: %d", s.dnsBlocked)
	}
}
