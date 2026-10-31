//go:build !linux

package nintendoguard

import (
	"fmt"
	"net"
)

func OriginalDestination(c *net.TCPConn) (string, error) {
	return "", fmt.Errorf("transparent connections require Linux")
}
