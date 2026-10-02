//go:build windows

package main

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// enumerateDJIPnPDevices lists present USB nodes (composite parent and
// interface children) whose hardware ID carries the DJI vendor ID.
func enumerateDJIPnPDevices() ([]pnpDevice, error) {
	set, err := windows.SetupDiGetClassDevsEx(nil, "USB", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return nil, err
	}
	defer set.Close()

	var devices []pnpDevice
	for index := 0; ; index++ {
		data, err := set.EnumDeviceInfo(index)
		if err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			continue
		}
		instanceID, err := set.DeviceInstanceID(data)
		if err != nil {
			continue
		}
		vendor, _, ok := pnpVendorProduct(instanceID)
		if !ok || vendor != djiUSBVendorID {
			continue
		}
		device := pnpDevice{
			InstanceID:    instanceID,
			FriendlyName:  registryString(set, data, windows.SPDRP_FRIENDLYNAME),
			Description:   registryString(set, data, windows.SPDRP_DEVICEDESC),
			Manufacturer:  registryString(set, data, windows.SPDRP_MFG),
			Class:         registryString(set, data, windows.SPDRP_CLASS),
			Location:      registryString(set, data, windows.SPDRP_LOCATION_INFORMATION),
			CompatibleIDs: registryStrings(set, data, windows.SPDRP_COMPATIBLEIDS),
		}
		var status, problem uint32
		if windows.CM_Get_DevNode_Status(&status, &problem, data.DevInst, 0) == nil && status&windows.DN_HAS_PROBLEM != 0 {
			device.Problem = problem
		}
		device.PortName = deviceKeyString(set, data, windows.DIREG_DEV, "PortName")
		if strings.EqualFold(device.Class, "Net") {
			device.NetCfgInstanceID = deviceKeyString(set, data, windows.DIREG_DRV, "NetCfgInstanceId")
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func discoverDJIUSBDevice() *usbDeviceStatus {
	nodes, err := enumerateDJIPnPDevices()
	if err != nil {
		return nil
	}
	return buildUSBDeviceStatus(nodes)
}

func registryString(set windows.DevInfo, data *windows.DevInfoData, property windows.SPDRP) string {
	value, err := set.DeviceRegistryProperty(data, property)
	if err != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}
	}
	return ""
}

func registryStrings(set windows.DevInfo, data *windows.DevInfoData, property windows.SPDRP) []string {
	value, err := set.DeviceRegistryProperty(data, property)
	if err != nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case string:
		return []string{typed}
	}
	return nil
}

func deviceKeyString(set windows.DevInfo, data *windows.DevInfoData, keyType windows.DIREG, name string) string {
	handle, err := set.OpenDevRegKey(data, windows.DICS_FLAG_GLOBAL, 0, keyType, windows.KEY_READ)
	if err != nil {
		return ""
	}
	key := registry.Key(handle)
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}
