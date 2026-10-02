package backend

import "time"

// SignalInfo 訊號品質資訊（AT 和 QMI 後端統一返回此結構）
type SignalInfo struct {
	// 通用訊號強度
	RSSI int // dBm（AT+CSQ 轉換值 或 NAS.GetSignalStrength）

	// LTE 專有
	RSRP int // dBm（AT+QENG 或 NAS.GetSignalInfo）
	RSRQ int // dB（AT+QENG 或 NAS.GetSignalInfo）
	SINR int // dB（NAS.GetSignalInfo LTE RSSNR）

	// 5G 專有（NAS.GetSignalInfo 5G TLV）
	NR5GRSRP int
	NR5GRSRQ int
	NR5GSINR int
}

// ServingSystem 網路註冊狀態（AT 和 QMI 後端統一返回此結構）
type ServingSystem struct {
	// 註冊狀態（0=未註冊, 1=本地註冊, 2=搜尋中, 3=被拒, 4=未知, 5=漫遊註冊）
	RegStatus     int
	RegStatusText string

	// PLMN 資訊
	Operator string // 電信業者名稱/程式碼
	MCC      uint16
	MNC      uint16

	// 位置資訊
	LAC    string // 位置區程式碼
	CellID string // 小區 ID

	// 接入技術
	NetworkMode   string // LTE/WCDMA/GSM 等
	NetworkDuplex string // FDD/TDD
	RadioBand     string // 目前服務小區/無線介面頻段
	RadioChannel  uint32 // EARFCN/ARFCN/channel

	// PS 附著狀態
	PSAttached bool
}

// SIMMetadata 表示 SIM/eSIM profile 的原生後設資料。
type SIMMetadata struct {
	NativeMCC    string
	NativeMNC    string
	GID1         string
	GID2         string
	PNN          []PNNRecord
	OPL          []OPLRecord
	ServiceTable *SIMServiceTable
}

type PNNRecord struct {
	Record    int    `json:"record"`
	FullName  string `json:"full_name,omitempty"`
	ShortName string `json:"short_name,omitempty"`
	RawHex    string `json:"raw_hex,omitempty"`
}

type OPLRecord struct {
	Record    int    `json:"record"`
	PLMN      string `json:"plmn,omitempty"`
	LACStart  uint16 `json:"lac_start,omitempty"`
	LACEnd    uint16 `json:"lac_end,omitempty"`
	PNNRecord int    `json:"pnn_record,omitempty"`
	RawHex    string `json:"raw_hex,omitempty"`
}

type SIMServiceTable struct {
	Kind            string `json:"kind,omitempty"`
	RawHex          string `json:"raw_hex,omitempty"`
	EnabledServices []int  `json:"enabled_services,omitempty"`
}

// SMS 簡訊訊息（統一資料結構）
type SMS struct {
	Index     int
	Sender    string
	Content   string
	Timestamp time.Time
}

// SMSSummary 簡訊列表概要
type SMSSummary struct {
	Index int
	Tag   int // 0=已讀, 1=未讀, 2=已傳送, 3=未傳送
}

// OperatingMode 操作模式（對映 AT+CFUN 值和 QMI DMS OperatingMode）
type OperatingMode int

const (
	ModeOnline   OperatingMode = 1 // AT+CFUN=1 / DMS ModeOnline
	ModeLowPower OperatingMode = 0 // AT+CFUN=0 / DMS ModeLowPower
	ModeRFOff    OperatingMode = 4 // AT+CFUN=4 / DMS ModePersistLow (飛航模式)
)
