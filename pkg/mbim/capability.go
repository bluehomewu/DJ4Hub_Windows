package mbim

import "sync/atomic"

type Capabilities struct {
	Services      DeviceServices
	MBIMExOK      bool
	QMIOverMBIMOK bool
	UICCReadOK    bool // READ_BINARY(透明 EF,如 EF_ICCID)
	UICCRecordOK  bool // READ_RECORD(線性記錄 EF,如 EF_DIR——AID 解析關鍵路徑)
	UICCChannelOK bool
	AppListOK     bool
	authAKADead   atomic.Bool
}

func (c *Capabilities) AuthAKAUsable() bool {
	if c == nil {
		return false
	}
	return c.Services.HasService(UUIDAuth) && !c.authAKADead.Load()
}

func (c *Capabilities) MarkAuthAKADead() {
	if c != nil {
		c.authAKADead.Store(true)
	}
}

func (c *Capabilities) UICCChannelAKAUsable() bool {
	return c != nil && c.UICCChannelOK
}

func (c *Capabilities) MBIMExUsable() bool {
	return c != nil && c.MBIMExOK
}

func (c *Capabilities) QMIReadUsable() bool {
	return c != nil && c.QMIOverMBIMOK
}

func (c *Capabilities) DeviceResetUsable() bool {
	return c != nil && c.Services.Supports(UUIDMSBasicConnectExtensions, CIDMSBasicConnectExtDeviceReset)
}

// AppListKnownUnsupported 報告 APPLICATION_LIST 是否"確知不支援":僅當 UICC 服務
// 已宣告(init 探針確實跑過)但探針失敗時為真。此時 AID 解析應直接走 EF_DIR 直接讀取,
// 跳過註定失敗的 APPLICATION_LIST。未宣告/未探(unknown)時回傳 false,保留原有
// "先試再回退"行為,不引入迴歸。
func (c *Capabilities) AppListKnownUnsupported() bool {
	return c != nil && c.Services.HasService(UUIDMSUICCLowLevelAccess) && !c.AppListOK
}
