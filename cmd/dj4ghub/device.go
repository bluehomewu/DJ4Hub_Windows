package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	djiUSBVendorID  = 0x2ca3
	djiUSBProductID = 0x4006

	// cmProbDisabled is CM_PROB_DISABLED: the user disabled the device.
	cmProbDisabled = 22
	// cmProbFailedInstall is CM_PROB_FAILED_INSTALL: no driver is installed.
	cmProbFailedInstall = 28
)

// pnpDevice is one present Plug and Play node that belongs to a DJI module.
// The composite parent and each USB interface (MI_xx) are separate nodes.
type pnpDevice struct {
	InstanceID       string
	FriendlyName     string
	Description      string
	Manufacturer     string
	Class            string
	Location         string
	CompatibleIDs    []string
	PortName         string
	NetCfgInstanceID string
	Problem          uint32
}

var (
	pnpVIDPattern   = regexp.MustCompile(`(?i)\bVID_([0-9A-F]{4})`)
	pnpPIDPattern   = regexp.MustCompile(`(?i)\bPID_([0-9A-F]{4})`)
	pnpMIPattern    = regexp.MustCompile(`(?i)&MI_([0-9A-F]{2})`)
	pnpClassPattern = regexp.MustCompile(`(?i)\bClass_([0-9A-F]{2})(?:&SubClass_([0-9A-F]{2}))?(?:&Prot_([0-9A-F]{2}))?`)
)

func pnpVendorProduct(instanceID string) (vendor, product int, ok bool) {
	vid := pnpVIDPattern.FindStringSubmatch(instanceID)
	pid := pnpPIDPattern.FindStringSubmatch(instanceID)
	if len(vid) != 2 || len(pid) != 2 {
		return 0, 0, false
	}
	v, errV := strconv.ParseUint(vid[1], 16, 16)
	p, errP := strconv.ParseUint(pid[1], 16, 16)
	if errV != nil || errP != nil {
		return 0, 0, false
	}
	return int(v), int(p), true
}

// pnpInterfaceNumber returns the USB interface number of an MI_xx child node.
func pnpInterfaceNumber(instanceID string) (int, bool) {
	match := pnpMIPattern.FindStringSubmatch(instanceID)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseUint(match[1], 16, 8)
	return int(value), err == nil
}

// pnpInterfaceClass reads the USB class triple from compatible IDs such as
// USB\Class_FF&SubClass_FF&Prot_30.
func pnpInterfaceClass(compatibleIDs []string) (class, subclass, protocol int) {
	for _, id := range compatibleIDs {
		match := pnpClassPattern.FindStringSubmatch(id)
		if len(match) != 4 {
			continue
		}
		class = parseHexOrZero(match[1])
		subclass = parseHexOrZero(match[2])
		protocol = parseHexOrZero(match[3])
		return class, subclass, protocol
	}
	return 0, 0, 0
}

func parseHexOrZero(value string) int {
	parsed, err := strconv.ParseUint(value, 16, 8)
	if err != nil {
		return 0
	}
	return int(parsed)
}

func pnpDisplayName(device pnpDevice) string {
	if name := strings.TrimSpace(device.FriendlyName); name != "" {
		return name
	}
	return strings.TrimSpace(device.Description)
}

// atPortScore ranks serial ports exposed by the module. Quectel drivers name
// the command interface "AT Port"; diagnostic and GNSS ports must never be
// probed with AT traffic.
func atPortScore(name string) int {
	upper := strings.ToUpper(name)
	switch {
	case strings.Contains(upper, " DM ") || strings.Contains(upper, "DIAG") ||
		strings.Contains(upper, "NMEA") || strings.Contains(upper, "GPS") ||
		strings.Contains(upper, "GNSS") || strings.Contains(upper, "DM PORT"):
		return -1
	case strings.Contains(upper, "AT PORT"):
		return 100
	case strings.Contains(upper, "MODEM"):
		return 40
	default:
		return 10
	}
}

