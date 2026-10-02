package backend

// 編譯期介面合規性檢查（確保所有後端實作都滿足 DeviceBackend 介面）
var (
	_ DeviceBackend = (*ATBackend)(nil)
	_ DeviceBackend = (*QMIBackend)(nil)
	_ USSDProvider  = (*ATBackend)(nil)
	_ USSDProvider  = (*QMIBackend)(nil)
)
