package modem

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/WongLoki/DJ4Hub/internal/apduarbiter"
	"github.com/WongLoki/DJ4Hub/internal/config"
	"github.com/WongLoki/DJ4Hub/pkg/logger"
	"github.com/WongLoki/DJ4Hub/pkg/smscodec"
	"github.com/warthog618/sms/encoding/gsm7"

	"go.bug.st/serial"
)

// SMSCallback 簡訊回呼函式型別
type SMSCallback func(sender, content string, timestamp time.Time)

// rxMsg 串列埠接收到的訊息包裝
type rxMsg struct {
	Data string
	Err  error
}

// commandRequest AT 指令請求結構
type commandRequest struct {
	cmd          string
	respChan     chan string
	errChan      chan error
	timeout      time.Duration
	silent       bool
	highPriority bool

	// 互動式模式支援
	interactive bool   // 是否為互動式指令 (如傳送簡訊)
	waitPrompt  bool   // 是否等待 "> " 提示符
	followUp    string // 後續指令 (當 waitPrompt=true 且收到提示符時傳送)
}

// Manager 管理單個 EC20 模組的 AT 指令通訊
type Manager struct {
	cfg      config.DeviceConfig
	atPort   string
	port     serial.Port
	portMode *serial.Mode

	// 通道驅動的非同步架構
	stop        chan struct{}
	stopOnce    sync.Once
	loopWG      sync.WaitGroup
	cmdChan     chan commandRequest // 普通優先順序
	cmdChanHigh chan commandRequest // 高優先順序 (簡訊, IP 切換)
	rxChan      chan rxMsg
	triggerChan chan struct{} // 簡訊觸發訊號
	ready       chan struct{}
	readyOnce   sync.Once

	// 資源池
	reqPool sync.Pool

	// 狀態
	running  bool
	busy     bool
	busyMu   sync.Mutex
	healthy  bool
	eofCount int // readLoop 中連續 EOF 計數，用於檢測裝置中斷

	atTimeoutMu     sync.Mutex
	atTimeoutStreak int

	// 裝置資訊 (從 AT 指令取得)
	imei        string
	firmware    string
	iccid       string
	imsi        string
	msisdn      string
	operator    string
	simInserted bool
	signalDBM   int
	signalRSRQ  int
	signalRSRP  int

	// 網路資訊
	regStatus     int    // 網路註冊狀態 (0-5)
	regStatusText string // 註冊狀態文字
	lac           string // 位置區碼
	cellID        string // 基地台 ID
	apn           string // 存取點
	imsStatus     int    // IMS 註冊狀態
	networkMode   string // 網路模式 (LTE/WCDMA/GSM等)
	networkDuplex string // 網路雙工方式 (FDD/TDD)
	usbnetMode    int    // USBNET 模式 (0: QMI, 1: ECM)

	infoMu sync.RWMutex

	// 回呼
	smsCallback            SMSCallback
	newSMSHandler          func(index string) // 處理新簡訊索引的回呼 (用於 bubble up URC)
	disableURCRead         bool               // 如果啟用 QMI，禁用 AT 自動讀取
	simStatusHandler       func(inserted *bool, state string)
	onDisconnectWithReason func(reason string)

	// CS 來電回呼
	ringCallback    func()              // RING URC 回呼
	clipCallback    func(number string) // +CLIP URC 回呼 (來電號碼)
	hangupCallback  func()              // NO CARRIER URC 回呼 (對方掛斷)
	connectCallback func()              // CONNECT/OK URC 回呼 (對方接聽外呼)
	qpcmvChan       chan int            // +QPCMV URC 流控通道 (0=忙, 1=就緒)

	reassembler *smscodec.Reassembler

	// SIM 卡低頻巡檢警告狀態
	simFailCount int
	simAlerting  bool

	// USSD 工作階段通道：當有協程在等待 USSD 回應時，+CUSD URC 會被投遞到此通道
	ussdChan chan USSDResult

	// RDY 事件訂閱（模組重啟後廣播）
	rdyMu   sync.Mutex
	rdySubs []chan struct{}

	// APDU 仲裁（裝置級全域性）
	apduArbiter  *apduarbiter.Arbiter
	apduLeaseMu  sync.Mutex
	apduSessions map[int]apduSessionInfo
}

const atTimeoutWatchdogThreshold = 5

type apduSessionInfo struct {
	Channel  int
	Owner    string
	Class    apduarbiter.APDUClass
	OpenedAt time.Time
}

func (m *Manager) DeviceID() string {
	return m.cfg.ID
}

func pureQMIBackendConfig(cfg config.DeviceConfig) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.DeviceBackend))
	return mode == "qmi" || mode == "mbim" || (mode == "" && strings.TrimSpace(cfg.ControlDevice) != "")
}

func (m *Manager) pureQMIBackend() bool {
	return pureQMIBackendConfig(m.cfg)
}

func New(cfg config.DeviceConfig) (*Manager, error) {
	m := &Manager{
		cfg:          cfg,
		atPort:       cfg.ATPort,
		stop:         make(chan struct{}),
		cmdChan:      make(chan commandRequest, 10),
		cmdChanHigh:  make(chan commandRequest, 5),
		rxChan:       make(chan rxMsg, 100),
		triggerChan:  make(chan struct{}, 1),
		ready:        make(chan struct{}),
		healthy:      true,
		reassembler:  smscodec.NewReassembler(),
		ussdChan:     make(chan USSDResult, 1),
		apduSessions: make(map[int]apduSessionInfo),
		reqPool: sync.Pool{
			New: func() interface{} {
				return &commandRequest{
					respChan: make(chan string, 1),
					errChan:  make(chan error, 1),
				}
			},
		},
	}

	// 如果未指定 AT 埠，使用 ManagePort
	if m.atPort == "" {
		m.atPort = cfg.ManagePort
	}
	// QMI 後端模式下允許 AT 埠為空（模組不依賴 AT 串列埠）
	// AT 模式仍然要求 AT 埠非空
	if m.atPort == "" && !pureQMIBackendConfig(cfg) {
		return nil, errors.New("AT port not configured")
	}

	m.portMode = &serial.Mode{
		BaudRate: 115200,
	}

	return m, nil
}

func (m *Manager) markReady() {
	m.readyOnce.Do(func() {
		close(m.ready)
	})
}

func (m *Manager) WaitReady(timeout time.Duration) bool {
	select {
	case <-m.ready:
		return true
	case <-time.After(timeout):
		return false
	case <-m.stop:
		return false
	}
}

func (m *Manager) SetSMSCallback(cb SMSCallback) {
	m.smsCallback = cb
}

// SetNewSMSHandler 設定新簡訊索引回呼 (當收到 URC 時呼叫，用於接管讀取流程)
func (m *Manager) SetNewSMSHandler(handler func(index string)) {
	m.infoMu.Lock()
	m.newSMSHandler = handler
	m.infoMu.Unlock()
}

// SetDisableURCRead 啟用/禁用 URC 自動讀取 (當 QMI 接管時應禁用)
func (m *Manager) SetDisableURCRead(disable bool) {
	m.infoMu.Lock()
	m.disableURCRead = disable
	m.infoMu.Unlock()
}

func (m *Manager) SetSIMStatusHandler(handler func(inserted *bool, state string)) {
	m.infoMu.Lock()
	m.simStatusHandler = handler
	m.infoMu.Unlock()
}

// SetRingCallback 設定來電 RING 回呼
func (m *Manager) SetRingCallback(cb func()) {
	m.infoMu.Lock()
	m.ringCallback = cb
	m.infoMu.Unlock()
}

// SetClipCallback 設定 +CLIP 來電號碼回呼
func (m *Manager) SetClipCallback(fn func(number string)) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.clipCallback = fn
}

// SetHangupCallback 設定 NO CARRIER 對方掛斷回呼
func (m *Manager) SetHangupCallback(fn func()) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.hangupCallback = fn
}

// GetQPCMVChan 取得 +QPCMV URC 流控通道 (0=模組忙, 1=就緒)
func (m *Manager) GetQPCMVChan() <-chan int {
	if m.qpcmvChan == nil {
		m.qpcmvChan = make(chan int, 4)
	}
	return m.qpcmvChan
}

// AnswerCall 接聽來電 (ATA)
func (m *Manager) AnswerCall() error {
	_, err := m.ExecuteAT("ATA", 5*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 接聽來電失敗", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] 已接聽來電", m.cfg.ID))
	return nil
}

