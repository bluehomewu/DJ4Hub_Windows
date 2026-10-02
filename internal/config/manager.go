package config

import (
	"sync"

	"github.com/WongLoki/DJ4Hub/pkg/logger"
)

var (
	globalConfig *Config
	configMu     sync.RWMutex
	configPath   string
)

// InitGlobalManager 初始化全域性設定管理器，將首次從檔案載入到記憶體
func InitGlobalManager(path string) error {
	configPath = path
	return ReloadFromFile()
}

// ReloadFromFile 從磁碟重新載入最新設定到記憶體，通常在任何更新設定的動作後主動呼叫
func ReloadFromFile() error {
	if configPath == "" {
		return nil
	}
	cfg, err := Load(configPath)
	if err != nil {
		return err
	}
	configMu.Lock()
	globalConfig = cfg
	configMu.Unlock()
	logger.Info("設定檔已從磁碟熱載入到記憶體", "path", configPath)
	return nil
}

// GetConfig 取得目前處於記憶體中的全域性設定。為保障一致性，不可在外部直接修改回傳值。
func GetConfig() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	if globalConfig == nil {
		return &Config{}
	}
	return globalConfig
}

// GetConfigPath 回傳目前全域性設定檔路徑，供需要直接讀寫設定檔的場景使用
// （如裝置恢復後回寫發現到的實體路徑）。
func GetConfigPath() string {
	configMu.RLock()
	defer configMu.RUnlock()
	return configPath
}

// ListDevices 快捷取得記憶體中的裝置清單，替代原 ListDevicesFromFile 造成的高頻 IO
func ListDevices() []DeviceConfig {
	return GetConfig().Devices
}

// GetDeviceByID 快捷取得記憶體中指定 ID 的裝置
func GetDeviceByID(id string) (*DeviceConfig, error) {
	devices := ListDevices()
	for i := range devices {
		if devices[i].ID == id {
			return &devices[i], nil
		}
	}
	return nil, nil // not found
}
