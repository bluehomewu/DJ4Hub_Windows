//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
)

const adbExecutableName = "adb.exe"

// adbSerialIdentity identifies a USB ADB device by serial. Windows adb does
// not print the usb: location that macOS uses; network targets (host:port)
// are never accepted.
func adbSerialIdentity(serial string) string {
	if serial == "" || strings.Contains(serial, ":") {
		return ""
	}
	return "serial:" + serial
}

// adbIdentityForLocation returns the adb identity to expect for a USB
// location. Windows cannot relate the two, so the first single matching
// device pins the session instead.
func adbIdentityForLocation(string) string {
	return ""
}

// audioUSBLocation returns the PnP location of the only connected module,
// used to prove the same port came back after an ADB reboot.
func audioUSBLocation(context.Context) (string, error) {
	nodes, err := enumerateDJIPnPDevices()
	if err != nil {
		return "", errors.New("無法核對唯一 USB 裝置，未初始化")
	}
	var locations []string
	for _, node := range nodes {
		if _, isInterface := pnpInterfaceNumber(node.InstanceID); isInterface {
			continue
		}
		if vendor, product, ok := pnpVendorProduct(node.InstanceID); ok && vendor == djiUSBVendorID && product == djiUSBProductID {
			locations = append(locations, node.Location)
		}
	}
	if len(locations) != 1 || locations[0] == "" {
		return "", errors.New("請只連線一臺 DJI 模組；未授權或修改任何裝置")
	}
	return locations[0], nil
}
