package esim

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/WongLoki/DJ4Hub/pkg/logger"
	"github.com/WongLoki/DJ4Hub/pkg/mbim"
	qmiq "github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

var (
	ErrQMITransportNotAvailable = errors.New("qmi_transport_not_available")
	ErrQMIUIMNotAvailable       = errors.New("qmi_uim_not_available")
	ErrQMIUIMNotSupported       = errors.New("qmi_uim_not_supported")
	ErrQMIControlDeviceMissing  = errors.New("qmi_control_device_missing")
	// ErrQMIUIMCardReset 表示卡片在執行 APDU（通常是攜帶 refresh=true 的 EnableProfile）時觸發了內部 UICC RESET。
	// 模組會回傳 QMI_ERR_CARD_CALL_CONTROL_REF_FAILED (0x0030)，這是正常預期行為，不代表切卡失敗。
	ErrQMIUIMCardReset = errors.New("qmi_uim_card_reset")
	// ErrMBIMUICCInvalidChannel 是 ErrQMIUIMCardReset 的 MBIM 對應物：MBIM 模組在同樣的場景下
	// 回傳 Microsoft UICC Low Level Access 定義的 MBIM_STATUS_ERROR_MS_INVALID_LOGICAL_CHANNEL /
	// MS_SELECT_FAILED / MS_NO_LOGICAL_CHANNELS（0x8743000x），代表 eUICC 內部 RESET 使邏輯通道失效，
	// 同樣是預期訊號，不代表切卡失敗。
	ErrMBIMUICCInvalidChannel             = errors.New("mbim_uicc_invalid_channel")
	ErrQMITransportRequiresNetworkManager = ErrQMITransportNotAvailable
	ErrQMIESIMRequiresNetworkManager      = ErrQMITransportRequiresNetworkManager // Deprecated: kept for backward compatibility.
)

const qmiAPDUSuccessLogThreshold = 500 * time.Millisecond

func shouldLogQMIAPDUSuccess(elapsed time.Duration) bool {
	return elapsed >= qmiAPDUSuccessLogThreshold
}

type QMIAPDUTransport interface {
	ControlDevice() string
	OpenEUICCLogicalChannel(ctx context.Context, slot byte, aid []byte) (byte, error)
	CloseEUICCLogicalChannel(ctx context.Context, slot byte, channel byte) error
	TransmitEUICCAPDU(ctx context.Context, slot byte, channel byte, command []byte) ([]byte, error)
}

type QMIAPDUTransportLifecycle interface {
	QMIAPDUTransport
	Start() error
	Stop() error
}

type QMIChannel struct {
	transport QMIAPDUTransport
	slot      byte
	channel   byte
	opened    bool
	mu        sync.Mutex
	// activeCtx 是目前操作的 context（由 DownloadProfile 注入）。
	// 普通操作未注入時為 nil，使用 context.Background() 作為後備。
	activeCtx atomic.Pointer[context.Context]
}

func NewQMIChannel(transport QMIAPDUTransport, slot byte) *QMIChannel {
	if slot == 0 {
		slot = 1
	}
	return &QMIChannel{transport: transport, slot: slot}
}

func (c *QMIChannel) CurrentChannel() byte {
	return c.channel
}

// SetContext 注入 APDU 操作的 context，由 DownloadProfile 在 LPA client 建立後呼叫。
// 注入後所有 Transmit / OpenLogicalChannel 呼叫均使用該 ctx。
func (c *QMIChannel) SetContext(ctx context.Context) {
	c.activeCtx.Store(&ctx)
}

// getActiveCtx 回傳目前有效的 context。未注入時回傳 context.Background()。
func (c *QMIChannel) getActiveCtx() context.Context {
	if p := c.activeCtx.Load(); p != nil {
		return *p
	}
	return context.Background()
}

