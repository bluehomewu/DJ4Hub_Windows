//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

func discoverHostNetworkInterfaces() []hostNetInterface {
	adapters, err := adapterAddresses()
	if err != nil {
		return nil
	}
	var interfaces []hostNetInterface
	for adapter := adapters; adapter != nil; adapter = adapter.Next {
		kind := classifyInterfaceType(adapter.IfType)
		if kind == "loopback" {
			continue
		}
		item := hostNetInterface{
			Name:        windows.UTF16PtrToString(adapter.FriendlyName),
			Description: windows.UTF16PtrToString(adapter.Description),
			Index:       int(adapter.IfIndex),
			AdapterID:   normalizeAdapterID(windows.BytePtrToString(adapter.AdapterName)),
			Status:      "inactive",
			Kind:        kind,
		}
		if adapter.OperStatus == windows.IfOperStatusUp {
			item.Status = "active"
		}
		for address := adapter.FirstUnicastAddress; address != nil; address = address.Next {
			if ip := address.Address.IP().To4(); ip != nil {
				item.IPv4 = ip.String()
				break
			}
		}
		for gateway := adapter.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
			if ip := gateway.Address.IP().To4(); ip != nil {
				item.Gateway = ip.String()
				break
			}
		}
		interfaces = append(interfaces, item)
	}
	return interfaces
}

func adapterAddresses() (*windows.IpAdapterAddresses, error) {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER
	size := uint32(16 * 1024)
	for range 4 {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if err == nil {
			return first, nil
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return nil, err
		}
	}
	return nil, errors.New("GetAdaptersAddresses buffer kept growing")
}

// discoverHostDefaultRoute asks Windows which interface would carry traffic
// to a public address, matching what applications actually use.
func discoverHostDefaultRoute() hostDefaultRoute {
	var index uint32
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}, &index); err != nil {
		return hostDefaultRoute{}
	}
	for _, item := range discoverHostNetworkInterfaces() {
		if item.Index == int(index) {
			return hostDefaultRoute{Interface: item.Name, Gateway: item.Gateway}
		}
	}
	return hostDefaultRoute{}
}

func discoverInterfaceCounters(index int) (networkByteCounters, error) {
	row := windows.MibIfRow2{InterfaceIndex: uint32(index)}
	if err := windows.GetIfEntry2Ex(windows.MibIfEntryNormal, &row); err != nil {
		return networkByteCounters{}, fmt.Errorf("讀取網卡計數: %w", err)
	}
	return networkByteCounters{RX: row.InOctets, TX: row.OutOctets}, nil
}

// runHidden runs a console tool without flashing a window and decodes its
// output, which follows the console code page unless the tool emits UTF-8.
func runHidden(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := command.CombinedOutput()
	return decodeConsoleOutput(output), err
}

func decodeConsoleOutput(output []byte) string {
	if utf8.Valid(output) {
		return string(output)
	}
	codePage, err := windows.GetConsoleOutputCP()
	if err != nil || codePage == 0 {
		codePage = windows.GetACP()
	}
	n, err := windows.MultiByteToWideChar(codePage, 0, &output[0], int32(len(output)), nil, 0)
	if err != nil || n == 0 {
		return string(output)
	}
	wide := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(codePage, 0, &output[0], int32(len(output)), &wide[0], n); err != nil {
		return string(output)
	}
	return windows.UTF16ToString(wide)
}

// wwanProfiles lists the Mobile Broadband profiles of an interface. The
// header text is localized, so only the indented name lines are parsed.
func wwanProfiles(ctx context.Context, interfaceName string) ([]string, error) {
	// netsh may exit non-zero even after listing profiles, so trust the output.
	output, err := runHidden(ctx, "netsh", "mbn", "show", "profiles", "interface="+interfaceName)
	profiles := parseWWANProfiles(output)
	if err != nil && len(profiles) == 0 && !strings.Contains(output, "---") {
		return nil, fmt.Errorf("讀取行動寬頻設定檔失敗: %s", strings.TrimSpace(output))
	}
	return profiles, nil
}

func connectWWAN(ctx context.Context, interfaceName, profile string) error {
	output, err := runHidden(ctx, "netsh", "mbn", "connect", "interface="+interfaceName, "connmode=name", "name="+profile)
	if err != nil {
		return fmt.Errorf("Windows 拒絕連線行動寬頻: %s", strings.TrimSpace(output))
	}
	return nil
}

// enableAdapterElevated re-enables an adapter that was disabled in Windows.
// This needs administrator rights, so Windows shows a UAC prompt.
func enableAdapterElevated(ctx context.Context, interfaceName string) error {
	script := "Start-Process -FilePath netsh.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList @('interface','set','interface'," +
		powershellQuote("name="+interfaceName) + ",'admin=enabled')"
	output, err := runHidden(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("等待 Windows 管理員授權逾時")
		}
		detail := strings.TrimSpace(output)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("啟用 Windows 網卡失敗（可能已取消 UAC 授權）: %s", detail)
	}
	return nil
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// enableDJINetworkService makes the module's Windows adapter usable: enable
// it when disabled, or connect the Mobile Broadband interface when idle.
func enableDJINetworkService(service *hostNetworkService) error {
	if service == nil {
		return errors.New("network service is empty")
	}
	if service.Disabled {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return enableAdapterElevated(ctx, service.Name)
	}
	if service.Kind != "wwan" {
		return errors.New("該網卡由 Windows 自動透過 DHCP 取得位址；請檢查模組撥號狀態或重新插拔")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	profiles, err := wwanProfiles(ctx, service.Name)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return errors.New("Windows 沒有可用的行動寬頻設定檔；請在 Windows 設定 → 網路和 Internet → 手機網路中新增 APN")
	}
	var lastErr error
	for _, profile := range profiles {
		if lastErr = connectWWAN(ctx, service.Name, profile); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func waitForDJINetworkService(device *usbDeviceStatus, timeout time.Duration) *hostNetworkService {
	deadline := time.Now().Add(timeout)
	for {
		if current := discoverDJIUSBDevice(); current != nil {
			device = current
		}
		service := currentDJINetworkService(device, discoverHostNetworkInterfaces())
		if networkServiceReady(service) || time.Now().After(deadline) {
			return service
		}
		time.Sleep(500 * time.Millisecond)
	}
}
