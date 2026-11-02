package nintendoguard

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestPortableConfig(t *testing.T) {
	c, err := (Config{Mode: "off"}).Validate()
	if err != nil || c.Upstream != "127.0.0.1:53" {
		t.Fatal(c, err)
	}
	for _, value := range []string{"localhost:53", "127.0.0.1:5354", "127.0.0.1:5355", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:53; reboot"} {
		if _, err := (Config{Mode: "off", Upstream: value}).Validate(); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"127.0.0.1:5335", "[::1]:53", "192.0.2.53:53"} {
		if _, err := (Config{Mode: "off", Upstream: value}).Validate(); err != nil {
			t.Error(err)
		}
	}
	if _, err := (Config{Mode: "updates"}).Validate(); err == nil {
		t.Fatal("active mode accepted empty target")
	}
}

func TestOffListenerRefusesDirectQueries(t *testing.T) {
	s := &Service{config: Config{Mode: "off"}}
	q := new(dns.Msg)
	q.SetQuestion("example.org.", dns.TypeA)
	w := new(response)
	s.dns(w, q)
	if w.msg.Rcode != dns.RcodeRefused {
		t.Fatal("off listener became a resolver")
	}
}

func TestUpstreamTCPFallback(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	readyUDP, readyTCP := make(chan struct{}), make(chan struct{})
	udpServer := &dns.Server{PacketConn: udp, NotifyStartedFunc: func() { close(readyUDP) }, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		r := new(dns.Msg)
		r.SetReply(q)
		r.Truncated = true
		w.WriteMsg(r)
	})}
	tcpServer := &dns.Server{Listener: tcp, NotifyStartedFunc: func() { close(readyTCP) }, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		r := new(dns.Msg)
		r.SetReply(q)
		rr, _ := dns.NewRR(q.Question[0].Name + " 60 IN A 192.0.2.123")
		r.Answer = []dns.RR{rr}
		w.WriteMsg(r)
	})}
	go udpServer.ActivateAndServe()
	go tcpServer.ActivateAndServe()
	defer udpServer.Shutdown()
	defer tcpServer.Shutdown()
	for _, ready := range []chan struct{}{readyUDP, readyTCP} {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("DNS test server not ready")
		}
	}
	s := &Service{config: Config{Devices: []Device{{Mode: "updates"}, {Mode: "all"}}, Upstream: tcp.Addr().String()}}
	q := new(dns.Msg)
	q.SetQuestion("atum.p01.lp1.d4c.srv.nintendo.net.", dns.TypeA)
	w := new(response)
	s.dns(w, q)
	if w.msg == nil || w.msg.Truncated || w.msg.Id != q.Id || len(w.msg.Answer) != 1 {
		t.Fatal("upstream TCP fallback failed", w.msg)
	}
}