// DialCall 發起語音外呼 (ATD<number>;)
func (m *Manager) DialCall(number string) error {
	cmd := fmt.Sprintf("ATD%s;", number)
	_, err := m.ExecuteAT(cmd, 60*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 撥號失敗", m.cfg.ID), "err", err, "number", number)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] 撥號指令已發出", m.cfg.ID), "number", number)
	return nil
}

// HangupCall 掛斷通話 (ATH)
func (m *Manager) HangupCall() error {
	_, err := m.ExecuteAT("ATH", 3*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 掛斷通話失敗", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] 已掛斷通話", m.cfg.ID))
	return nil
}

// SetConnectCallback 設定 CONNECT/OK (對方接聽外呼) 回呼
func (m *Manager) SetConnectCallback(fn func()) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.connectCallback = fn
}

// SetOnDisconnectWithReason 設定帶原因的串列埠/控制面掉線回呼。
func (m *Manager) SetOnDisconnectWithReason(cb func(reason string)) {
	m.infoMu.Lock()
	m.onDisconnectWithReason = cb
	m.infoMu.Unlock()
}

func (m *Manager) notifyDisconnect(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "modem_disconnect"
	}
	m.infoMu.RLock()
	withReason := m.onDisconnectWithReason
	m.infoMu.RUnlock()
	if withReason != nil {
		go withReason(reason)
	}
}

func (m *Manager) resetATTimeoutWatchdog() {
	m.atTimeoutMu.Lock()
	m.atTimeoutStreak = 0
	m.atTimeoutMu.Unlock()
}

func (m *Manager) recordATTimeout(req commandRequest) (int, bool) {
	if req.highPriority {
		return 0, false
	}
	m.atTimeoutMu.Lock()
	defer m.atTimeoutMu.Unlock()
	m.atTimeoutStreak++
	return m.atTimeoutStreak, m.atTimeoutStreak >= atTimeoutWatchdogThreshold
}

func (m *Manager) tripATTimeoutWatchdog(cmd string, failures int) {
	if !m.running {
		return
	}
	logger.Warn(fmt.Sprintf("[%s] AT 連續逾時達到閾值，觸發控制面恢復", m.cfg.ID),
		"cmd", cmd,
		"port", m.atPort,
		"failures", failures,
		"threshold", atTimeoutWatchdogThreshold)
	m.healthy = false
	m.Stop()
	m.notifyDisconnect("at_timeout_threshold")
}

// Start 啟動 AT 管理器的背景協程
func (m *Manager) Start() error {
	if m.pureQMIBackend() {
		logger.Info(fmt.Sprintf("[%s] 純 QMI 模式，跳過 AT 管理器啟動", m.cfg.ID), "at_port", m.atPort)
		m.running = false
		m.markReady()
		return nil
	}
	if m.atPort == "" {
		return errors.New("AT port not configured")
	}

	// 檢查並強制接管被佔用的埠
	m.forceReleasePort(m.atPort)

	var err error
	for attempt := 0; attempt < 8; attempt++ {
		m.port, err = serial.Open(m.atPort, m.portMode)
		if err == nil {
			break
		}
		if !isRetryableSerialOpenErr(err) {
			break
		}
		time.Sleep(time.Duration(80*(attempt+1)) * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("開啟串列埠 %s 失敗: %w", m.atPort, err)
	}

	m.port.SetReadTimeout(100 * time.Millisecond)
	m.running = true

	// 啟動讀取協程
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		m.readLoop()
	}()

	// 啟動主事件迴圈
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		m.runLoop()
	}()

	// 啟動分段清理協程
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		ticker := time.NewTicker(2 * time.Minute)
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.cleanupOldFragments()
			}
		}
	}()

	logger.Info(fmt.Sprintf("[%s] AT 管理器已啟動", m.cfg.ID), "port", m.atPort)
	return nil
}

func isRetryableSerialOpenErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "resource busy") ||
		strings.Contains(msg, "device or resource busy") ||
		strings.Contains(msg, "temporarily unavailable")
}

func isFatalSerialRuntimeErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "no such device") ||
		strings.Contains(msg, "bad file descriptor") ||
		strings.Contains(msg, "device disconnected")
}

func (m *Manager) handleFatalSerialRuntimeErr(err error, phase string, cmd string) {
	if !isFatalSerialRuntimeErr(err) {
		return
	}
	if !m.running {
		return
	}
	logger.Warn(fmt.Sprintf("[%s] AT 串列埠執行期失效，觸發恢復", m.cfg.ID),
		"phase", phase, "cmd", cmd, "port", m.atPort, "err", err)
	m.healthy = false
	m.Stop()
	m.notifyDisconnect("serial_runtime_error")
}

// Stop 停止管理器
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		m.releaseAllAPDULeases("stop")
		close(m.stop)
		if m.port != nil {
			m.port.Close()
		}
		m.running = false
	})
}

func (m *Manager) StopAndWait(timeout time.Duration) bool {
	m.Stop()
	done := make(chan struct{})
	go func() {
		m.loopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// runLoop 主事件迴圈，處理指令和 URC
func (m *Manager) runLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("[%s] runLoop panic recovered", m.cfg.ID), "err", r)
		}
	}()

	// 非同步初始化模組，避免阻塞主迴圈導致指令執行死鎖
	go m.initModem()

	for {
		// 優先順序排程邏輯
		select {
		case <-m.stop:
			logger.Info(fmt.Sprintf("[%s] AT 管理器已停止", m.cfg.ID))
			return
		case req := <-m.cmdChanHigh:
			// 優先處理高優先順序指令
			m.handleCommand(req)
			continue
		default:
			// 如果沒有高優先順序指令，則檢查普通指令
		}

		select {
		case <-m.stop:
			logger.Info(fmt.Sprintf("[%s] AT 管理器已停止", m.cfg.ID))
			return
		case req := <-m.cmdChanHigh: // 再次檢查高優先順序，防止餓死
			m.handleCommand(req)
		case req := <-m.cmdChan:
			m.handleCommand(req)
		case msg := <-m.rxChan:
			// 空閒狀態下的資料處理（主要是 URC）
			if msg.Err != nil {
				logger.Error(fmt.Sprintf("[%s] 串列埠讀取錯誤，模組可能已掉線", m.cfg.ID), "err", msg.Err)
				m.Stop()
				m.notifyDisconnect("serial_read_error")
				return
			}
			if m.isURC(msg.Data) {
				m.handleURC(msg.Data)
			}
		}
	}
}

// handleCommand 處理單個 AT 指令
func (m *Manager) handleCommand(req commandRequest) {
	startTime := time.Now()

	// 傳送指令
	if _, err := m.port.Write([]byte(req.cmd + "\r\n")); err != nil {
		req.errChan <- err
		m.handleFatalSerialRuntimeErr(err, "write", req.cmd)
		return
	}

	// 等待回應
	fullResponse := []string{}
	timeoutTimer := time.NewTimer(req.timeout)
	defer timeoutTimer.Stop()

RespLoop:
	for {
		select {
		case <-timeoutTimer.C:
			// 逾時時嘗試傳送 ESC (0x1B) 以取消可能的掛起操作（如簡訊輸入）
			m.port.Write([]byte{0x1B})
			logger.Warn(fmt.Sprintf("[%s] 指令執行逾時，已傳送 ESC 嘗試恢復", m.cfg.ID), "port", m.atPort, "cmd", req.cmd, "cost", time.Since(startTime).String())
			req.errChan <- errors.New("指令執行逾時")
			if failures, tripped := m.recordATTimeout(req); tripped {
				m.tripATTimeoutWatchdog(req.cmd, failures)
			}
			return

		case msg := <-m.rxChan:
			if msg.Err != nil {
				req.errChan <- msg.Err
				m.handleFatalSerialRuntimeErr(msg.Err, "read", req.cmd)
				return
			}

			line := msg.Data

			if line == "OK" {
				m.resetATTimeoutWatchdog()
				if !req.silent {
					logger.Debug(fmt.Sprintf("[%s] AT 執行成功", m.cfg.ID),
						"cmd", req.cmd,
						"resp", strings.Join(fullResponse, " | "),
						"cost", time.Since(startTime).Truncate(time.Millisecond).String())
				}
				req.respChan <- strings.Join(fullResponse, "\n")
				break RespLoop
			} else if strings.Contains(line, "ERROR") {
				m.resetATTimeoutWatchdog()
				fullResponse = append(fullResponse, line)
				if !req.silent {
					logger.Warn(fmt.Sprintf("[%s] AT 執行失敗", m.cfg.ID),
						"cmd", req.cmd,
						"resp", strings.Join(fullResponse, " | "),
						"cost", time.Since(startTime).Truncate(time.Millisecond).String())
				}
				req.errChan <- fmt.Errorf("裝置回傳錯誤: %s", strings.Join(fullResponse, "\n"))
				break RespLoop
			} else if strings.Contains(line, ">") {
				m.resetATTimeoutWatchdog()
				fullResponse = append(fullResponse, line)
				if !req.silent {
					logger.Debug(fmt.Sprintf("[%s] AT 收到提示", m.cfg.ID),
						"cmd", req.cmd,
						"resp", ">",
						"cost", time.Since(startTime).Truncate(time.Millisecond).String())
				}

				if req.interactive && req.waitPrompt && req.followUp != "" {
					// 收到提示符，立即傳送後續指令
					m.port.Write([]byte(req.followUp))
					// 繼續等待最終回應 (OK/ERROR)
					// 重置 waitPrompt 防止重複觸發
					req.waitPrompt = false
					continue
				}

				req.respChan <- "> "
				break RespLoop
			} else if m.isURC(line) {
				// 對於確認為 URC 的行，始終分發出去
				m.handleURC(line)

				// 但僅當它是某些明確的、完全非同步的事件（如簡訊通知、來電、USSD、插拔卡）時，才將其從目前指令的 fullResponse 中剔除，以免汙染解析器
				// 其餘如 +CIMI:, +QCCID:, +CSQ: 實際上既是 URC 也是指令回顯，必須被目前指令捕獲！
				isPureAsyncURC := func(s string) bool {
					key := urcKey(s)
					switch key {
					case "+CUSD", "+CMTI", "RING", "+CLIP", "+QSIMSTAT", "+QSTKURC", "+QPCMV":
						return true
					}
					return false
				}

				if !isPureAsyncURC(line) {
					fullResponse = append(fullResponse, line)
				}
			} else {
				fullResponse = append(fullResponse, line)
			}
		}
	}
}

