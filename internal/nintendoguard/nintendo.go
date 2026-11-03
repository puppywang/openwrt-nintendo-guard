package nintendoguard

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

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
	// These are the connection-check responses documented by NintendoClients.
	// The example's fixed historical Date/Expires cannot be reused: use the
	// current HTTP date and equivalent non-cacheable semantics instead.
	method := strings.SplitN(string(request), " ", 2)[0]
	if method != "GET" && method != "HEAD" {
		return []byte("HTTP/1.1 405 Method Not Allowed\r\nAllow: GET, HEAD\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	}
	body, contentType := "ok", "text/plain"
	if normalizeHost(host) == "conntest.nintendowifi.net" {
		body = "<html><head><title>HTML Page</title></head><body>This is test.html page</body></html>"
		contentType = "text/html"
	}
	now := time.Now().UTC().Format(http.TimeFormat)
	header := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\nExpires: %s\r\nCache-Control: max-age=0, no-cache, no-store\r\nPragma: no-cache\r\nDate: %s\r\nConnection: close\r\nX-Organization: Nintendo\r\nContent-Type: %s\r\n\r\n", len(body), now, now, contentType)
	// Connection: close is truthful: the transparent proxy handles one request
	// per socket, unlike the persistent sample connection. GET/HEAD response
	// status, content and cache semantics still match the protocol.
	if method == "HEAD" {
		return []byte(header)
	}
	return []byte(header + body)
}
