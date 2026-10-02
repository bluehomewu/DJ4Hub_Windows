package backend

import "context"

// SMSProvider 簡訊收發介面
type SMSProvider interface {
	// SendSMS 傳送簡訊
	// AT 實現：AT+CMGS (PDU 模式)
	// QMI 實現：WMS.SendRawMessage
	SendSMS(ctx context.Context, to, body string) error

	// ReadSMS 讀取指定索引的簡訊
	// AT 實現：AT+CMGR
	// QMI 實現：WMS.RawReadMessage
	ReadSMS(ctx context.Context, index int) (*SMS, error)

	// DeleteSMS 刪除指定索引的簡訊
	// AT 實現：AT+CMGD
	// QMI 實現：WMS.DeleteMessage
	DeleteSMS(ctx context.Context, index int) error

	// ListSMS 列出所有簡訊概要
	// AT 實現：AT+CMGL=4
	// QMI 實現：WMS.ListMessages
	ListSMS(ctx context.Context) ([]SMSSummary, error)

	// DeleteAllSMS 刪除所有簡訊
	// AT 實現：AT+CMGD=1,4
	// QMI 實現：WMS.DeleteMessagesByTag（遍歷所有 tag）
	DeleteAllSMS(ctx context.Context) error
}