// readLoop 專用讀取協程
func (m *Manager) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("[%s] readLoop panic recovered", m.cfg.ID), "err", r)
		}
	}()

	buf := make([]byte, 1024)
	var lineBuf bytes.Buffer

	for {
		select {
		case <-m.stop:
			return
		default:
		}

		n, err := m.port.Read(buf)
		if err != nil {
			errMsg := err.Error()
			// 忽略逾時錯誤和多次讀取無資料錯誤
			if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "multiple Read calls return no data") {
				continue
			}

			// EOF 處理：連續 EOF 超過閾值則判定為裝置已中斷
			if err == io.EOF {
				m.eofCount++
				if m.eofCount >= 30 { // 連續 30 次 EOF（約 3 秒）
					select {
					case <-m.stop:
						return
					default:
						logger.Warn(fmt.Sprintf("[%s] 串列埠連續 %d 次 EOF，判定裝置已中斷", m.cfg.ID, m.eofCount))
						m.rxChan <- rxMsg{Err: fmt.Errorf("連續 %d 次 EOF，判定裝置已中斷", m.eofCount)}
						return
					}
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			m.eofCount = 0 // 非 EOF 錯誤時重置計數

			select {
			case <-m.stop:
				return
			default:
				m.rxChan <- rxMsg{Err: err}
				return
			}
		}

		if n > 0 {
			m.eofCount = 0 // 成功讀取資料，重置 EOF 計數
			// 處理讀取到的資料
			for i := 0; i < n; i++ {
				b := buf[i]
				lineBuf.WriteByte(b)

				// 遇到換行符，或者遇到 '>' 提示符
				// 判定為行結束 (注意處理 > )
				// 這裡的邏輯有點 tricky，因為 "> " 通常是最後兩個字元
				// 簡化邏輯：遇到 \n 傳送；遇到 > 且前一個是 \r 或 \n 或者是行首？
				// 為了穩健，只要遇到 \n 就傳送。
			}

			// 重新實作更簡單的邏輯：
			// 迴圈處理所有換行符
			data := lineBuf.String()
			for {
				idx := strings.IndexByte(data, '\n')
				if idx >= 0 {
					// 有換行符，將前面的內容傳送出去
					line := strings.TrimSpace(data[:idx+1])
					if line != "" {
						select {
						case m.rxChan <- rxMsg{Data: line}:
						case <-m.stop:
							return
						}
					}
					// 更新資料，繼續處理剩餘部分
					data = data[idx+1:]
				} else {
					break
				}
			}

			// 重置 buffer 並寫入剩餘資料
			lineBuf.Reset()
			lineBuf.WriteString(data)

			// 檢查剩餘部分是否是特殊提示符
			// 注意：有些模組回傳 "\r\n> "，上面的迴圈會處理掉 "\r\n"，剩下 "> "
			if strings.HasSuffix(data, "> ") || data == "> " || strings.HasSuffix(data, ">") {
				// 遇到特殊提示符
				select {
				case m.rxChan <- rxMsg{Data: "> "}:
				case <-m.stop:
					return
				}
				// 清空緩衝區，因為已經消費了提示符
				lineBuf.Reset()
			}
		}
	}
}

// initModem 初始化模組
func (m *Manager) initModem() {
	if m.pureQMIBackend() {
		m.markReady()
		return
	}

	time.Sleep(150 * time.Millisecond)

	// 1. 探測連通性
	_, err := m.ExecuteATSilent("AT", 2*time.Second)
	if err != nil {
		logger.Warn(fmt.Sprintf("[%s] AT 探測失敗", m.cfg.ID), "err", err)
		m.markReady()
		return
	}

	// 2. 初始化指令序列
	initCmds := []string{
		"ATE0",              // 關閉回顯
		"AT+CMGF=0",         // PDU 模式
		"AT+CNMI=2,1,0,0,0", // 新簡訊上報 +CMTI
		"AT+CLIP=1",         // 啟用來電號碼顯示 (+CLIP URC)
		"AT+QPCMV=1,2",      // 開啟 UAC 語音模式 (PCM → ALSA 橋接必須)
	}

	for _, cmd := range initCmds {
		// 這些初始化指令使用 ExecuteATSilent 降低日誌噪音，避免使用者誤解全在走 AT
		m.ExecuteATSilent(cmd, 2*time.Second)
		time.Sleep(100 * time.Millisecond)
	}

	m.markReady()

	// 3. 擷取裝置資訊
	m.collectDeviceInfo()
	logger.Info(fmt.Sprintf("[%s] 模組初始化完成", m.cfg.ID), "imei", m.imei, "iccid", m.iccid)
}

// RefreshDeviceInfo 重新擷取裝置資訊（切卡後需要更新快取）
func (m *Manager) RefreshDeviceInfo() {
	m.collectDeviceInfo()
}

// collectDeviceInfo 擷取裝置資訊 (IMEI, ICCID, IMSI, 電信業者, 訊號等)
func (m *Manager) collectDeviceInfo() {
	// 1. 無鎖階段：執行所有 AT 指令
	var imei, firmware, iccid, imsi, msisdn, operator, apn, networkMode, networkDuplex string
	var simInserted bool
	var regStatus, imsStatus int
	var regStatusText, lac, cellID string
	var signalDBM, signalRSRQ, signalRSRP int = -999, 0, 0
	var usbnetMode int = -1

	if v, err := m.QueryIMEI(); err == nil {
		imei = v
	}
	if v, err := m.QueryFirmware(); err == nil {
		firmware = v
	}
	if v, err := m.QuerySIMInserted(); err == nil {
		simInserted = v
	}
	if simInserted {
		if v, err := m.QueryIMSI(); err == nil {
			imsi = v
		}
		if v, err := m.QueryMSISDN(); err == nil {
			msisdn = v
		}
	}
	if v, err := m.QueryICCID(); err == nil {
		iccid = v
	}
	if v, err := m.QueryOperator(); err == nil {
		operator = v
	}
	if st, text, lacV, cellV, err := m.QueryRegistration(); err == nil {
		regStatus = st
		regStatusText = text
		lac = lacV
		cellID = cellV
	}
	if _, dbm, err := m.QueryCSQ(); err == nil && dbm != -999 {
		signalDBM = dbm
	}
	if rsrp, rsrq, err := m.QueryServingCellLTE(); err == nil {
		signalRSRP = rsrp
		signalRSRQ = rsrq
	}
	if v, err := m.QueryAPN(); err == nil {
		apn = v
	}
	if v, err := m.QueryIMSStatus(); err == nil {
		imsStatus = v
	}
	if mode, duplex, err := m.QueryNetworkModeAndDuplex(); err == nil {
		networkMode = mode
		networkDuplex = duplex
	}
	if v, err := m.QueryUSBNetMode(); err == nil {
		usbnetMode = v
	}

	// 2. 有鎖階段：統一更新狀態
	m.infoMu.Lock()
	defer m.infoMu.Unlock()

	if imei != "" {
		m.imei = imei
	}
	if firmware != "" {
		m.firmware = firmware
	}
	if iccid != "" {
		m.iccid = iccid
	}
	if imsi != "" {
		m.imsi = imsi
	}
	// 本機號碼可能沒有寫入 SIM。每次重新擷取都覆蓋快取，避免換卡後
	// 繼續顯示上一張卡的號碼。
	m.msisdn = msisdn
	if operator != "" {
		m.operator = operator
	}
	m.simInserted = simInserted
	if signalDBM != -999 {
		m.signalDBM = signalDBM
	}
	if signalRSRP != 0 {
		m.signalRSRP = signalRSRP
	}
	if signalRSRQ != 0 {
		m.signalRSRQ = signalRSRQ
	}
	m.regStatus = regStatus
	m.regStatusText = regStatusText
	m.lac = lac
	m.cellID = cellID
	m.apn = apn
	m.imsStatus = imsStatus
	m.networkMode = networkMode
	m.networkDuplex = networkDuplex
	m.usbnetMode = usbnetMode
}

