package backend

import "context"

// OperatingModeController CFUN / 射頻控制介面
type OperatingModeController interface {
	// SetOperatingMode 設定操作模式
	// AT 實作：AT+CFUN=N
	// QMI 實作：DMS.SetOperatingMode
	SetOperatingMode(ctx context.Context, mode OperatingMode) error

	// GetOperatingMode 取得目前操作模式
	// AT 實作：AT+CFUN?
	// QMI 實作：DMS.GetOperatingMode
	GetOperatingMode(ctx context.Context) (OperatingMode, error)

	// Reboot 重啟模組
	// AT 實作：AT+CFUN=1,1
	// QMI 實作：DMS.SetOperatingMode(ModeReset)
	Reboot(ctx context.Context) error
}