// buildUSBDeviceStatus folds the PnP nodes of one module into the status
// shape used by the web console. Nodes from other modules are ignored.
func buildUSBDeviceStatus(nodes []pnpDevice) *usbDeviceStatus {
	var device *usbDeviceStatus
	var root *pnpDevice
	for i := range nodes {
		vendor, _, ok := pnpVendorProduct(nodes[i].InstanceID)
		if !ok || vendor != djiUSBVendorID {
			continue
		}
		if _, isInterface := pnpInterfaceNumber(nodes[i].InstanceID); !isInterface && root == nil {
			root = &nodes[i]
		}
	}
	for _, node := range nodes {
		vendor, product, ok := pnpVendorProduct(node.InstanceID)
		if !ok || vendor != djiUSBVendorID {
			continue
		}
		if device == nil {
			device = &usbDeviceStatus{
				Vendor:    "DJI",
				VendorID:  fmt.Sprintf("%04x", vendor),
				ProductID: fmt.Sprintf("%04x", product),
				Product:   "DJI 4G Module",
				Mode:      "vendor-specific USB mode",
			}
			if root != nil {
				device.InstanceID = root.InstanceID
				device.LocationID = root.Location
				if name := pnpDisplayName(*root); name != "" {
					device.Product = name
				}
				if mfg := strings.TrimSpace(root.Manufacturer); mfg != "" && !strings.HasPrefix(mfg, "(") {
					device.Vendor = mfg
				}
			}
		}
		number, isInterface := pnpInterfaceNumber(node.InstanceID)
		if !isInterface {
			if node.Problem != 0 {
				device.DriverIssues = append(device.DriverIssues, driverIssueText(node))
			}
			continue
		}
		class, subclass, protocol := pnpInterfaceClass(node.CompatibleIDs)
		iface := usbInterfaceStatus{
			Number:      number,
			Class:       class,
			Subclass:    subclass,
			Protocol:    protocol,
			Name:        pnpDisplayName(node),
			DriverClass: node.Class,
			Port:        node.PortName,
			Problem:     node.Problem,
		}
		if node.NetCfgInstanceID != "" {
			iface.NetworkAdapter = strings.ToUpper(node.NetCfgInstanceID)
		}
		if node.Problem != 0 && node.Problem != cmProbDisabled {
			device.DriverIssues = append(device.DriverIssues, driverIssueText(node))
		}
		device.Interfaces = append(device.Interfaces, iface)
	}
	if device == nil {
		return nil
	}
	sort.SliceStable(device.Interfaces, func(i, j int) bool {
		return device.Interfaces[i].Number < device.Interfaces[j].Number
	})
	device.ATPort = selectATPort(device.Interfaces)
	switch {
	case hasECMUSBInterfaces(device.Interfaces):
		device.Mode = "CDC ECM USB network mode"
	case allVendorSpecific(device.Interfaces):
		device.Mode = "vendor-specific QMI/diagnostic mode"
	}
	return device
}

func driverIssueText(node pnpDevice) string {
	name := pnpDisplayName(node)
	if name == "" {
		name = node.InstanceID
	}
	switch node.Problem {
	case cmProbFailedInstall:
		return fmt.Sprintf("%s 未安装驱动程序", name)
	case cmProbDisabled:
		return fmt.Sprintf("%s 已在 Windows 中停用", name)
	default:
		return fmt.Sprintf("%s 驱动异常（代码 %d）", name, node.Problem)
	}
}

// atPortCandidates lists serial ports worth probing, best first.
func atPortCandidates(interfaces []usbInterfaceStatus) []string {
	type candidate struct {
		port  string
		score int
	}
	var candidates []candidate
	for _, iface := range interfaces {
		if iface.Port == "" || iface.Problem != 0 {
			continue
		}
		score := atPortScore(iface.Name)
		if score < 0 {
			continue
		}
		candidates = append(candidates, candidate{port: iface.Port, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	ports := make([]string, 0, len(candidates))
	for _, item := range candidates {
		ports = append(ports, item.port)
	}
	return ports
}

func selectATPort(interfaces []usbInterfaceStatus) string {
	if ports := atPortCandidates(interfaces); len(ports) > 0 {
		return ports[0]
	}
	return ""
}

func allVendorSpecific(interfaces []usbInterfaceStatus) bool {
	if len(interfaces) == 0 {
		return false
	}
	for _, iface := range interfaces {
		if iface.Class != 255 {
			return false
		}
	}
	return true
}

func hasECMUSBInterfaces(interfaces []usbInterfaceStatus) bool {
	var control, data bool
	for _, item := range interfaces {
		switch item.Class {
		case 2:
			control = true
		case 10:
			data = true
		}
	}
	return control && data
}

// networkAdapterIDs returns the NetCfgInstanceId GUIDs of the module's
// network functions.
func (d *usbDeviceStatus) networkAdapterIDs() []string {
	if d == nil {
		return nil
	}
	var ids []string
	for _, iface := range d.Interfaces {
		if iface.NetworkAdapter != "" {
			ids = append(ids, iface.NetworkAdapter)
		}
	}
	return ids
}

// disabledNetworkFunction reports a network function the user disabled in
// Device Manager. Windows hides such adapters from the IP Helper API.
func (d *usbDeviceStatus) disabledNetworkFunction() (usbInterfaceStatus, bool) {
	if d == nil {
		return usbInterfaceStatus{}, false
	}
	for _, iface := range d.Interfaces {
		if strings.EqualFold(iface.DriverClass, "Net") && iface.Problem == cmProbDisabled {
			return iface, true
		}
	}
	return usbInterfaceStatus{}, false
}