// getRegStatusText 回傳網路註冊狀態文字
func (m *Manager) getRegStatusText(status int) string {
	switch status {
	case 0:
		return "未註冊"
	case 1:
		return "已註冊(本地)"
	case 2:
		return "搜尋中"
	case 3:
		return "註冊被拒"
	case 4:
		return "未知"
	case 5:
		return "已註冊(漫遊)"
	default:
		return "未知"
	}
}

// GetIMEI 回傳裝置 IMEI
func (m *Manager) GetIMEI() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imei
}

// GetICCID 回傳目前 SIM 卡 ICCID
func (m *Manager) GetICCID() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.iccid
}

// GetIMSI 回傳目前 SIM 卡 IMSI
func (m *Manager) GetIMSI() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imsi
}

// GetOperator 回傳電信業者名稱
func (m *Manager) GetOperator() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.operator
}

// GetSignalDBM 回傳訊號強度 (dBm)
func (m *Manager) GetSignalDBM() int {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.signalDBM
}

// GetFirmware 回傳韌體版本
func (m *Manager) GetFirmware() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.firmware
}

// IsSimInserted 回傳是否插入 SIM 卡
func (m *Manager) IsSimInserted() bool {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.simInserted
}

// GetRegStatus 回傳網路註冊狀態
func (m *Manager) GetRegStatus() (int, string) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.regStatus, m.regStatusText
}

// GetCellInfo 回傳基地台資訊 (LAC, CellID)
func (m *Manager) GetCellInfo() (string, string) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.lac, m.cellID
}

// GetSignalLTE 回傳 LTE 詳細訊號 (RSRP, RSRQ)
func (m *Manager) GetSignalLTE() (int, int) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.signalRSRP, m.signalRSRQ
}

// GetAPN 回傳目前 APN
func (m *Manager) GetAPN() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.apn
}

