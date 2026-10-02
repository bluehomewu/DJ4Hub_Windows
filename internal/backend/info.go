package backend

import "context"

// DeviceInfoProvider 裝置資訊查詢介面（狀態查詢主線）
type DeviceInfoProvider interface {
	// GetIMEI 取得裝置 IMEI
	// AT 實作：AT+CGSN
	// QMI 實作：DMS.GetDeviceSerialNumbers
	GetIMEI(ctx context.Context) (string, error)

	// GetIMSI 取得 SIM 卡 IMSI
	// AT 實作：AT+CIMI
	// QMI 實作：UIM.GetIMSI
	GetIMSI(ctx context.Context) (string, error)

	// GetICCID 取得 SIM 卡 ICCID
	// AT 實作：AT+QCCID
	// QMI 實作：UIM.GetICCID
	GetICCID(ctx context.Context) (string, error)

	// GetMSISDN 取得本機號碼。
	// AT 實作：AT+CNUM
	// QMI 實作：DMS.GetMSISDN
	GetMSISDN(ctx context.Context) (string, error)

	// GetRevision 取得韌體版本
	// AT 實作：AT+CGMR
	// QMI 實作：DMS.GetDeviceRevision
	GetRevision(ctx context.Context) (string, error)

	// GetSignalInfo 取得訊號品質資訊
	// AT 實作：AT+CSQ + AT+QENG="servingcell"
	// QMI 實作：NAS.GetSignalStrength + NAS.GetSignalInfo
	GetSignalInfo(ctx context.Context) (*SignalInfo, error)

	// GetServingSystem 取得網路註冊狀態和服務系統資訊
	// AT 實作：AT+CREG? + AT+COPS?
	// QMI 實作：NAS.GetServingSystem
	GetServingSystem(ctx context.Context) (*ServingSystem, error)

	// IsSimInserted 判斷 SIM 卡是否已插入
	// AT 實作：AT+QSIMSTAT? / AT+CPIN?
	// QMI 實作：DMS.GetSIMStatus
	IsSimInserted(ctx context.Context) (bool, error)

	// GetNativeMCCMNC 取得 SIM 歸屬 MCC 和 MNC（區別於目前駐留網路）。
	// 實作必須基於 IMSI + EF_AD
	GetNativeMCCMNC(ctx context.Context) (mcc string, mnc string, err error)

	// GetNativeSPN 讀取 SIM EF_SPN 服務提供商名稱（區別於目前駐留網路 operator）。
	// AT 實作：AT+CRSM 讀取 EF_SPN
	// QMI 實作：UIM.ReadTransparent讀取 EF_SPN
	GetNativeSPN(ctx context.Context) (string, error)

	// GetSIMMetadata 讀取 SIM/eSIM profile 的原生後設資料。
	// AT 實作：AT+CRSM 讀取 EF_AD/GID/PNN/OPL/SST/UST
	// QMI 實作：UIM.ReadTransparent/ReadRecord 讀取對應 EF
	GetSIMMetadata(ctx context.Context) (*SIMMetadata, error)
}
