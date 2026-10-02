package modem

import (
	"errors"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// imeiCacheItem 儲存 IMEI 快取條目及對應的取得時間戳
type imeiCacheItem struct {
	IMEI string
	TS   time.Time
}

// imeiCache 提供執行緒安全的記憶體 IMEI 對映快取，避免頻繁透過串列埠發起硬體查詢
var imeiCache struct {
	mu sync.RWMutex
	m  map[string]imeiCacheItem
}

// ProbeIMEICached 在 10 分鐘快取有效期內優先從記憶體快取中取得指定 AT 串列埠的 IMEI；若未命中或過期，則呼叫底層串列埠方法探測
func ProbeIMEICached(atPort string, timeout time.Duration) (string, error) {
	atPort = strings.TrimSpace(atPort)
	if atPort == "" {
		return "", errors.New("empty at port")
	}

	imeiCache.mu.RLock()
	if imeiCache.m != nil {
		if it, ok := imeiCache.m[atPort]; ok {
			if it.IMEI != "" && time.Since(it.TS) < 10*time.Minute {
				imeiCache.mu.RUnlock()
				return it.IMEI, nil
			}
		}
	}
	imeiCache.mu.RUnlock()

	imei, err := ProbeIMEI(atPort, timeout)
	if err == nil && imei != "" {
		imeiCache.mu.Lock()
		if imeiCache.m == nil {
			imeiCache.m = make(map[string]imeiCacheItem)
		}
		imeiCache.m[atPort] = imeiCacheItem{IMEI: imei, TS: time.Now()}
		imeiCache.mu.Unlock()
	}
	return imei, err
}

// ProbeIMEI 透過開啟底層 TTY 串列埠裝置並執行 `AT+CGSN` 指令來即時探測模組的 IMEI 串號
func ProbeIMEI(atPort string, timeout time.Duration) (string, error) {
	atPort = strings.TrimSpace(atPort)
	if atPort == "" {
		return "", errors.New("empty at port")
	}
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}

	// 設定標準的 3 線非同步串列埠鮑率與訊框驗證格式
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	}

	p, err := serial.Open(atPort, mode)
	if err != nil {
		return "", err
	}
	defer p.Close()

	_ = p.SetReadTimeout(80 * time.Millisecond)

	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1024)
	var acc strings.Builder

	write := func(s string) {
		_, _ = p.Write([]byte(s))
	}

	// 寫入 AT 測試指令與查詢 IMEI 的 AT+CGSN 指令
	write("AT\r\n")
	time.Sleep(40 * time.Millisecond)
	write("AT+CGSN\r\n")

	// 在指定的截止時間內輪詢並解析串列埠輸出內容
	for time.Now().Before(deadline) {
		n, rerr := p.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if imei := parseIMEI(acc.String()); imei != "" {
				return imei, nil
			}
		}
		if rerr != nil {
			if strings.Contains(strings.ToLower(rerr.Error()), "timeout") {
				continue
			}
		}
	}

	// 最終嘗試解析一次累積的串列埠緩衝區
	if imei := parseIMEI(acc.String()); imei != "" {
		return imei, nil
	}
	return "", errors.New("imei probe timeout")
}