// GetIMSStatus 回傳 IMS 註冊狀態
func (m *Manager) GetIMSStatus() int {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imsStatus
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

// GetFullStatus 回傳完整狀態資訊
type DeviceStatus struct {
	IMEI            string           `json:"imei"`
	Firmware        string           `json:"firmware"`
	ICCID           string           `json:"iccid"`
	IMSI            string           `json:"imsi"`
	PhoneNumber     string           `json:"phone_number,omitempty"`
	NativeSPN       string           `json:"native_spn,omitempty"`
	NativeMCC       string           `json:"native_mcc,omitempty"`
	NativeMNC       string           `json:"native_mnc,omitempty"`
	GID1            string           `json:"gid1,omitempty"`
	GID2            string           `json:"gid2,omitempty"`
	PNN             []PNNRecord      `json:"pnn,omitempty"`
	OPL             []OPLRecord      `json:"opl,omitempty"`
	SIMServiceTable *SIMServiceTable `json:"sim_service_table,omitempty"`
	Operator        string           `json:"operator"`
	SimInserted     bool             `json:"sim_inserted"`
	SignalDBM       int              `json:"signal_dbm"`
	SignalRSRP      int              `json:"signal_rsrp"`
	SignalRSRQ      int              `json:"signal_rsrq"`
	SignalSINR      int              `json:"signal_sinr,omitempty"`
	NR5GSignalSINR  int              `json:"nr5g_signal_sinr,omitempty"`
	RadioBand       string           `json:"radio_band,omitempty"`
	RadioChannel    uint32           `json:"radio_channel,omitempty"`
	RegStatus       int              `json:"reg_status"`
	RegStatusText   string           `json:"reg_status_text"`
	PSAttached      bool             `json:"ps_attached"`
	LAC             string           `json:"lac"`
	CellID          string           `json:"cell_id"`
	APN             string           `json:"apn"`
	IMSStatus       int              `json:"ims_status"`
	NetworkMode     string           `json:"network_mode"`
	NetworkDuplex   string           `json:"network_duplex"`
	USBNetMode      int              `json:"usbnet_mode"`
	OperatingMode   *int             `json:"operating_mode,omitempty"`
}

func (m *Manager) GetFullStatus() DeviceStatus {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return DeviceStatus{
		IMEI:          m.imei,
		Firmware:      m.firmware,
		ICCID:         m.iccid,
		IMSI:          m.imsi,
		PhoneNumber:   m.msisdn,
		Operator:      m.operator,
		SimInserted:   m.simInserted,
		SignalDBM:     m.signalDBM,
		SignalRSRP:    m.signalRSRP,
		SignalRSRQ:    m.signalRSRQ,
		RegStatus:     m.regStatus,
		RegStatusText: m.regStatusText,
		LAC:           m.lac,
		CellID:        m.cellID,
		APN:           m.apn,
		IMSStatus:     m.imsStatus,
		NetworkMode:   m.networkMode,
		NetworkDuplex: m.networkDuplex,
		USBNetMode:    m.usbnetMode,
		OperatingMode: nil,
	}
}

// RefreshStatus 重新整理裝置狀態 (訊號、電信業者、SIM)，並在發現 SIM 卡掉線時觸發警告
func (m *Manager) RefreshStatus(onAlert func(msg string), onRecover func(msg string)) {
	// 1. 在鎖外執行耗時的 AT 指令
	var operator, networkMode, networkDuplex string
	var signalDBM, signalRSRP, signalRSRQ int = -999, 0, 0

	// 檢查 SIM 卡是否存活
	simInserted, simErr := m.QuerySIMInserted()

	if v, err := m.QueryOperator(); err == nil {
		operator = v
	}
	if _, dbm, err := m.QueryCSQ(); err == nil && dbm != -999 {
		signalDBM = dbm
	}
	if mode, duplex, err := m.QueryNetworkModeFallbackAndDuplex(); err == nil {
		networkMode = mode
		networkDuplex = duplex
	}

	// 2. 取得鎖並更新狀態
	m.infoMu.Lock()
	defer m.infoMu.Unlock()

	if operator != "" {
		m.operator = operator
	}
	if signalDBM != -999 {
		m.signalDBM = signalDBM
	}
	if networkMode != "" {
		m.networkMode = networkMode
		m.networkDuplex = networkDuplex
	}
	if signalRSRP != 0 {
		m.signalRSRP = signalRSRP
	}
	if signalRSRQ != 0 {
		m.signalRSRQ = signalRSRQ
	}

	// 處理 SIM 卡警告邏輯 (連續 3 次探測失敗)
	if simErr != nil || !simInserted {
		m.simFailCount++
		if m.simFailCount >= 3 && !m.simAlerting {
			m.simAlerting = true
			errDetail := "SIM 卡未插入或狀態異常"
			if simErr != nil {
				errDetail = fmt.Sprintf("AT+CPIN 查詢失敗: %v", simErr)
			}
			logger.Warn(fmt.Sprintf("[%s] 定時巡檢發現 SIM 卡掉線", m.cfg.ID), "err", errDetail)
			if onAlert != nil {
				// 非同步傳送警告避免阻塞鎖內時間
				go onAlert(fmt.Sprintf("⚠️ 裝置 %s SIM 卡掉線: %s", m.cfg.ID, errDetail))
			}
		}
	} else {
		if m.simAlerting {
			logger.Info(fmt.Sprintf("[%s] 定時巡檢發現 SIM 卡已恢復", m.cfg.ID))
			if onRecover != nil {
				go onRecover(fmt.Sprintf("✅ 裝置 %s SIM 卡已恢復正常", m.cfg.ID))
			}
		}
		m.simFailCount = 0
		m.simAlerting = false
	}
}

// isURC 判斷是否為 URC
func (m *Manager) isURC(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	// 排除確認為同步指令的非同步回顯，避免被 URC 處理函式攔截並報「未分類」
	if strings.HasPrefix(s, "+CSIM:") || strings.HasPrefix(s, "+CGLA:") || strings.HasPrefix(s, "+CCHO:") || strings.HasPrefix(s, "+CMGR:") || strings.HasPrefix(s, "+CMGS:") || strings.HasPrefix(s, "+QENG:") {
		return false
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "^") || strings.HasPrefix(s, "$") {
		return true
	}
	switch s {
	case "RING", "RDY", "SMS Ready", "Call Ready", "NORMAL POWER DOWN", "NO CARRIER", "BUSY", "NO ANSWER":
		return true
	default:
		return false
	}
}

// SubscribeRDY 訂閱一次性 RDY 事件。
// 呼叫方應在發出會觸發模組重啟的操作 *之前* 先呼叫本方法，然後等待回傳的 channel。
// 模組重啟並行出 RDY URC 後，channel 會被關閉（可透過 `<-ch` 或 `select` 接收）。
func (m *Manager) SubscribeRDY() <-chan struct{} {
	ch := make(chan struct{})
	m.rdyMu.Lock()
	m.rdySubs = append(m.rdySubs, ch)
	m.rdyMu.Unlock()
	return ch
}

// dispatchRDY 內部呼叫：廣播 RDY 事件並清空訂閱清單
func (m *Manager) dispatchRDY() {
	m.rdyMu.Lock()
	subs := m.rdySubs
	m.rdySubs = nil
	m.rdyMu.Unlock()
	for _, ch := range subs {
		close(ch)
	}
}

func (m *Manager) dispatchSIMStatusURC(inserted *bool, state string) {
	m.infoMu.RLock()
	handler := m.simStatusHandler
	m.infoMu.RUnlock()
	if handler != nil {
		go handler(inserted, state)
	}
}

// handleURC 處理 URC
func (m *Manager) handleURC(line string) {
	s := strings.TrimSpace(line)
	if s == "" {
		return
	}

	fr := m.formatURC(s)
	msg := fmt.Sprintf("[%s] %s", m.cfg.ID, fr.Msg)
	switch fr.Level {
	case urcLogWarn:
		logger.Warn(msg, fr.Fields...)
	case urcLogInfo:
		logger.Info(msg, fr.Fields...)
	default:
		logger.Debug(msg, fr.Fields...)
	}

	// 模組重啟訊號：廣播給所有 SubscribeRDY() 的等待方。
	// 部分 EC20 韌體重啟後不發 RDY，而是直接發 +CPIN: READY，兩者都作為就緒訊號處理。
	if fr.Key == "RDY" {
		m.dispatchRDY()
	}
	if fr.Key == "+CPIN" {
		state := ""
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "state" {
				state, _ = fr.Fields[i+1].(string)
				if state == "READY" {
					m.dispatchRDY()
				}
				break
			}
		}
		m.dispatchSIMStatusURC(nil, state)
	}

	if fr.Key == "+QSIMSTAT" {
		var inserted *bool
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "inserted" {
				if v, ok := fr.Fields[i+1].(int); ok && v >= 0 {
					b := v == 1
					inserted = &b
				}
				break
			}
		}
		m.dispatchSIMStatusURC(inserted, "")
	}

	// 分發 +CUSD USSD 回應到等待通道
	if fr.Key == "+CUSD" {
		var n, dcs int
		var text string
		for i := 0; i < len(fr.Fields)-1; i += 2 {
			key, _ := fr.Fields[i].(string)
			switch key {
			case "n":
				n, _ = fr.Fields[i+1].(int)
			case "dcs":
				dcs, _ = fr.Fields[i+1].(int)
			case "text":
				text, _ = fr.Fields[i+1].(string)
			}
		}
		result := USSDResult{Status: n, RawText: text, DCS: dcs}
		result.Text = m.decodeUSSDText(text, dcs)
		select {
		case m.ussdChan <- result:
		default:
			// 沒有人在等待，丟棄
			logger.Debug(fmt.Sprintf("[%s] USSD 回應無人等待，已丟棄", m.cfg.ID), "text", result.Text)
		}
	}

	if fr.Key == "+CMTI" && fr.CMTIIndex != "" {
		index := fr.CMTIIndex
		storage := fr.CMTIStorage
		m.infoMu.RLock()
		disabled := m.disableURCRead
		handler := m.newSMSHandler
		m.infoMu.RUnlock()

		if handler != nil {
			go handler(index)
			return
		}
		if !disabled {
			go m.readAndProcessSMSFromStorage(storage, index)
		} else {
			logger.Debug(fmt.Sprintf("[%s] 收到 URC 但已禁用自動讀取 (QMI 接管)", m.cfg.ID), "index", index, "storage", storage)
		}
	}

	// 分發 RING 來電事件
	if fr.Key == "RING" {
		m.infoMu.RLock()
		cb := m.ringCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分發對方掛斷事件 NO CARRIER
	if fr.Key == "NO CARRIER" {
		m.infoMu.RLock()
		cb := m.hangupCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分發對方接聽外呼事件 (CONNECT)
	if fr.Key == "CONNECT" || fr.Key == "MO CONNECTED" {
		m.infoMu.RLock()
		cb := m.connectCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分發 +CLIP 來電號碼
	if fr.Key == "+CLIP" {
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "number" {
				if number, _ := fr.Fields[i+1].(string); number != "" {
					m.infoMu.RLock()
					cb := m.clipCallback
					m.infoMu.RUnlock()
					if cb != nil {
						go cb(number)
					}
				}
				break
			}
		}
	}

	// 分發 +QPCMV 流控事件 (0=模組忙, 1=就緒)
	if fr.Key == "+QPCMV" {
		rest := parseURCAfterColon(s)
		if v, ok := parseInt(strings.TrimSpace(rest)); ok {
			if m.qpcmvChan != nil {
				select {
				case m.qpcmvChan <- v:
				default:
				}
			}
		}
	}
}

// readAndProcessSMS 讀取並處理簡訊
func (m *Manager) readAndProcessSMS(index string) {
	// 公開給外部呼叫的封裝 (如果需要)
	m.ReadAndProcessSMS(index)
}

// ReadAndProcessSMS 公開方法：讀取並處理簡訊
func (m *Manager) ReadAndProcessSMS(index string) {
	m.readAndProcessSMSFromStorage("", index)
}

// ReadAndProcessSMSFromStorage 讀取指定 AT 簡訊儲存中的簡訊。
func (m *Manager) ReadAndProcessSMSFromStorage(storage, index string) {
	m.readAndProcessSMSFromStorage(storage, index)
}

func (m *Manager) readAndProcessSMSFromStorage(storage, index string) {
	index, ok := normalizeSMSIndex(index)
	if !ok {
		logger.Warn(fmt.Sprintf("[%s] 簡訊索引非法，跳過讀取", m.cfg.ID), "index", index)
		return
	}

	normalizedStorage, hasStorage := normalizeSMSStorage(storage)
	if hasStorage {
		restore, switched := m.switchSMSStorageForRead(normalizedStorage)
		if !switched {
			logger.Warn(fmt.Sprintf("[%s] 切換簡訊儲存失敗，跳過讀取以避免誤刪其他儲存簡訊", m.cfg.ID), "index", index, "storage", normalizedStorage)
			return
		}
		if restore != nil {
			defer restore()
		}
	}

	fields := []any{"index", index}
	if hasStorage {
		fields = append(fields, "storage", normalizedStorage)
	}
	logger.Info(fmt.Sprintf("[%s] 讀取簡訊 (AT)", m.cfg.ID), fields...)

	// 讀取簡訊 PDU
	resp, err := m.ExecuteAT("AT+CMGR="+index, 5*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 讀取簡訊失敗", m.cfg.ID), "index", index, "err", err)
		return
	}

	// 解析 PDU
	pduHex, _ := extractSMSPDUAfterPrefix(resp, "+CMGR:")

	if pduHex == "" || pduHex == "OK" {
		logger.Warn(fmt.Sprintf("[%s] 未找到 PDU 資料", m.cfg.ID))
		return
	}

	// 解碼 PDU
	sender, content, timestamp := m.decodePDU(pduHex)

	// 如果內容為空（說明是分段且未完成），則不進行回呼
	if content == "" {
		// 刪除已讀分段 (非常重要，否則SIM卡滿了)
		m.ExecuteAT("AT+CMGD="+index, 3*time.Second)
		return
	}

	logger.Debug(fmt.Sprintf("[%s] 簡訊內容", m.cfg.ID), "sender", sender, "content", content)

	// 回呼通知
	if m.smsCallback != nil {
		m.smsCallback(sender, content, timestamp)
	}

	// 刪除已讀簡訊
	m.ExecuteAT("AT+CMGD="+index, 3*time.Second)
}

func normalizeSMSIndex(index string) (string, bool) {
	index = strings.TrimSpace(index)
	if index == "" {
		return "", false
	}
	for _, ch := range index {
		if ch < '0' || ch > '9' {
			return "", false
		}
	}
	return index, true
}

func normalizeSMSStorage(storage string) (string, bool) {
	storage = strings.ToUpper(strings.Trim(strings.TrimSpace(storage), `"`))
	if storage == "" || len(storage) > 8 {
		return "", false
	}
	for _, ch := range storage {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return "", false
	}
	return storage, true
}

func parseCPMSStorages(resp string) []string {
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CPMS:") {
			continue
		}
		fields := parseCommaFields(parseURCAfterColon(line))
		storages := make([]string, 0, 3)
		for i := 0; i < len(fields); i += 3 {
			if storage, ok := normalizeSMSStorage(fields[i]); ok {
				storages = append(storages, storage)
			}
		}
		return storages
	}
	return nil
}

