package esim

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/WongLoki/DJ4Hub/internal/modem"
)

// ModemChannel 實作 euicc-go 的 driver.SmartCardChannel 介面
// 將 eUICC APDU 請求橋接到 modem.Manager 的 AT 指令執行框架
type ModemChannel struct {
	modem   *modem.Manager
	channel byte // 目前開啟的 logical channel 號
	mu      sync.Mutex
}

// NewModemChannel 建立一個新的 ModemChannel
func NewModemChannel(m *modem.Manager) *ModemChannel {
	return &ModemChannel{modem: m}
}

func (c *ModemChannel) CurrentChannel() byte {
	return c.channel
}

// Connect 連線到 APDU 介面（modem 已由外部管理，此處為空操作）
func (c *ModemChannel) Connect() error {
	return nil
}

// Disconnect 中斷 APDU 介面連線（modem 由外部管理，此處為空操作）
func (c *ModemChannel) Disconnect() error {
	return nil
}

// OpenLogicalChannel 透過 AT+CCHO 開啟 logical channel 並選擇指定 AID
// 回傳 channel 號
// 注意：不在此處做 ClearLogicalChannels，由上層 Manager 在遍歷開始前統一預清理，
// 避免對 SIM 卡頻繁傳送通道指令導致卡片進入保護狀態。
func (c *ModemChannel) OpenLogicalChannel(AID []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	aidHex := fmt.Sprintf("%X", AID)
	ch, err := c.modem.OpenLogicalChannel(aidHex)
	if err != nil {
		return 0, fmt.Errorf("開啟 logical channel 失敗 (AID=%s): %w", aidHex, err)
	}
	c.channel = byte(ch)
	return c.channel, nil
}

// Transmit 透過 AT+CGLA 在 logical channel 上直通傳送 APDU 指令
// 輸入原始二進位 APDU 指令，回傳原始二進位 APDU 回應
func (c *ModemChannel) Transmit(command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 將二進位 APDU 編碼為 hex 字串
	cmdHex := fmt.Sprintf("%X", command)

	// 透過 AT+CGLA 傳送
	respHex, err := c.modem.TransmitAPDU(int(c.channel), cmdHex)
	if err != nil {
		return nil, fmt.Errorf("APDU 直通傳送失敗: %w", err)
	}

	// 將 hex 回應解碼為二進位
	respBytes, err := hex.DecodeString(respHex)
	if err != nil {
		return nil, fmt.Errorf("解析 APDU 回應 hex 失敗: %w", err)
	}

	return respBytes, nil
}

// CloseLogicalChannel 透過 AT+CCHC 關閉 logical channel
func (c *ModemChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.modem.CloseLogicalChannel(int(channel)); err != nil {
		return fmt.Errorf("關閉 logical channel %d 失敗: %w", channel, err)
	}
	c.channel = 0
	return nil
}
