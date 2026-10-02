package logger

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

var (
	goRunOnce sync.Once
	goRunMode bool
)

// IsGoRun 回傳目前行程是否大機率由 `go run` 啟動。
// 也支援透過 DJ4GHUB_FORCE_GO_RUN_LOG=true/false 手動覆蓋判定結果。
func IsGoRun() bool {
	goRunOnce.Do(func() {
		goRunMode = detectGoRunMode()
	})
	return goRunMode
}

func detectGoRunMode() bool {
	raw := strings.TrimSpace(os.Getenv("DJ4GHUB_FORCE_GO_RUN_LOG"))
	if raw == "" {
		// Keep the upstream variable as a compatibility fallback for existing setups.
		raw = strings.TrimSpace(os.Getenv("VOHIVE_FORCE_GO_RUN_LOG"))
	}
	if raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}

	if exe, err := os.Executable(); err == nil {
		path := filepath.ToSlash(strings.ToLower(strings.TrimSpace(exe)))
		// `go run` 的臨時二進位通常位於 .../go-build... 路徑下。
		if strings.Contains(path, "/go-build") {
			return true
		}
	}

	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		// `go run` 預設以 command-line-arguments 作為主模組路徑。
		if strings.TrimSpace(bi.Path) == "command-line-arguments" {
			return true
		}
	}

	return false
}
