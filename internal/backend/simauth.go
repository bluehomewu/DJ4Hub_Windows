package backend

import "context"

// SIMAuthProvider SIM 卡驗證 / APDU 通道介面
type SIMAuthProvider interface {
	// OpenLogicalChannel 開啟邏輯通道
	// AT 實作：AT+CCHO
	// QMI 實作：UIM.OpenLogicalChannel
	OpenLogicalChannel(ctx context.Context, aid string) (channelID int, err error)

	// CloseLogicalChannel 關閉邏輯通道
	// AT 實作：AT+CCHC
	// QMI 實作：UIM.CloseLogicalChannel
	CloseLogicalChannel(ctx context.Context, channelID int) error

	// TransmitAPDU 在邏輯通道上傳輸 APDU
	// AT 實作：AT+CGLA
	// QMI 實作：UIM.SendAPDU
	TransmitAPDU(ctx context.Context, channelID int, command string) (response string, err error)
}

type SIMAuthAIDResolver interface {
	ResolveSIMAuthAID(ctx context.Context, app string, fallbackAID string) (resolvedAID string, source string, err error)
}
