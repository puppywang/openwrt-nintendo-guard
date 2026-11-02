package nintendoguard

import (
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
)

// Return the router address on the route back to this client. UDP connect
// selects an address without sending a packet or contacting an external host.
func routerIPv4(w dns.ResponseWriter) net.IP {
	host, _, _ := net.SplitHostPort(w.LocalAddr().String())
	if ip := net.ParseIP(host).To4(); ip != nil && !ip.IsUnspecified() {
		return ip
	}
	host, _, err := net.SplitHostPort(w.RemoteAddr().String())
	if err != nil {
		return nil
	}
	ip := net.ParseIP(host).To4()
	if ip == nil || ip.IsUnspecified() {
		return nil
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: 9})
	if err != nil {
		return nil
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.To4()
}

func nintendoReply(req *dns.Msg, ip net.IP, blocked bool) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.RecursionAvailable = true
	for _, q := range req.Question {
		h := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: 60}
		switch q.Qtype {
		case dns.TypeA:
			if ip != nil {
				resp.Answer = append(resp.Answer, &dns.A{Hdr: h, A: ip.To4()})
			}
		case dns.TypeAAAA:
			if blocked {
				resp.Answer = append(resp.Answer, &dns.AAAA{Hdr: h, AAAA: net.IPv6zero})
			}
		}
	}
	return resp
}

func connectionTestHTTP(host string, request []byte) []byte {
	body, contentType := "ok", "text/plain"
	if normalizeHost(host) == "conntest.nintendowifi.net" {
		body = "<html><head><title>HTML Page</title></head><body>This is test.html page</body></html>"
		contentType = "text/html"
	}
	header := fmt.Sprintf("HTTP/1.1 200 OK\r\nX-Organization: Nintendo\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", contentType, len(body))
	if strings.HasPrefix(string(request), "HEAD ") {
		return []byte(header)
	}
	return []byte(header + body)
}
