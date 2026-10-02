//go:build !windows

package modem

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WongLoki/DJ4Hub/pkg/logger"
)

// forceReleasePort 檢查埠是否被佔用，如果是則殺掉佔用者
func (m *Manager) forceReleasePort(portPath string) {
	// 裝置檔案不存在時 fuser 可能回傳核心執行緒 PID，直接跳過避免誤殺。
	if _, err := os.Stat(portPath); err != nil {
		return
	}

	// 先查詢佔用行程，再排除目前行程(及其執行緒)後定向釋放，避免誤殺自身。
	out, _ := exec.Command("fuser", portPath).CombinedOutput()
	if len(out) == 0 {
		return
	}

	occupiedPIDs := parseFuserPIDs(string(out))
	if len(occupiedPIDs) == 0 {
		return
	}

	selfTaskPIDs := currentProcessTaskPIDSet()
	released := make([]int, 0, len(occupiedPIDs))
	skipped := make([]int, 0, len(occupiedPIDs))

	for _, pid := range occupiedPIDs {
		// 跳過核心關鍵行程: PID 1 (init/systemd), PID 2 (kthreadd)
		if pid <= 2 {
			skipped = append(skipped, pid)
			continue
		}
		if _, isSelf := selfTaskPIDs[pid]; isSelf {
			skipped = append(skipped, pid)
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			released = append(released, pid)
		}
	}

	if len(skipped) > 0 {
		logger.Warn(fmt.Sprintf("[%s] 埠被目前行程佔用，跳過自殺式釋放", m.cfg.ID), "port", portPath, "self_pids", skipped)
	}
	if len(released) > 0 {
		logger.Warn(fmt.Sprintf("[%s] 檢測到埠被外部行程佔用，正在強制釋放", m.cfg.ID), "port", portPath, "pids", released)
		// 等待行程完全結束
		time.Sleep(200 * time.Millisecond)
	}
}

func parseFuserPIDs(raw string) []int {
	// fuser 輸出通常形如: "/dev/ttyUSB2: 1234 5678"
	// 只解析冒號後的 PID，避免把裝置名中的數字(如 ttyUSB2)誤當作 PID。
	if idx := strings.Index(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		raw = raw[idx+1:]
	}
	seen := make(map[int]struct{})
	out := make([]int, 0)
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r < '0' || r > '9'
	})
	for _, f := range fields {
		pid, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	return out
}

func currentProcessTaskPIDSet() map[int]struct{} {
	out := map[int]struct{}{
		os.Getpid(): {},
	}
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(strings.TrimSpace(e.Name()))
		if err != nil || pid <= 0 {
			continue
		}
		out[pid] = struct{}{}
	}
	return out
}

// SetSMSCallback 設定簡訊接收回呼