func cpmsSetCommand(storages ...string) string {
	normalized := make([]string, 0, len(storages))
	for _, storage := range storages {
		if s, ok := normalizeSMSStorage(storage); ok {
			normalized = append(normalized, s)
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	if len(normalized) == 1 {
		normalized = []string{normalized[0], normalized[0], normalized[0]}
	}
	if len(normalized) > 3 {
		normalized = normalized[:3]
	}

	quoted := make([]string, 0, len(normalized))
	for _, storage := range normalized {
		quoted = append(quoted, fmt.Sprintf("%q", storage))
	}
	return "AT+CPMS=" + strings.Join(quoted, ",")
}

func (m *Manager) switchSMSStorageForRead(storage string) (func(), bool) {
	targetCmd := cpmsSetCommand(storage)
	if targetCmd == "" {
		return nil, true
	}

	var previous []string
	if resp, err := m.ExecuteAT("AT+CPMS?", 3*time.Second); err == nil {
		previous = parseCPMSStorages(resp)
		if len(previous) > 0 && strings.EqualFold(previous[0], storage) {
			return nil, true
		}
	} else {
		logger.Warn(fmt.Sprintf("[%s] 查詢簡訊儲存失敗，將直接嘗試切換", m.cfg.ID), "storage", storage, "err", err)
	}

	if _, err := m.ExecuteAT(targetCmd, 5*time.Second); err != nil {
		logger.Warn(fmt.Sprintf("[%s] 切換簡訊儲存失敗", m.cfg.ID), "storage", storage, "err", err)
		return nil, false
	}

	restoreCmd := cpmsSetCommand(previous...)
	if restoreCmd == "" || restoreCmd == targetCmd {
		return nil, true
	}
	return func() {
		if _, err := m.ExecuteAT(restoreCmd, 5*time.Second); err != nil {
			logger.Warn(fmt.Sprintf("[%s] 恢復簡訊儲存失敗", m.cfg.ID), "storage", previous, "err", err)
		}
	}, true
}

// Reboot 重啟模組 (AT+CFUN=1,1)
func (m *Manager) Reboot() error {
	logger.Warn(fmt.Sprintf("[%s] 正在重啟模組...", m.cfg.ID))
	_, err := m.ExecuteAT("AT+CFUN=1,1", 5*time.Second)
	return err
}

// cleanupOldFragments 清理過期的簡訊分段
func (m *Manager) cleanupOldFragments() {
	m.reassembler.Cleanup(10 * time.Minute)
}

// decodePDU 解碼 PDU
func (m *Manager) decodePDU(raw string) (sender, content string, timestamp time.Time) {
	timestamp = time.Now()

	b, err := hex.DecodeString(raw)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] PDU 十六進位解碼失敗", m.cfg.ID), "err", err)
		content = fmt.Sprintf("[解碼失敗] %s", raw)
		return
	}

	// 跳過 SMSC 頭部
	if len(b) > 0 {
		smscLen := int(b[0])
		if len(b) > smscLen+1 {
			b = b[smscLen+1:]
		}
	}

	var concat smscodec.ConcatInfo
	sender, content, msgTime, concat, err := smscodec.DecodeDeliverTPDU(b)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] TPDU 解析失敗", m.cfg.ID), "err", err)
		content = fmt.Sprintf("[PDU 解析失敗] %s", raw)
		return
	}
	if !msgTime.IsZero() {
		timestamp = msgTime
	}

	if concat.IsConcat {
		logger.Debug(fmt.Sprintf("[%s] 收到簡訊分段", m.cfg.ID), "ref", concat.Ref, "seq", concat.Seq, "total", concat.Total)
		complete, full := m.reassembler.Add(sender, concat, content)
		if !complete {
			return "", "", time.Time{}
		}
		content = full
		logger.Info(fmt.Sprintf("[%s] 長簡訊重組完成", m.cfg.ID), "total", concat.Total)
		return sender, content, timestamp
	}

	return sender, content, timestamp
}

// ExecuteAT 執行 AT 指令 (普通優先順序)
func (m *Manager) ExecuteAT(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, false, false)
}

// ExecuteATSilent 靜默執行 AT 指令 (普通優先順序)
func (m *Manager) ExecuteATSilent(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, true, false)
}

// ExecuteATHigh 執行 AT 指令 (高優先順序)
func (m *Manager) ExecuteATHigh(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, false, true)
}

// executeAT 內部通用的 AT 指令執行邏輯
func (m *Manager) executeAT(cmd string, timeout time.Duration, silent, highPriority bool) (string, error) {
	if !m.HasATPort() {
		return "", errors.New("目前裝置沒有可用 AT 埠")
	}
	if !m.CanExecuteAT() {
		return "", errors.New("AT 管理器未啟動或不可用")
	}
	if !m.healthy {
		return "", errors.New("裝置異常")
	}

	// 從池中取得請求物件
	req := m.reqPool.Get().(*commandRequest)
	// 重置欄位
	req.cmd = cmd
	req.timeout = timeout
	req.silent = silent
	req.highPriority = highPriority
	req.interactive = false
	req.waitPrompt = false
	req.followUp = ""

	// 確保回收資源
	defer func() {
		// 清空通道以防萬一
		select {
		case <-req.respChan:
		default:
		}
		select {
		case <-req.errChan:
		default:
		}
		m.reqPool.Put(req)
	}()

	// 根據優先順序選擇通道
	targetChan := m.cmdChan
	if highPriority {
		targetChan = m.cmdChanHigh
	}

	select {
	case targetChan <- *req: // 注意：這裡傳送的是值複製，但這不影響 respChan/errChan 的引用
		select {
		case resp := <-req.respChan:
			return resp, nil
		case err := <-req.errChan:
			return "", err
		case <-m.stop:
			select {
			case err := <-req.errChan:
				return "", err
			case resp := <-req.respChan:
				return resp, nil
			default:
			}
			return "", errors.New("manager stopped")
		}
	case <-time.After(5 * time.Second): // 通道寫入逾時 (佇列滿)
		return "", errors.New("command queue full")
	case <-m.stop:
		return "", errors.New("manager stopped")
	}
}

// SetBusy 設定忙碌狀態
func (m *Manager) SetBusy(busy bool) {
	m.busyMu.Lock()
	m.busy = busy
	m.busyMu.Unlock()
}

// IsBusy 查詢忙碌狀態
func (m *Manager) IsBusy() bool {
	m.busyMu.Lock()
	defer m.busyMu.Unlock()
	return m.busy
}

// IsHealthy 回傳健康狀態
func (m *Manager) IsHealthy() bool {
	return m.healthy && m.running
}

// HasATPort 回傳目前管理器是否設定了可用的 AT 埠。
func (m *Manager) HasATPort() bool {
	return strings.TrimSpace(m.atPort) != ""
}

// ATPort 回傳設定中的 AT 埠路徑。純 QMI 模式會保留該值供人工 AT 終端使用。
func (m *Manager) ATPort() string {
	return strings.TrimSpace(m.atPort)
}

// CanExecuteAT 回傳目前管理器是否已啟動，可接受 AT 指令。
func (m *Manager) CanExecuteAT() bool {
	return !m.pureQMIBackend() && m.HasATPort() && m.running
}