func (c *QMIChannel) Connect() error {
	if c.transport == nil {
		return ErrQMITransportNotAvailable
	}
	if strings.TrimSpace(c.transport.ControlDevice()) == "" {
		return ErrQMIControlDeviceMissing
	}
	return nil
}

func (c *QMIChannel) Disconnect() error {
	return nil
}

func (c *QMIChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	channel, err := c.transport.OpenEUICCLogicalChannel(c.getActiveCtx(), c.slot, aid)
	if err != nil {
		return 0, wrapQMIChannelError("open logical channel", err)
	}
	c.channel = channel
	c.opened = true
	logger.RunDebug("QMI logical channel 開啟成功",
		"transport", transportQMI,
		"control_device", c.transport.ControlDevice(),
		"aid", strings.ToUpper(hex.EncodeToString(aid)),
		"channel", channel)
	return channel, nil
}

func (c *QMIChannel) Transmit(command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.opened || c.channel == 0 {
		return nil, errors.New("QMI logical channel is not open")
	}
	started := time.Now()
	resp, err := c.transport.TransmitEUICCAPDU(c.getActiveCtx(), c.slot, c.channel, command)
	if err != nil {
		return nil, wrapQMIChannelError("transmit APDU", err)
	}
	if elapsed := time.Since(started); shouldLogQMIAPDUSuccess(elapsed) {
		logger.RunDebug("QMI APDU 直通傳送成功",
			"transport", transportQMI,
			"control_device", c.transport.ControlDevice(),
			"channel", c.channel,
			"elapsed_ms", elapsed.Milliseconds(),
			"response_len", len(resp))
	}
	return resp, nil
}

func (c *QMIChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 關閉 channel 必須完成，使用獨立的 Background context（防止被 DownloadProfile ctx 取消）。
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.transport.CloseEUICCLogicalChannel(closeCtx, c.slot, channel); err != nil {
		return wrapQMIChannelError("close logical channel", err)
	}
	logger.RunDebug("QMI logical channel 已關閉",
		"transport", transportQMI,
		"control_device", c.transport.ControlDevice(),
		"channel", channel)
	c.channel = 0
	c.opened = false
	return nil
}

func wrapQMIChannelError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrQMIControlDeviceMissing) || errors.Is(err, ErrQMITransportNotAvailable) {
		return err
	}
	if qe := qmiq.GetQMIError(err); qe != nil {
		switch qe.ErrorCode {
		case qmiq.QMIErrNotSupported, qmiq.QMIErrInvalidQmiCmd:
			return fmt.Errorf("%w: %s", ErrQMIUIMNotSupported, operation)
		case qmiq.QMIErrDeviceNotReady, qmiq.QMIErrInvalidID:
			return fmt.Errorf("%w: %s", ErrQMIUIMNotAvailable, operation)
		case qmiq.QMIErrCardCallControlRefFail:
			// 卡片執行 EnableProfile+refresh 後觸發內部 UICC RESET，模組回傳 0x0030 屬於正常行為。
			// 包裝為 ErrQMIUIMCardReset，讓上層用 errors.Is 精確識別，而不依賴字串匹配。
			return fmt.Errorf("%w: %s", ErrQMIUIMCardReset, operation)
		}
	}
	if _, ok := err.(*qmiq.NotSupportedError); ok {
		return fmt.Errorf("%w: %s", ErrQMIUIMNotSupported, operation)
	}
	var mbimStatusErr *mbim.StatusError
	if errors.As(err, &mbimStatusErr) {
		switch mbimStatusErr.Status {
		case mbim.StatusMSInvalidLogicalChannel, mbim.StatusMSSelectFailed, mbim.StatusMSNoLogicalChannels:
			return fmt.Errorf("%w: %s", ErrMBIMUICCInvalidChannel, operation)
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "uim service not available") ||
		strings.Contains(strings.ToLower(err.Error()), "qmi_uim_not_available") {
		return fmt.Errorf("%w: %s", ErrQMIUIMNotAvailable, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
