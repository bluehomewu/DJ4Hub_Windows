//go:build windows

package main

import (
	"encoding/binary"
	"syscall"
)

// ipUnicastIF is IP_UNICAST_IF. Windows expects the IPv4 interface index in
// network byte order.
const ipUnicastIF = 31

func bindSocketToInterface(fd uintptr, index int) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(index))
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, ipUnicastIF, int(binary.NativeEndian.Uint32(buf[:])))
}
