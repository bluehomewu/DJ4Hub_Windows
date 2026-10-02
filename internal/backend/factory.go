package backend

import (
	"fmt"
	"strings"

	"github.com/WongLoki/DJ4Hub/internal/modem"
	"github.com/WongLoki/DJ4Hub/pkg/logger"
)

// 後端模式常數
const (
	BackendAT   = "at"
	BackendQMI  = "qmi"
	BackendMBIM = "mbim"
)

// NormalizeBackendMode 標準化後端模式字串
func NormalizeBackendMode(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", BackendAT:
		return BackendAT // 預設 AT 模式
	case BackendQMI:
		return BackendQMI
	case BackendMBIM:
		return BackendMBIM
	default:
		return BackendAT
	}
}

// ValidateBackendMode 驗證後端模式是否有效
func ValidateBackendMode(in string) error {
	switch NormalizeBackendMode(in) {
	case BackendAT, BackendQMI, BackendMBIM:
		return nil
	default:
		return fmt.Errorf("無效的 device_backend 值: %q (可選: at, qmi, mbim)", in)
	}
}

// NewBackend 根據設定模式建立對應後端實例的工廠方法
// mode: "at" | "qmi"
// controlPath: QMI 控制裝置路徑（qmi 模式必須）
// m: modem.Manager（at 模式必須）
// source: QMI Core 資源源（qmi 模式必須）
func NewBackend(mode, controlPath string, m *modem.Manager, source QMISource, mbimSource MBIMSource) (DeviceBackend, error) {
	mode = NormalizeBackendMode(mode)

	switch mode {
	case BackendAT:
		if m == nil {
			return nil, fmt.Errorf("AT 模式需要 modem.Manager")
		}
		logger.Info("[backend] 使用 AT 後端模式")
		return NewATBackend(m), nil

	case BackendQMI:
		b, err := NewQMIBackend(controlPath, source)
		if err != nil {
			return nil, fmt.Errorf("QMI 後端初始化失敗: %w", err)
		}
		logger.Info("[backend] 使用 QMI 後端模式", "control_path", controlPath)
		return b, nil

	case BackendMBIM:
		if mbimSource == nil {
			return nil, fmt.Errorf("MBIM 模式需要 MBIMSource")
		}
		logger.Info("[backend] 使用 MBIM 後端模式", "control_path", controlPath)
		return NewMBIMBackend(controlPath, mbimSource), nil

	default:
		return nil, fmt.Errorf("不支援的後端模式: %s", mode)
	}
}
