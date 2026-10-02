package modem

import (
	"testing"

	"github.com/WongLoki/DJ4Hub/internal/config"
)

func TestPureControlPlaneBackendMBIM(t *testing.T) {
	if !pureQMIBackendConfig(config.DeviceConfig{DeviceBackend: "mbim", ControlDevice: "/dev/cdc-wdm2"}) {
		t.Fatal("mbim 應視為控制面後端，跳過 AT 啟動")
	}
	if !pureQMIBackendConfig(config.DeviceConfig{DeviceBackend: "mbim"}) {
		t.Fatal("mbim 無 AT 口也應跳過 AT 啟動")
	}
	if !pureQMIBackendConfig(config.DeviceConfig{DeviceBackend: "qmi"}) {
		t.Fatal("qmi 仍應成立")
	}
	if pureQMIBackendConfig(config.DeviceConfig{DeviceBackend: "at", ATPort: "/dev/ttyUSB0"}) {
		t.Fatal("at 不應跳過 AT 啟動")
	}
}