func (m *Manager) SetAPDUArbiter(arbiter *apduarbiter.Arbiter) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	if m.apduSessions == nil {
		m.apduSessions = make(map[int]apduSessionInfo)
	}
	if m.apduArbiter == arbiter {
		return
	}
	clear(m.apduSessions)
	m.apduArbiter = arbiter
}

func (m *Manager) acquireAPDUTransportLease(timeout time.Duration, owner string, class apduarbiter.APDUClass, channel int) (*apduarbiter.Lease, error) {
	m.apduLeaseMu.Lock()
	arbiter := m.apduArbiter
	m.apduLeaseMu.Unlock()
	if arbiter == nil {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return arbiter.AcquireTransport(ctx, apduarbiter.Request{
		Owner:   owner,
		Mode:    "AT",
		Class:   class,
		Channel: channel,
	})
}

func (m *Manager) bindAPDUSession(channel int, owner string, class ...apduarbiter.APDUClass) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	if m.apduSessions == nil {
		m.apduSessions = make(map[int]apduSessionInfo)
	}
	sessionClass := apduarbiter.APDUClassEUICCWrite
	if len(class) > 0 && class[0] != "" {
		sessionClass = class[0]
	}
	m.apduSessions[channel] = apduSessionInfo{
		Channel:  channel,
		Owner:    strings.TrimSpace(owner),
		Class:    sessionClass,
		OpenedAt: time.Now(),
	}
}

func (m *Manager) getAPDUSession(channel int) (apduSessionInfo, bool) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	session, ok := m.apduSessions[channel]
	return session, ok
}

func (m *Manager) hasAPDUSession(channel int) bool {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	_, ok := m.apduSessions[channel]
	return ok
}

func (m *Manager) takeAPDUSession(channel int) (apduSessionInfo, bool) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	session, ok := m.apduSessions[channel]
	delete(m.apduSessions, channel)
	return session, ok
}

func (m *Manager) releaseAllAPDULeases(reason string) {
	m.apduLeaseMu.Lock()
	count := len(m.apduSessions)
	clear(m.apduSessions)
	m.apduLeaseMu.Unlock()

	if count > 0 {
		logger.Warn(fmt.Sprintf("[%s] APDU logical session registry 已清理", m.cfg.ID), "reason", reason, "session_count", count)
	}
}

