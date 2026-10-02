//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modIPHelper             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = modIPHelper.NewProc("GetExtendedTcpTable")
)

const tcpTableOwnerPIDAll = 5

type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

var tcpStateNames = map[uint32]string{
	1: "Closed", 2: "Listen", 3: "SynSent", 4: "SynReceived", 5: "Established",
	6: "FinWait1", 7: "FinWait2", 8: "CloseWait", 9: "Closing", 10: "LastAck",
	11: "TimeWait", 12: "DeleteTCB",
}

// sampleTCPActivity lists TCP connections whose local address is the
// module's IPv4 address, i.e. traffic Windows is sending over cellular.
// Windows does not expose per-connection byte counters without enabling
// extended statistics as administrator, so byte fields stay zero.
func sampleTCPActivity(localIPv4, interfaceName string) ([]networkActivityRecord, error) {
	local := net.ParseIP(localIPv4).To4()
	if local == nil {
		return nil, errors.New("invalid local IPv4")
	}
	rows, err := extendedTCPTable()
	if err != nil {
		return nil, err
	}
	names := make(map[uint32]string)
	var records []networkActivityRecord
	for _, row := range rows {
		if row.State == 2 || row.State == 12 {
			continue
		}
		if !net.IP(uint32ToIPv4(row.LocalAddr)).Equal(local) {
			continue
		}
		remote := net.IP(uint32ToIPv4(row.RemoteAddr))
		if remote.IsUnspecified() || remote.IsLoopback() {
			continue
		}
		name, ok := names[row.OwningPID]
		if !ok {
			name = processName(row.OwningPID)
			names[row.OwningPID] = name
		}
		records = append(records, networkActivityRecord{
			Process:   name,
			IP:        remote.String(),
			Port:      strconv.Itoa(int(networkPort(row.RemotePort))),
			Protocol:  "tcp4",
			Interface: interfaceName,
			State:     tcpStateNames[row.State],
		})
	}
	return records, nil
}

func extendedTCPTable() ([]mibTCPRowOwnerPID, error) {
	if err := procGetExtendedTCPTable.Find(); err != nil {
		return nil, err
	}
	size := uint32(32 * 1024)
	for range 4 {
		buffer := make([]byte, size)
		ret, _, _ := procGetExtendedTCPTable.Call(
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(unsafe.Pointer(&size)),
			0,
			uintptr(windows.AF_INET),
			tcpTableOwnerPIDAll,
			0,
		)
		switch windows.Errno(ret) {
		case 0:
			count := binary.LittleEndian.Uint32(buffer[:4])
			rowSize := unsafe.Sizeof(mibTCPRowOwnerPID{})
			if uintptr(len(buffer)) < 4+uintptr(count)*rowSize {
				return nil, errors.New("TCP table is truncated")
			}
			rows := make([]mibTCPRowOwnerPID, count)
			for i := range rows {
				rows[i] = *(*mibTCPRowOwnerPID)(unsafe.Pointer(&buffer[4+uintptr(i)*rowSize]))
			}
			return rows, nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			size += 4096
			continue
		default:
			return nil, windows.Errno(ret)
		}
	}
	return nil, errors.New("TCP table kept growing")
}

// uint32ToIPv4 converts an address stored in network byte order.
func uint32ToIPv4(value uint32) []byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], value)
	return out[:]
}

func networkPort(value uint32) uint16 {
	return uint16(value&0xff)<<8 | uint16(value>>8&0xff)
}

func processName(pid uint32) string {
	switch pid {
	case 0:
		return "System Idle"
	case 4:
		return "System"
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "PID " + strconv.Itoa(int(pid))
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "PID " + strconv.Itoa(int(pid))
	}
	return strings.TrimSuffix(filepath.Base(windows.UTF16ToString(buf[:size])), ".exe")
}
