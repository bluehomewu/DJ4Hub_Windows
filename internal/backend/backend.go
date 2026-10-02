// Package backend 定義 DJ 4G Hub 的裝置後端抽象層。
// AT 和 QMI 是平等的兩種後端實作，透過設定開關 device_backend 選擇。
package backend

// DeviceBackend 頂層聚合介面 — 所有後端模式（at/qmi/auto）均實作此介面
type DeviceBackend interface {
	DeviceInfoProvider
	SMSProvider
	OperatingModeController
	SIMAuthProvider

	// Mode 回傳目前後端模式識別碼: "at" | "qmi"
	Mode() string

	// Close 釋放後端持有的資源（QMI service 連線等）
	Close() error
}
