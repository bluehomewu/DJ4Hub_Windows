package logger

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SlogAdapter 是一個 slog.Handler 實現，用於將 slog 的日誌橋接到 zap logger。
type SlogAdapter struct {
	logger *zap.Logger
	// callerChain 承載 slog logger.With("caller", "...") 注入的呼叫鏈，
	// 避免同名 caller 欄位重複列印。
	callerChain []string
}

// NewSlogHandler 建立一個橋接 slog 到 zap 的處理器。
func NewSlogHandler(logger *zap.Logger) *SlogAdapter {
	if logger == nil {
		logger = ZapLogger() // Fallback 到全域性 Log
	}
	// 關閉 logger 側 caller 自動推斷，避免 caller skip 在不同協程棧下漂移到 runtime/asm。
	// 實際 caller 在 Handle 中透過 slog.Record.PC 精確寫入 zap Entry.Caller。
	return &SlogAdapter{
		logger:      logger.WithOptions(zap.WithCaller(false)),
		callerChain: nil,
	}
}

// Enabled 決定是否啟用某日誌級別
func (h *SlogAdapter) Enabled(_ context.Context, level slog.Level) bool {
	// 如果全域性開關或者配置沒有把 debug 開啟，這裡的 zap logger 會自動濾除
	// 為了確保所有的都丟給 zap 判斷，我們總是返回 true，或者可以根據 zap 的配置判斷
	return true
}

// Handle 處理單條 slog 日誌並寫入 zap
func (h *SlogAdapter) Handle(_ context.Context, r slog.Record) error {
	fields := make([]zap.Field, 0, r.NumAttrs())
	errText := ""
	src := callerFromPC(r.PC)
	callers := make([]string, 0, len(h.callerChain)+2)
	callers = append(callers, h.callerChain...)
	r.Attrs(func(a slog.Attr) bool {
		if strings.TrimSpace(a.Key) == "" {
			return true
		}
		if a.Key == "caller" {
			if v := strings.TrimSpace(fmt.Sprint(a.Value.Any())); v != "" {
				callers = append(callers, v)
			}
			return true
		}
		if a.Key == "error" {
			errText = strings.TrimSpace(fmt.Sprint(a.Value.Any()))
		}
		fields = append(fields, zap.Any(a.Key, a.Value.Any()))
		return true
	})

	if uniq := dedupeNonEmpty(callers); len(uniq) > 0 {
		fields = append(fields, zap.String("caller", uniq[len(uniq)-1]))
		if len(uniq) > 1 {
			fields = append(fields, zap.String("caller_chain", strings.Join(uniq, " -> ")))
		}
	}

	level := r.Level
	msg := r.Message
	if strings.EqualFold(strings.TrimSpace(msg), "Read error") {
		errLower := strings.ToLower(errText)
		if strings.Contains(errLower, "connection reset by peer") ||
			strings.Contains(errLower, "connection timed out") ||
			strings.Contains(errLower, "i/o timeout") ||
			strings.Contains(errLower, "use of closed network connection") ||
			strings.Contains(errLower, "broken pipe") ||
			strings.Contains(errLower, "eof") {
			msg = "SIP TCP 通道讀異常"
			// 連線被顯式關閉或切換通道導致 EOF/closed，降為 DEBUG；其他斷連降為 WARN。
			if strings.Contains(errLower, "use of closed network connection") || strings.Contains(errLower, "eof") {
				level = slog.LevelDebug
			} else {
				level = slog.LevelWarn
			}
		}
	}

	h.writeWithCaller(toZapLevel(level), r.Time, msg, fields, src)

	return nil
}

// WithAttrs 返回帶有預設屬性的 Handler
func (h *SlogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := make([]zap.Field, 0, len(attrs))
	callers := make([]string, 0, len(h.callerChain)+len(attrs))
	callers = append(callers, h.callerChain...)
	for _, a := range attrs {
		if strings.TrimSpace(a.Key) == "" {
			continue
		}
		if a.Key == "caller" {
			if v := strings.TrimSpace(fmt.Sprint(a.Value.Any())); v != "" {
				callers = append(callers, v)
			}
			continue
		}
		fields = append(fields, zap.Any(a.Key, a.Value.Any()))
	}
	return &SlogAdapter{
		logger:      h.logger.With(fields...),
		callerChain: callers,
	}
}

// WithGroup 返回帶名稱空間的 Handler (這裡簡略實現)
func (h *SlogAdapter) WithGroup(name string) slog.Handler {
	return &SlogAdapter{
		logger:      h.logger.Named(name),
		callerChain: append([]string(nil), h.callerChain...),
	}
}

func dedupeNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func callerFromPC(pc uintptr) zapcore.EntryCaller {
	if pc == 0 {
		return zapcore.EntryCaller{}
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" || frame.Line <= 0 {
		return zapcore.EntryCaller{}
	}
	return zapcore.EntryCaller{
		Defined:  true,
		PC:       frame.PC,
		File:     frame.File,
		Line:     frame.Line,
		Function: frame.Function,
	}
}

func toZapLevel(level slog.Level) zapcore.Level {
	switch {
	case level <= slog.LevelDebug:
		return zapcore.DebugLevel
	case level < slog.LevelWarn:
		return zapcore.InfoLevel
	case level < slog.LevelError:
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}

func (h *SlogAdapter) writeWithCaller(level zapcore.Level, ts time.Time, msg string, fields []zap.Field, caller zapcore.EntryCaller) {
	core := h.logger.Core()
	entry := zapcore.Entry{
		Level:   level,
		Time:    ts,
		Message: msg,
		Caller:  caller,
	}
	if ce := core.Check(entry, nil); ce != nil {
		ce.Write(fields...)
	}
}