// Rotate 執行 IP 切換
func (m *Manager) Rotate() error {
	m.SetBusy(true)
	defer m.SetBusy(false)

	logger.Info(fmt.Sprintf("[%s] 開始 IP 切換", m.cfg.ID))

	if err := m.SetAttach(false); err != nil {
		return fmt.Errorf("PS 域脫附失敗: %w", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := m.SetAttach(true); err != nil {
		return fmt.Errorf("PS 域附著失敗: %w", err)
	}

	logger.Info(fmt.Sprintf("[%s] IP 切換完成", m.cfg.ID))
	return nil
}

// CheckSignal 檢查訊號強度
func (m *Manager) CheckSignal() (int, error) {
	resp, err := m.ExecuteAT("AT+CSQ", 3*time.Second)
	if err != nil {
		return 0, err
	}

	rssi, _, ok := parseCSQ(resp)
	if ok {
		return rssi, nil
	}

	return 0, errors.New("無法解析訊號強度")
}

// Close 關閉管理器
func (m *Manager) Close() error {
	m.Stop()
	return nil
}

// CheckAllSMS 檢查所有簡訊（輪詢模式）
func (m *Manager) CheckAllSMS() {
	if m.IsBusy() {
		return
	}

	pdus, err := m.SMSListAllPDU()
	if err != nil {
		logger.Warn(fmt.Sprintf("[%s] 檢查簡訊失敗", m.cfg.ID), "err", err)
		return
	}

	if len(pdus) == 0 {
		return
	}

	for _, pduHex := range pdus {
		sender, content, timestamp := m.decodePDU(pduHex)
		if m.smsCallback != nil && content != "" {
			m.smsCallback(sender, content, timestamp)
		}
	}

	// 刪除所有簡訊
	_ = m.SMSDeleteAll()
}

// DeleteSMS 刪除指定索引的簡訊
func (m *Manager) DeleteSMS(index uint32) error {
	_, err := m.ExecuteAT(fmt.Sprintf("AT+CMGD=%d", index), 3*time.Second)
	return err
}

// SendSMS 使用 PDU 模式傳送簡訊
func (m *Manager) SendSMS(phone, message string) error {
	return m.SendSMSWithOptions(phone, message, smscodec.SubmitOptions{})
}

// SendSMSWithOptions 使用 PDU 模式傳送簡訊，並允許呼叫方指定文字編碼策略。
func (m *Manager) SendSMSWithOptions(phone, message string, opts smscodec.SubmitOptions) error {
	m.SetBusy(true)
	defer m.SetBusy(false)

	logger.Info(fmt.Sprintf("[%s] 準備傳送簡訊 (PDU)", m.cfg.ID), "to", phone)

	// 確保處於 PDU 模式
	if _, err := m.ExecuteATHigh("AT+CMGF=0", 3*time.Second); err != nil {
		return fmt.Errorf("設定 PDU 模式失敗: %w", err)
	}

	// 建置 PDUs
	pduHexList, tpduLenList, err := m.buildSMSPDUsWithOptions(phone, message, opts)
	if err != nil {
		return fmt.Errorf("建置 PDU 失敗: %w", err)
	}

	for i, pduHex := range pduHexList {
		tpduLen := tpduLenList[i]
		logger.Debug(fmt.Sprintf("[%s] PDU 編碼完成 (分段 %d/%d)", m.cfg.ID, i+1, len(pduHexList)), "pdu", pduHex, "tpdu_len", tpduLen)

		req := commandRequest{
			cmd:          fmt.Sprintf("AT+CMGS=%d", tpduLen), // PDU 長度 (不含 SMSC)
			respChan:     make(chan string, 1),
			errChan:      make(chan error, 1),
			timeout:      20 * time.Second, // 增加逾時時間，因為包含兩步
			highPriority: true,
			interactive:  true,
			waitPrompt:   true,
			followUp:     pduHex + "\x1A", // PDU + Ctrl+Z
		}

		// 使用高優先順序通道原子執行
		select {
		case m.cmdChanHigh <- req:
		case <-time.After(5 * time.Second):
			return errors.New("command queue full")
		}

		// 等待最終回應 (OK)
		select {
		case resp := <-req.respChan:
			if !strings.Contains(resp, "OK") && !strings.Contains(resp, "+CMGS:") {
				return fmt.Errorf("傳送分段 %d 失敗: %s", i+1, resp)
			}
		case err := <-req.errChan:
			return fmt.Errorf("傳送分段 %d 失敗: %w", i+1, err)
		case <-time.After(20 * time.Second):
			return errors.New("傳送逾時")
		}

		// 稍微等待下一段發信，防止模組佇列溢位
		if i < len(pduHexList)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	logger.Info(fmt.Sprintf("[%s] 簡訊已傳送", m.cfg.ID))
	return nil
}

// buildSMSPDUs 建置多段 SMS-SUBMIT PDU
// 回傳: PDU 十六進位字串清單, TPDU 長度清單 (不含 SMSC), 錯誤
func (m *Manager) buildSMSPDUs(phone, message string) ([]string, []int, error) {
	return m.buildSMSPDUsWithOptions(phone, message, smscodec.SubmitOptions{})
}

func (m *Manager) buildSMSPDUsWithOptions(phone, message string, opts smscodec.SubmitOptions) ([]string, []int, error) {
	tpduBytesList, tpduLenList, err := smscodec.BuildSubmitTPDUsWithOptions(phone, message, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("PDU 編碼失敗: %w", err)
	}

	// SMSC 使用預設 (長度位元組為 00)
	smsc := []byte{0x00}
	var pduHexList []string

	for _, tpduBytes := range tpduBytesList {
		// 完整 PDU = SMSC + TPDU
		fullPDU := append(smsc, tpduBytes...)

		// 轉換為十六進位
		pduHex := strings.ToUpper(hex.EncodeToString(fullPDU))
		pduHexList = append(pduHexList, pduHex)
	}

	return pduHexList, tpduLenList, nil
}

// USSDResult USSD 工作階段回應結果
type USSDResult struct {
	Status  int    `json:"status"`   // 0=無需操作, 1=需要使用者回覆, 2=工作階段結束, 5=網路逾時
	Text    string `json:"text"`     // 解碼後的可讀文字
	RawText string `json:"raw_text"` // 原始文字（除錯用）
	DCS     int    `json:"dcs"`      // 資料編碼方案
}

// decodeUSSDText 根據 DCS (Data Coding Scheme) 解碼 USSD 文字
// 參考 3GPP TS 23.038 的編碼方案
func (m *Manager) decodeUSSDText(raw string, dcs int) string {
	if raw == "" {
		return ""
	}

	// 判斷是否為 Hex 字串（偶數長度、全 hex 字元）
	isHex := smscodec.IsHexString(raw)

	// DCS 高 4 位判斷編碼型別
	// 0x00-0x03 (0-3): GSM 7-bit
	// 0x04-0x07 (4-7): 8-bit data
	// 0x08-0x0B (8-11): UCS2
	// 0x0F (15): GSM 7-bit (unspecified)
	// 0x48 (72): UCS2
	codingGroup := (dcs >> 4) & 0x0F
	alphabet := (dcs >> 2) & 0x03

	isUCS2 := false
	if codingGroup == 0x00 || codingGroup == 0x01 {
		// 一般編碼組
		isUCS2 = alphabet == 2 // bit3-2 = 10 -> UCS2
	} else if dcs == 72 {
		isUCS2 = true
	} else if dcs >= 0x40 && dcs <= 0x7F {
		// 訊息類編碼組
		isUCS2 = alphabet == 2
	}

	if isUCS2 && isHex {
		// UCS2: Hex 字串 -> UTF-16BE -> UTF-8
		b, err := hex.DecodeString(raw)
		if err != nil {
			logger.Debug(fmt.Sprintf("[%s] USSD UCS2 hex 解碼失敗", m.cfg.ID), "err", err, "raw", raw)
			return raw
		}
		if len(b)%2 != 0 {
			return raw
		}
		u16 := make([]uint16, len(b)/2)
		for i := 0; i < len(b); i += 2 {
			u16[i/2] = uint16(b[i])<<8 | uint16(b[i+1])
		}
		return string(utf16.Decode(u16))
	}

	if isHex {
		// 可能是 GSM 7-bit packed 的 hex 表示
		b, err := hex.DecodeString(raw)
		if err != nil {
			return raw
		}
		unpacked := gsm7.Unpack7BitUSSD(b, 0)
		decoded, err := gsm7.Decode(unpacked)
		if err != nil {
			return raw
		}
		return string(decoded)
	}

	// 非 Hex 字串，直接回傳原文（某些 Modem 已經做了解碼）
	return raw
}

// ExecuteUSSD 傳送 USSD 指令並等待網路回傳結果
// command: USSD 代碼，如 "*100#", "*135#"
// timeout: 等待 URC 回應的逾時時間
func (m *Manager) ExecuteUSSD(command string, timeout time.Duration) (*USSDResult, error) {
	// 清空可能殘留的舊結果
	select {
	case <-m.ussdChan:
	default:
	}

	logger.Info(fmt.Sprintf("[%s] 開始執行 USSD: %s", m.cfg.ID, command), "timeout", timeout.String())

	// 設定字元集，避免部分模組因使用非 GSM 的簡訊格式導致發不出去 USSD
	m.ExecuteATSilent(`AT+CSCS="GSM"`, 2*time.Second)

	// 傳送 AT+CUSD=1,"command",15
	cmd := fmt.Sprintf(`AT+CUSD=1,"%s",15`, command)
	_, err := m.ExecuteAT(cmd, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("傳送 USSD 指令失敗: %w", err)
	}

	logger.Debug(fmt.Sprintf("[%s] USSD 傳送成功，等待網路回包 URC (+CUSD)...", m.cfg.ID))

	// 阻塞等待 +CUSD URC 回呼
	select {
	case result := <-m.ussdChan:
		logger.Info(fmt.Sprintf("[%s] 收到 USSD 回傳", m.cfg.ID), "status", result.Status, "text", result.Text)
		return &result, nil
	case <-time.After(timeout):
		logger.Warn(fmt.Sprintf("[%s] USSD 回應網路逾時（無回呼），正在自動取消網路等待", m.cfg.ID), "timeout", timeout.String())
		// 逾時後取消 USSD 工作階段
		m.CancelUSSD()
		return nil, errors.New("USSD 回應網路逾時（無回呼）")
	case <-m.stop:
		return nil, errors.New("裝置已停止")
	}
}

// CancelUSSD 取消目前 USSD 工作階段
func (m *Manager) CancelUSSD() {
	_, err := m.ExecuteATSilent(`AT+CUSD=2`, 3*time.Second)
	if err != nil {
		logger.Debug(fmt.Sprintf("[%s] 取消 USSD 工作階段(AT+CUSD=2)失敗", m.cfg.ID), "err", err)
	} else {
		logger.Debug(fmt.Sprintf("[%s] 已傳送 USSD 取消指令 (AT+CUSD=2)", m.cfg.ID))
	}
}

// CheckAndEnableUAC 查詢並確保開啟 USB Audio Class (UAC) 介面
// 許多 Quectel 模組需要 AT+QCFG="USBCFG" 最後一位為 1 才能在系統列舉出音效卡
// 回傳 modified(bool) 表示是否發生了設定更改，如果發生了更改，必須重啟才能生效
func (m *Manager) CheckAndEnableUAC() (bool, error) {
	resp, err := m.ExecuteAT(`AT+QCFG="USBCFG"?`, 3*time.Second)
	if err != nil {
		return false, err
	}

	// 查詢 +QCFG: "usbcfg" 或 +QCFG: "USBCFG"
	idx := strings.Index(strings.ToLower(resp), `+qcfg: "usbcfg",`)
	if idx == -1 {
		return false, nil // 可能不支援該指令或格式不符
	}

	start := idx + 7 // Skip "+QCFG: " (7 chars)
	line := resp[start:]
	if end := strings.IndexAny(line, "\r\n"); end != -1 {
		line = line[:end]
	}
	line = strings.TrimSpace(line)

	// line 例: "usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0
	parts := strings.Split(line, ",")
	if len(parts) < 8 {
		return false, nil // 引數過少跳過
	}

	lastIdx := len(parts) - 1
	lastVal := strings.TrimSpace(parts[lastIdx])

	if lastVal == "0" {
		parts[lastIdx] = "1"
		newArgs := strings.Join(parts, ",")
		newCmd := fmt.Sprintf(`AT+QCFG=%s`, newArgs)
		logger.Info(fmt.Sprintf("[%s] 檢測到 UAC 介面未開啟，正在透過 %s 執行開啟", m.cfg.ID, newCmd))
		_, err := m.ExecuteAT(newCmd, 3*time.Second)
		if err != nil {
			return false, fmt.Errorf("動態開啟 UAC 失敗: %w", err)
		}
		return true, nil
	} else {
		logger.Debug(fmt.Sprintf("[%s] UAC 介面已處於開啟狀態 (%s)，無需重啟", m.cfg.ID, lastVal))
	}
	return false, nil
}

// EnableUSBAudio 開啟 USB Audio UAC模式 (AT+QPCMV=1,2)
// 注意：每次模組重啟此設定都會失效，需要在開機後初始化流程或業務需要前呼叫
func (m *Manager) EnableUSBAudio() error {
	// 查詢目前 QPCMV 狀態避免重複傳送
	enabled, _, err := m.QueryUSBAudioMode()
	if err == nil && enabled {
		logger.Debug(fmt.Sprintf("[%s] USB Audio 此時已經處於開啟狀態，無需重複下發指令", m.cfg.ID))
		return nil
	}

	_, err = m.ExecuteAT("AT+QPCMV=1,2", 2*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 開啟 USB Audio (QPCMV) 失敗", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] USB Audio (QPCMV) 已設定開啟", m.cfg.ID))
	return nil
}

// DisableUSBAudio 關閉 USB Audio 模式 (AT+QPCMV=0)
func (m *Manager) DisableUSBAudio() error {
	_, err := m.ExecuteAT("AT+QPCMV=0", 2*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 關閉 USB Audio 失敗", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] USB Audio 已設定關閉", m.cfg.ID))
	return nil
}

// QueryUSBAudioMode 查詢目前 USB Audio 狀態
func (m *Manager) QueryUSBAudioMode() (bool, int, error) {
	resp, err := m.ExecuteAT("AT+QPCMV?", 2*time.Second)
	if err != nil {
		return false, 0, err
	}
	idx := strings.Index(resp, "+QPCMV:")
	if idx == -1 {
		return false, 0, errors.New("查詢失敗: 回應未包含 +QPCMV")
	}
	parts := strings.Split(strings.TrimSpace(resp[idx+7:]), ",")
	enabled := strings.TrimSpace(parts[0]) == "1"
	mode := 0
	if len(parts) > 1 {
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &mode)
	}
	return enabled, mode, nil
}
