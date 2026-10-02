package main

import (
	"net"
	"strings"
)

// hostNetInterface is one Windows network adapter as reported by IP Helper.
type hostNetInterface struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Index       int    `json:"index"`
	AdapterID   string `json:"adapter_id,omitempty"`
	Status      string `json:"status"`
	IPv4        string `json:"ipv4"`
	Gateway     string `json:"gateway,omitempty"`
	Kind        string `json:"kind"`
}

type hostDefaultRoute struct {
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
}

// hostNetworkService describes the Windows adapter that belongs to the
// connected DJI module. Kind is "wwan" for the Mobile Broadband interface the
// Quectel NDIS/MBIM driver provides, or "ethernet" for ECM/RNDIS modes.
type hostNetworkService struct {
	Name             string `json:"name"`
	HardwarePort     string `json:"hardware_port"`
	Device           string `json:"device"`
	Kind             string `json:"kind"`
	Index            int    `json:"index,omitempty"`
	Disabled         bool   `json:"disabled"`
	InterfacePresent bool   `json:"interface_present"`
	InterfaceStatus  string `json:"interface_status,omitempty"`
	IPv4             string `json:"ipv4,omitempty"`
}

type networkServiceRepairResult struct {
	Ready          bool                `json:"ready"`
	Summary        string              `json:"summary"`
	NetworkService *hostNetworkService `json:"network_service,omitempty"`
}

// usableIPv4 rejects addresses that do not prove a working data session,
// such as APIPA 169.254.x.x addresses assigned while DHCP is still failing.
func usableIPv4(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value)).To4()
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	return true
}

func normalizeAdapterID(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value != "" && !strings.HasPrefix(value, "{") {
		value = "{" + value + "}"
	}
	return value
}

// currentDJINetworkService maps the module's PnP network function to its
// Windows adapter by NetCfgInstanceId, so other USB modems or phones
// tethering over RNDIS are never mistaken for the DJI module.
func currentDJINetworkService(device *usbDeviceStatus, interfaces []hostNetInterface) *hostNetworkService {
	if device == nil {
		return nil
	}
	ids := make(map[string]bool)
	for _, id := range device.networkAdapterIDs() {
		ids[normalizeAdapterID(id)] = true
	}
	var best *hostNetworkService
	for _, item := range interfaces {
		if !ids[normalizeAdapterID(item.AdapterID)] {
			continue
		}
		candidate := &hostNetworkService{
			Name:             item.Name,
			HardwarePort:     item.Description,
			Device:           item.Name,
			Kind:             item.Kind,
			Index:            item.Index,
			InterfacePresent: true,
			InterfaceStatus:  item.Status,
			IPv4:             item.IPv4,
		}
		if best == nil || (networkServiceReady(candidate) && !networkServiceReady(best)) {
			best = candidate
		}
	}
	if best != nil {
		return best
	}
	if iface, disabled := device.disabledNetworkFunction(); disabled {
		return &hostNetworkService{
			Name:         iface.Name,
			HardwarePort: iface.Name,
			Kind:         "unknown",
			Disabled:     true,
		}
	}
	return nil
}

func networkServiceReady(service *hostNetworkService) bool {
	return service != nil && !service.Disabled && service.InterfacePresent &&
		service.InterfaceStatus == "active" && usableIPv4(service.IPv4)
}

func findHostInterface(interfaces []hostNetInterface, name string) (hostNetInterface, bool) {
	for _, item := range interfaces {
		if item.Name == name {
			return item, true
		}
	}
	return hostNetInterface{}, false
}

func classifyInterfaceType(ifType uint32) string {
	switch ifType {
	case 6:
		return "ethernet"
	case 71:
		return "wifi"
	case 243, 244:
		return "wwan"
	case 131, 23:
		return "tunnel"
	case 24:
		return "loopback"
	default:
		return "other"
	}
}

func sessionTrafficFromCounters(current, baseline networkByteCounters) (rx, tx, total uint64) {
	rx = current.RX - baseline.RX
	tx = current.TX - baseline.TX
	return rx, tx, rx + tx
}
