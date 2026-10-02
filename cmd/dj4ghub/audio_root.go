package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Only requests adbd's supported root mode; never unlocks or flashes a device.
func ensureAudioRoot(ctx context.Context, uid func() (string, error), request func() error) error {
	value, err := uid()
	if err != nil {
		return fmt.Errorf("無法檢查 ADB 權限，請確認裝置已授權：%w", err)
	}
	if strings.TrimSpace(value) == "0" {
		return nil
	}
	if err := request(); err != nil {
		return fmt.Errorf("自動 adb root 失敗：%w", err)
	}
	for i := 0; i < 12; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		value, err = uid()
		if err == nil && strings.TrimSpace(value) == "0" {
			return nil
		}
	}
	return fmt.Errorf("adb root 後未確認同一裝置的 root 權限；請檢查韌體支援，未上傳驅動")
}
