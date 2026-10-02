//go:build windows && hardware

package main

// Read-only smoke tests against a connected module:
//
//	go test -tags hardware -run Hardware -v ./cmd/dj4ghub
//
// Only query commands are sent; nothing is written to the module or SIM.

import (
	"testing"
	"time"
)

func TestHardwareDiscoveryAndATQueries(t *testing.T) {
	device := discoverDJIUSBDevice()
	if device == nil {
		t.Skip("no DJI module connected")
	}
	t.Logf("device: %s %s (%s:%s) location=%q mode=%q AT=%q", device.Vendor, device.Product, device.VendorID, device.ProductID, device.LocationID, device.Mode, device.ATPort)
	for _, iface := range device.Interfaces {
		t.Logf("  MI_%02X class=%02x/%02x/%02x %s %s %s problem=%d", iface.Number, iface.Class, iface.Subclass, iface.Protocol, iface.DriverClass, iface.Name, iface.Port, iface.Problem)
	}
	for _, issue := range device.DriverIssues {
		t.Logf("  driver issue: %s", issue)
	}

	at, err := openDJIUSBAT()
	if err != nil {
		t.Fatalf("open AT: %v", err)
	}
	defer at.Close()
	t.Logf("opened %s", at.Description())
	for _, command := range []string{"ATI", "AT+CPIN?", "AT+CSQ", "AT+CEREG?", "AT+QNWINFO", `AT+QCFG="usbnet"`, "AT+CGACT?"} {
		started := time.Now()
		response, err := at.Command(command, 3*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if !atResponseComplete("\n" + response) {
			t.Fatalf("%s: incomplete response %q", command, response)
		}
		t.Logf("%s (%s) -> %q", command, time.Since(started).Round(time.Millisecond), response)
	}
	if firmware := parseUSBATFirmware(mustAT(t, at, "ATI")); firmware == "" {
		t.Fatal("firmware not parsed")
	}
}

func TestHardwareWindowsNetwork(t *testing.T) {
	device := discoverDJIUSBDevice()
	if device == nil {
		t.Skip("no DJI module connected")
	}
	interfaces := discoverHostNetworkInterfaces()
	if len(interfaces) == 0 {
		t.Fatal("no host interfaces")
	}
	route := discoverHostDefaultRoute()
	t.Logf("default route: %+v", route)
	service := currentDJINetworkService(device, interfaces)
	t.Logf("module adapter: %+v ready=%v", service, networkServiceReady(service))
	if service == nil {
		t.Skip("module network adapter not present")
	}
	if service.InterfacePresent {
		counters, err := discoverInterfaceCounters(service.Index)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("counters: %+v", counters)
	}
	if service.Kind == "wwan" {
		profiles, err := wwanProfiles(t.Context(), service.Name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("WWAN profiles: %d", len(profiles))
	}
	if networkServiceReady(service) {
		records, err := sampleTCPActivity(service.IPv4, service.Name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("TCP connections over cellular: %d", len(records))
	}
}

// Sends a few small HTTP requests over the module's adapter only.
func TestHardwareCellularProbe(t *testing.T) {
	device := discoverDJIUSBDevice()
	service := currentDJINetworkService(device, discoverHostNetworkInterfaces())
	if !networkServiceReady(service) {
		t.Skip("module network adapter not ready")
	}
	target, dnsOK, err := probeCellularInternet(t.Context(), service.Device, service.IPv4)
	if err != nil {
		t.Fatalf("probe over %s (%s): %v", service.Device, service.IPv4, err)
	}
	t.Logf("probe over %s (%s) reached %s, dns=%v", service.Device, service.IPv4, target, dnsOK)
	records, err := sampleTCPActivity(service.IPv4, service.Device)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		t.Logf("  %s -> %s:%s %s", record.Process, record.IP, record.Port, record.State)
	}
}

func mustAT(t *testing.T, at *usbAT, command string) string {
	t.Helper()
	response, err := at.Command(command, 3*time.Second)
	if err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	return response
}
