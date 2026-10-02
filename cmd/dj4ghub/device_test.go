package main

import "testing"

// Nodes as observed on Windows 11 with Quectel drivers and usbnet=0.
func observedDJINodes() []pnpDevice {
	return []pnpDevice{
		{InstanceID: `USB\VID_2CA3&PID_4006\9&18F8D694&0&2`, FriendlyName: "Quectel USB Composite Device (0046)", Manufacturer: "Quectel", Class: "USB", Location: "Port_#0002.Hub_#0003"},
		{InstanceID: `USB\VID_2CA3&PID_4006&MI_00\A&9F4DAC1&0&0000`, FriendlyName: "Quectel USB DM Port (COM19)", Class: "Ports", PortName: "COM19", CompatibleIDs: []string{`USB\Class_FF&SubClass_FF&Prot_30`, `USB\Class_FF`}},
		{InstanceID: `USB\VID_2CA3&PID_4006&MI_01\A&9F4DAC1&0&0001`, FriendlyName: "Quectel USB NMEA Port (COM18)", Class: "Ports", PortName: "COM18", CompatibleIDs: []string{`USB\Class_FF&SubClass_00&Prot_00`}},
		{InstanceID: `USB\VID_2CA3&PID_4006&MI_02\A&9F4DAC1&0&0002`, FriendlyName: "Quectel USB AT Port (COM17)", Class: "Ports", PortName: "COM17", CompatibleIDs: []string{`USB\Class_FF&SubClass_00&Prot_00`}},
		{InstanceID: `USB\VID_2CA3&PID_4006&MI_03\A&9F4DAC1&0&0003`, Description: "Quectel USB Modem", Class: "Modem", PortName: "COM20", CompatibleIDs: []string{`USB\Class_FF&SubClass_00&Prot_00`}},
		{InstanceID: `USB\VID_2CA3&PID_4006&MI_04\A&9F4DAC1&0&0004`, FriendlyName: "Baiwang Wireless Ethernet Adapter #3", Class: "Net", NetCfgInstanceID: "{dd33297b-510d-411a-ac11-9c82778e9353}", CompatibleIDs: []string{`USB\Class_FF&SubClass_FF&Prot_FF`}},
		{InstanceID: `USB\VID_2C7C&PID_0125&MI_02\B&1&0002`, FriendlyName: "Quectel USB AT Port (COM5)", Class: "Ports", PortName: "COM5"},
	}
}

func TestBuildUSBDeviceStatusPicksATPortAndAdapter(t *testing.T) {
	device := buildUSBDeviceStatus(observedDJINodes())
	if device == nil {
		t.Fatal("device not found")
	}
	if device.ATPort != "COM17" {
		t.Fatalf("AT port = %q, want COM17", device.ATPort)
	}
	if device.VendorID != "2ca3" || device.ProductID != "4006" || device.Vendor != "Quectel" {
		t.Fatalf("identity = %+v", device)
	}
	if len(device.Interfaces) != 5 || device.Interfaces[4].Number != 4 || device.Interfaces[0].Protocol != 0x30 {
		t.Fatalf("interfaces = %+v", device.Interfaces)
	}
	ports := atPortCandidates(device.Interfaces)
	if len(ports) != 2 || ports[0] != "COM17" || ports[1] != "COM20" {
		t.Fatalf("AT candidates = %q; DM and NMEA ports must never be probed", ports)
	}
	if ids := device.networkAdapterIDs(); len(ids) != 1 || ids[0] != "{DD33297B-510D-411A-AC11-9C82778E9353}" {
		t.Fatalf("adapter IDs = %q", ids)
	}
}

func TestBuildUSBDeviceStatusReportsMissingDriver(t *testing.T) {
	nodes := observedDJINodes()[:1]
	nodes = append(nodes, pnpDevice{InstanceID: `USB\VID_2CA3&PID_4006&MI_02\A&1&0002`, Description: "Baiwang", Problem: cmProbFailedInstall})
	device := buildUSBDeviceStatus(nodes)
	if device.ATPort != "" || len(device.DriverIssues) != 1 {
		t.Fatalf("device = %+v", device)
	}
}

func TestBuildUSBDeviceStatusIgnoresOtherVendors(t *testing.T) {
	if device := buildUSBDeviceStatus(observedDJINodes()[6:]); device != nil {
		t.Fatalf("foreign modem reported as DJI: %+v", device)
	}
}

func TestCurrentDJINetworkServiceMatchesAdapterID(t *testing.T) {
	device := buildUSBDeviceStatus(observedDJINodes())
	interfaces := []hostNetInterface{
		{Name: "乙太網路 2", AdapterID: "{11111111-0000-0000-0000-000000000000}", Kind: "ethernet", Status: "active", IPv4: "192.168.50.7"},
		{Name: "行動電話 5", Description: "Baiwang Wireless Ethernet Adapter #3", AdapterID: "{DD33297B-510D-411A-AC11-9C82778E9353}", Kind: "wwan", Status: "active", IPv4: "10.18.22.217", Index: 29},
	}
	service := currentDJINetworkService(device, interfaces)
	if service == nil || service.Name != "行動電話 5" || service.Kind != "wwan" || service.Index != 29 {
		t.Fatalf("service = %+v", service)
	}
	if !networkServiceReady(service) {
		t.Fatal("service with a usable address should be ready")
	}
	interfaces[1].IPv4 = "169.254.10.2"
	if networkServiceReady(currentDJINetworkService(device, interfaces)) {
		t.Fatal("APIPA address must not count as ready")
	}
}

func TestCurrentDJINetworkServiceReportsDisabledAdapter(t *testing.T) {
	nodes := observedDJINodes()
	nodes[5].Problem = cmProbDisabled
	device := buildUSBDeviceStatus(nodes)
	service := currentDJINetworkService(device, nil)
	if service == nil || !service.Disabled || networkServiceReady(service) {
		t.Fatalf("service = %+v", service)
	}
	if len(device.DriverIssues) != 0 {
		t.Fatalf("a disabled adapter is not a driver issue: %q", device.DriverIssues)
	}
}
