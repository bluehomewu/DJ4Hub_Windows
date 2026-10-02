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

// forceReleasePort 检查端口是否被占用，如果是则杀掉占用者
func (m *Manager) forceReleasePort(portPath string) {
	// 设备文件不存在时 fuser 可能返回内核线程 PID，直接跳过避免误杀。
	if _, err := os.Stat(portPath); err != nil {
		return
	}

	// 先查询占用进程，再排除当前进程(及其线程)后定向释放，避免误杀自身。
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
		// 跳过内核关键进程: PID 1 (init/systemd), PID 2 (kthreadd)
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
		logger.Warn(fmt.Sprintf("[%s] 端口被当前进程占用，跳过自杀式释放", m.cfg.ID), "port", portPath, "self_pids", skipped)
	}
	if len(released) > 0 {
		logger.Warn(fmt.Sprintf("[%s] 检测到端口被外部进程占用，正在强制释放", m.cfg.ID), "port", portPath, "pids", released)
		// 等待进程完全退出
		time.Sleep(200 * time.Millisecond)
	}
}

func parseFuserPIDs(raw string) []int {
	// fuser 输出通常形如: "/dev/ttyUSB2: 1234 5678"
	// 只解析冒号后的 PID，避免把设备名中的数字(如 ttyUSB2)误当作 PID。
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

// SetSMSCallback 设置短信接收回调
