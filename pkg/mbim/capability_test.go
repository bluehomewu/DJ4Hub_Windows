package mbim

import "testing"

func TestCapabilitiesAuthAKAUsable(t *testing.T) {
	c := &Capabilities{
		Services: DeviceServices{Elements: []DeviceServiceElement{
			{Service: UUIDAuth, CIDs: []uint32{1}},
		}},
	}
	if !c.AuthAKAUsable() {
		t.Fatal("宣告 Auth 應可用")
	}
	c.MarkAuthAKADead()
	if c.AuthAKAUsable() {
		t.Fatal("熔斷後應不可用")
	}
}

func TestCapabilitiesAuthAKANotAdvertised(t *testing.T) {
	c := &Capabilities{Services: DeviceServices{}}
	if c.AuthAKAUsable() {
		t.Fatal("未宣告 Auth 不應可用")
	}
}

func TestCapabilitiesUICCChannelAndMBIMEx(t *testing.T) {
	c := &Capabilities{UICCChannelOK: true, MBIMExOK: true, QMIOverMBIMOK: true}
	if !c.UICCChannelAKAUsable() || !c.MBIMExUsable() || !c.QMIReadUsable() {
		t.Fatal("探針位應直通傳送")
	}
}

func TestCapabilitiesAppListKnownUnsupported(t *testing.T) {
	uiccAdvertised := DeviceServices{Elements: []DeviceServiceElement{
		{Service: UUIDMSUICCLowLevelAccess, CIDs: []uint32{7}},
	}}
	if !(&Capabilities{Services: uiccAdvertised, AppListOK: false}).AppListKnownUnsupported() {
		t.Fatal("宣告 UICC 但探針失敗應判為確知不支援")
	}
	if (&Capabilities{Services: uiccAdvertised, AppListOK: true}).AppListKnownUnsupported() {
		t.Fatal("探針成功不應判為不支援")
	}
	if (&Capabilities{}).AppListKnownUnsupported() {
		t.Fatal("未宣告 UICC 應為 unknown,不判為確知不支援")
	}
	if (*Capabilities)(nil).AppListKnownUnsupported() {
		t.Fatal("nil 應為 false")
	}
}

func TestCapabilitiesDeviceResetUsable(t *testing.T) {
	resetAdvertised := DeviceServices{Elements: []DeviceServiceElement{
		{Service: UUIDMSBasicConnectExtensions, CIDs: []uint32{CIDMSBasicConnectExtDeviceReset}},
	}}
	if !(&Capabilities{Services: resetAdvertised}).DeviceResetUsable() {
		t.Fatal("DeviceResetUsable() = false, want true when DEVICE_RESET is advertised")
	}
	if (&Capabilities{}).DeviceResetUsable() {
		t.Fatal("DeviceResetUsable() = true, want false without DEVICE_RESET")
	}
	if (*Capabilities)(nil).DeviceResetUsable() {
		t.Fatal("nil 應為 false")
	}
}
