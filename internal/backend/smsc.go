package backend

import "context"

// SMSCProvider 可選能力介面：讀取簡訊中心號碼（SMSC）。
// 該介面不併入 DeviceBackend 聚合，呼叫方按需進行型別斷言。
type SMSCProvider interface {
	GetSMSC(ctx context.Context) (string, error)
}
