package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 期望終態:Update/Add 儲存裝置時不把執行時路徑寫進 config(只存 IMEI + 意圖)。
// 目前實作會寫 control_device/interface/at_port → 本測試現在應 FAIL,證明儲存側洩漏。
func TestUpdateDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  modem_imei: \"867383058993207\"\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// 模擬切電信業者/編輯:傳入帶執行時解析路徑的 cfg。
	newDev := DeviceConfig{
		ID:            "dev1",
		ModemIMEI:     "867383058993207",
		DeviceBackend: "qmi",
		ControlDevice: "/dev/cdc-wdm3", // 執行時路徑,不應被持久化
		Interface:     "wwan2",
		ATPort:        "/dev/ttyUSB9",
		USBPath:       "/sys/bus/usb/devices/1-7",
	}
	if err := UpdateDeviceInFile(path, "dev1", newDev); err != nil {
		t.Fatalf("UpdateDeviceInFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	d := got.Devices[0]
	if d.ModemIMEI != "867383058993207" || d.DeviceBackend != "qmi" {
		t.Fatalf("identity/intent fields lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}

// Add 新裝置時同樣不寫路徑(只 IMEI + 意圖)。
func TestAddDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dev := DeviceConfig{
		ID: "dev9", ModemIMEI: "861234567890123", DeviceBackend: "mbim",
		ControlDevice: "/dev/cdc-wdm5", Interface: "wwan5", ATPort: "/dev/ttyUSB1",
		USBPath: "/sys/bus/usb/devices/2-1",
	}
	if err := AddDeviceInFile(path, dev); err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}
	got, _ := Load(path)
	d := got.Devices[0]
	if d.ModemIMEI != "861234567890123" || d.DeviceBackend != "mbim" {
		t.Fatalf("identity/intent lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}
