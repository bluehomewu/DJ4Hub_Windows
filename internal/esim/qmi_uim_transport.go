package esim

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/apduarbiter"
	"github.com/WongLoki/DJ4Hub/pkg/logger"
	qmiq "github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// QMIUIMTransport 提供獨立於 qmicore.Manager 的 QMI UIM APDU 傳輸實現。
// 僅負責 eUICC APDU，不負責網路撥號。
type QMIUIMTransport struct {
	controlDevice string
	clientOptions qmiq.ClientOptions

	mu     sync.RWMutex
	client *qmiq.Client
	uim    *qmiq.UIMService

	coord *apduCoordinator
}

func NewQMIUIMTransportWithOptions(controlDevice string, clientOptions qmiq.ClientOptions) *QMIUIMTransport {
	return &QMIUIMTransport{
		controlDevice: strings.TrimSpace(controlDevice),
		clientOptions: clientOptions,
		coord:         newAPDUCoordinator("QMI"),
	}
}

// getOrCreateChanMu 返回指定 channel 對應的互斥鎖（懶建立，執行緒安全）
func (t *QMIUIMTransport) getOrCreateChanMu(channel byte) *sync.Mutex {
	return t.coord.getOrCreateChanMu(channel)
}

func (t *QMIUIMTransport) ControlDevice() string {
	return strings.TrimSpace(t.controlDevice)
}

func (t *QMIUIMTransport) Start() error {
	controlDevice := t.ControlDevice()
	if controlDevice == "" {
		return ErrQMIControlDeviceMissing
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil && t.uim != nil {
		return nil
	}

	openCtx, openCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer openCancel()

	client, err := qmiq.NewClientWithOptions(openCtx, controlDevice, t.clientOptions)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrQMIUIMNotAvailable, err)
	}
	uim, err := qmiq.NewUIMService(client)
	if err != nil {
		_ = client.Close()
		return wrapQMIChannelError("initialize UIM service", err)
	}

	t.client = client
	t.uim = uim
	logger.Info("獨立 QMI UIM transport 啟動成功",
		"transport", transportQMI,
		"control_device", controlDevice)
	return nil
}

func (t *QMIUIMTransport) Stop() error {
	// 清理所有 per-channel 鎖，保證沒有飛行中的 Transmit
	t.releaseAllAPDULeases("stop")
	t.coord.resetChanMu()

	t.mu.Lock()
	uim := t.uim
	client := t.client
	t.uim = nil
	t.client = nil
	t.mu.Unlock()

	var stopErr error
	if uim != nil {
		if err := uim.Close(); err != nil {
			stopErr = err
		}
	}
	if client != nil {
		if err := client.Close(); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	if stopErr != nil {
		return fmt.Errorf("stop qmi uim transport: %w", stopErr)
	}
	return nil
}

func (t *QMIUIMTransport) OpenEUICCLogicalChannel(ctx context.Context, slot byte, aid []byte) (byte, error) {
	lease, err := t.acquireAPDUTransportLease(ctx, 10*time.Second, "esim_session_open", apduarbiter.APDUClassEUICCWrite, 0, apduarbiter.TransportScopeExclusive)
	if err != nil {
		return 0, err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}
	// Open 操作就用 getOrCreateChanMu(0) 也就是 channel 0 鎖來序列化
	// （Open 頻率遠低，通道 0 正常不會被用於應用層 APDU）
	openMu := t.getOrCreateChanMu(0)
	openMu.Lock()
	defer openMu.Unlock()

	uim, err := t.getUIM()
	if err != nil {
		return 0, err
	}

	channel, err := uim.OpenLogicalChannel(ctx, slot, aid)
	if err != nil {
		return 0, err
	}
	if lease != nil {
		lease.Touch()
	}
	t.bindAPDUSession(channel, "esim")
	return channel, nil
}

func (t *QMIUIMTransport) CloseEUICCLogicalChannel(ctx context.Context, slot byte, channel byte) error {
	t.takeAPDUSession(channel)
	lease, err := t.acquireAPDUTransportLease(ctx, 10*time.Second, "esim_session_close", apduarbiter.APDUClassEUICCWrite, channel, apduarbiter.TransportScopeExclusive)
	if err != nil {
		return err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}
	// Close 也用 channel 0 鎖序列化（與 Open 的鎖相同）
	closeMu := t.getOrCreateChanMu(0)
	closeMu.Lock()
	defer closeMu.Unlock()

	uim, err := t.getUIM()
	if err != nil {
		return err
	}

	err = uim.CloseLogicalChannel(ctx, slot, channel)
	if lease != nil {
		lease.Touch()
	}
	return err
}

func (t *QMIUIMTransport) TransmitEUICCAPDU(ctx context.Context, slot byte, channel byte, command []byte) ([]byte, error) {
	owner := "esim_apdu"
	class := apduarbiter.APDUClassEUICCWrite
	if channel == 0 {
		owner = "sim_aka"
		class = apduarbiter.APDUClassUSIMAKA
	} else if !t.hasAPDUSession(channel) {
		owner = "unbound_channel_apdu"
	}
	scope := apduarbiter.TransportScopeExclusive
	if channel > 0 {
		scope = apduarbiter.TransportScopeQMIChannel
	}
	lease, err := t.acquireAPDUTransportLease(ctx, 10*time.Second, owner, class, channel, scope)
	if err != nil {
		return nil, err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}

	// per-channel 互斥：同一通道內 APDU 順序執行，不同通道可併發
	chanMu := t.getOrCreateChanMu(channel)
	chanMu.Lock()
	defer chanMu.Unlock()

	uim, err := t.getUIM()
	if err != nil {
		return nil, err
	}

	// 使用上層傳入的 ctx（通常來自 DownloadProfile），不再建立固定 10 秒超時的內部 ctx。
	// 這樣 BPP 安裝期間 eUICC 加解密+NVRAM 寫入導致的長時回應就不會觸發超時錯誤。
	resp, err := uim.SendAPDU(ctx, slot, channel, command)
	if lease != nil {
		lease.Touch()
	}
	return resp, err
}

func (t *QMIUIMTransport) getUIM() (*qmiq.UIMService, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.uim == nil || t.client == nil {
		return nil, ErrQMIUIMNotAvailable
	}
	return t.uim, nil
}

func (t *QMIUIMTransport) SetAPDUArbiter(arbiter *apduarbiter.Arbiter) {
	t.coord.setArbiter(arbiter)
}

func (t *QMIUIMTransport) acquireAPDUTransportLease(ctx context.Context, timeout time.Duration, owner string, class apduarbiter.APDUClass, channel byte, scope apduarbiter.TransportScope) (*apduarbiter.Lease, error) {
	return t.coord.acquireLease(ctx, timeout, owner, class, channel, scope)
}

func (t *QMIUIMTransport) bindAPDUSession(channel byte, owner string) {
	t.coord.bindSession(channel, owner)
}

func (t *QMIUIMTransport) hasAPDUSession(channel byte) bool {
	return t.coord.hasSession(channel)
}

func (t *QMIUIMTransport) takeAPDUSession(channel byte) (apduSessionInfo, bool) {
	return t.coord.takeSession(channel)
}

func (t *QMIUIMTransport) releaseAllAPDULeases(reason string) {
	t.coord.releaseAllSessions(t.controlDevice, reason)
}

var _ QMIAPDUTransport = (*QMIUIMTransport)(nil)
var _ QMIAPDUTransportLifecycle = (*QMIUIMTransport)(nil)
