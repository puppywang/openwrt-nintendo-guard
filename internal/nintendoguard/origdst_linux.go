//go:build linux

package nintendoguard

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

func OriginalDestination(c *net.TCPConn) (string, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return "", err
	}
	var address [16]byte
	length := uint32(len(address))
	var callErr error
	err = raw.Control(func(fd uintptr) {
		_, _, e := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_IP, 80, uintptr(unsafe.Pointer(&address[0])), uintptr(unsafe.Pointer(&length)), 0)
		if e != 0 {
			callErr = e
		}
	})
	if err != nil {
		return "", err
	}
	if callErr != nil {
		return "", callErr
	}
	port := int(binary.BigEndian.Uint16(address[2:4]))
	if port != 80 && port != 443 {
		return "", fmt.Errorf("connection was not redirected from HTTP/HTTPS")
	}
	return net.JoinHostPort(net.IP(address[4:8]).String(), strconv.Itoa(port)), nil
}
