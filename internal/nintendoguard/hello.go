package nintendoguard

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxHello = 65536

// ReadClientHello preserves every byte for the upstream connection and supports
// ClientHello messages split across TCP packets and multiple TLS records.
func ReadClientHello(r io.Reader) (raw []byte, name string, err error) {
	var handshake []byte
	for len(raw) < maxHello {
		h := make([]byte, 5)
		if _, err = io.ReadFull(r, h); err != nil {
			return
		}
		if h[0] != 22 || h[1] != 3 {
			err = fmt.Errorf("not a TLS handshake")
			return
		}
		n := int(binary.BigEndian.Uint16(h[3:]))
		if n == 0 || n > 18432 || len(raw)+5+n > maxHello {
			err = fmt.Errorf("oversized TLS hello")
			return
		}
		b := make([]byte, n)
		if _, err = io.ReadFull(r, b); err != nil {
			return
		}
		raw = append(raw, h...)
		raw = append(raw, b...)
		handshake = append(handshake, b...)
		if len(handshake) < 4 {
			continue
		}
		if handshake[0] != 1 {
			err = fmt.Errorf("expected ClientHello")
			return
		}
		want := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
		if want > maxHello-4 {
			err = fmt.Errorf("oversized ClientHello")
			return
		}
		if len(handshake) < 4+want {
			continue
		}
		name, err = parseHello(handshake[4 : 4+want])
		return
	}
	err = fmt.Errorf("oversized ClientHello")
	return
}

func parseHello(b []byte) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("malformed ClientHello") }
	if len(b) < 35 {
		return bad()
	}
	b = b[34:]
	n := int(b[0])
	b = b[1:]
	if len(b) < n+2 {
		return bad()
	}
	b = b[n:]
	n = int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if n%2 != 0 || len(b) < n+1 {
		return bad()
	}
	b = b[n:]
	n = int(b[0])
	b = b[1:]
	if len(b) < n {
		return bad()
	}
	b = b[n:]
	if len(b) == 0 {
		return "", nil
	}
	if len(b) < 2 {
		return bad()
	}
	n = int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) != n {
		return bad()
	}
	var host string
	for len(b) > 0 {
		if len(b) < 4 {
			return bad()
		}
		kind, size := binary.BigEndian.Uint16(b), int(binary.BigEndian.Uint16(b[2:]))
		b = b[4:]
		if len(b) < size {
			return bad()
		}
		ext := b[:size]
		b = b[size:]
		// ECH conceals the domain. Active protection fails closed for ECH.
		if kind == 0xfe0d {
			return "", fmt.Errorf("encrypted ClientHello is not permitted")
		}
		if kind != 0 {
			continue
		}
		if host != "" || len(ext) < 2 || int(binary.BigEndian.Uint16(ext)) != len(ext)-2 {
			return bad()
		}
		ext = ext[2:]
		for len(ext) > 0 {
			if len(ext) < 3 {
				return bad()
			}
			t, z := ext[0], int(binary.BigEndian.Uint16(ext[1:]))
			ext = ext[3:]
			if len(ext) < z {
				return bad()
			}
			if t == 0 {
				if host != "" || z == 0 {
					return bad()
				}
				host = strings.ToLower(string(ext[:z]))
			}
			ext = ext[z:]
		}
	}
	if strings.ContainsAny(host, "\x00\r\n /\\") || len(host) > 253 {
		return bad()
	}
	return host, nil
}

func ReadHTTPHost(r io.Reader) ([]byte, string, error) {
	var raw []byte
	b := make([]byte, 1)
	for len(raw) < maxHello {
		if _, err := io.ReadFull(r, b); err != nil {
			return raw, "", err
		}
		raw = append(raw, b[0])
		if !bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
			continue
		}
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return raw, "", err
		}
		if req.Method == "CONNECT" {
			return raw, "", fmt.Errorf("HTTP CONNECT is not permitted")
		}
		host := req.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		return raw, strings.ToLower(host), nil
	}
	return raw, "", fmt.Errorf("oversized HTTP headers")
}
