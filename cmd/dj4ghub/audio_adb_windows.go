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

// adbIdentityForLocation returns the adb identity of the module at a PnP
// location. Windows adb prints no USB location, but it prints the USB serial
// number, which is the last segment of the PnP instance ID. Windows invents
// that segment (containing "&") when the device has no serial, and adb then
// shows "?". Pinning by serial keeps other adb devices, such as a phone or
// tablet, out of the audio session without sending them any command.
func adbIdentityForLocation(location string) string {
	nodes, err := enumerateDJIPnPDevices()
	if err != nil {
		return ""
	}
	for _, node := range nodes {
		if _, isInterface := pnpInterfaceNumber(node.InstanceID); isInterface || node.Location != location {
			continue
		}
		return adbSerialIdentity(usbSerialFromInstanceID(node.InstanceID))
	}
	return ""
}

func usbSerialFromInstanceID(instanceID string) string {
	serial := instanceID[strings.LastIndex(instanceID, `\`)+1:]
	if strings.Contains(serial, "&") {
		return "?"
	}
	return serial
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
		return "", errors.New("請只連線一台 DJI 模組；未授權或修改任何裝置")
	}
	return locations[0], nil
}
