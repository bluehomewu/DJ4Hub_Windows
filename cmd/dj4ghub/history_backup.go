package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type historyBackupConfig struct {
	Directory   string    `json:"directory"`
	Enabled     bool      `json:"enabled"`
	Owner       string    `json:"owner"`
	LastTime    time.Time `json:"last_time"`
	LastFile    string    `json:"last_file"`
	Fingerprint string    `json:"fingerprint"`
}

func (h *communicationHistory) backupConfigPath() string {
	return filepath.Join(filepath.Dir(h.path), "history-backup.json")
}

func (h *communicationHistory) loadBackupConfig() {
	if h.path == "" {
		return
	}
	data, err := os.ReadFile(h.backupConfigPath())
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		h.backupError = err.Error()
		return
	}
	if err := json.Unmarshal(data, &h.backup); err != nil {
		h.backup = historyBackupConfig{}
		h.backupError = "備份設定讀取失敗，請重新選擇目錄"
	}
}

func (h *communicationHistory) saveBackupConfig(config historyBackupConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(h.path), ".backup-config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), h.backupConfigPath()); err != nil {
		return err
	}
	h.backup = config
	return nil
}

// VACUUM INTO creates a consistent standalone snapshot including committed WAL data.
// Build locally first; publish only the completed snapshot to the cloud directory.
func (h *communicationHistory) snapshot(directory string, prefix string) (string, error) {
	if h.db == nil {
		return "", fmt.Errorf("記錄資料庫不可用")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("備份目錄不可用，請確認雲端硬碟目錄已下載到本機")
	}
	stage, err := os.MkdirTemp(filepath.Dir(h.path), ".history-snapshot-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	source := filepath.Join(stage, "snapshot.sqlite")
	if err := h.db.Exec("VACUUM INTO ?", source).Error; err != nil {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	output, err := os.CreateTemp(directory, "."+prefix+"-*.partial")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(output.Name()) }()
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return "", err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return "", err
	}
	if err := output.Close(); err != nil {
		return "", err
	}
	name := prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".sqlite"
	target := filepath.Join(directory, name)
	if err := os.Rename(output.Name(), target); err != nil {
		return "", err
	}
	return target, nil
}

func (h *communicationHistory) backupNow() (string, error) {
	config := h.backup
	if config.Directory == "" {
		return "", fmt.Errorf("請先選擇備份目錄")
	}
	if config.Owner == "" {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", err
		}
		config.Owner = hex.EncodeToString(token[:])
	}
	prefix := "dj4hub-" + config.Owner
	path, err := h.snapshot(config.Directory, prefix)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(h.records)
	if err != nil {
		return "", err
	}
	config.LastTime = time.Now()
	config.LastFile = path
	config.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(data))
	if err := h.saveBackupConfig(config); err != nil {
		return "", err
	}
	// Only rotate files belonging to this installation, never arbitrary cloud files.
	entries, err := os.ReadDir(config.Directory)
	if err != nil {
		return path, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), prefix+"-") && strings.HasSuffix(entry.Name(), ".sqlite") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > 10 {
		if err := os.Remove(filepath.Join(config.Directory, names[0])); err != nil {
			return path, err
		}
		names = names[1:]
	}
	return path, nil
}

// Read an explicitly selected file without creating, migrating or writing to it.
// sqliteReadOnlyURI builds file:///C:/dir/name.sqlite?mode=ro. SQLite URIs
// need forward slashes and a leading slash before a Windows drive letter.
func sqliteReadOnlyURI(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	u := url.URL{Scheme: "file", Path: slashed, RawQuery: "mode=ro"}
	return u.String()
}

func readHistoryBackup(path string) ([]historyRecord, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return nil, fmt.Errorf("請選擇小於 256 MB 的 SQLite 備份檔案")
	}
	db, err := gorm.Open(sqlite.Open(sqliteReadOnlyURI(path)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	conn, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	var integrity string
	if err := db.Raw("PRAGMA quick_check").Scan(&integrity).Error; err != nil || integrity != "ok" {
		return nil, fmt.Errorf("備份完整性檢查失敗")
	}
	var rows []communicationRecord
	if err := db.Limit(100001).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("不是有效的 DJ 4G Hub 通訊記錄備份")
	}
	if len(rows) > 100000 {
		return nil, fmt.Errorf("備份記錄過多")
	}
	records := make([]historyRecord, 0, len(rows))
	for _, row := range rows {
		var record historyRecord
		if err := json.Unmarshal([]byte(row.Payload), &record); err != nil {
			return nil, err
		}
		if record.ID == "" || record.ID != row.RecordID || record.Kind != row.Kind || record.ICCID != row.ICCID || (record.Kind != "call" && record.Kind != "sms") || record.Started.IsZero() {
			return nil, fmt.Errorf("備份記錄格式不符合要求")
		}
		records = append(records, record)
	}
	if _, err := encodeHistory(records); err != nil {
		return nil, err
	}
	return records, nil
}

func (h *communicationHistory) restoreBackup(path string) (int, string, error) {
	if len(h.active) != 0 {
		return 0, "", fmt.Errorf("通話結束後才能恢復記錄")
	}
	records, err := readHistoryBackup(path)
	if err != nil {
		return 0, "", err
	}
	safetyDir := filepath.Join(filepath.Dir(h.path), "Restore Backups")
	if err := os.MkdirAll(safetyDir, 0700); err != nil {
		return 0, "", err
	}
	safety, err := h.snapshot(safetyDir, "before-restore")
	if err != nil {
		return 0, "", err
	}
	seen := make(map[string]bool, len(h.records))
	for _, record := range h.records {
		seen[record.ID] = true
	}
	merged := append([]historyRecord(nil), h.records...)
	count := 0
	for _, record := range records {
		if seen[record.ID] {
			continue
		}
		seen[record.ID] = true
		if record.Kind == "call" && record.Ended == nil {
			record.State = "interrupted"
		}
		merged = append(merged, record)
		count++
	}
	previous := h.records
	h.records = merged
	if err := h.save(); err != nil {
		h.records = previous
		return 0, safety, err
	}
	return count, safety, nil
}

func (a *app) monitorHistoryBackup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h := a.historyStore()
			if h == nil || a.demo {
				continue
			}
			h.mu.Lock()
			h.backupIfDue(time.Now())
			h.mu.Unlock()
		}
	}
}

// Caller holds h.mu, so the fingerprint and snapshot cover the same records.
func (h *communicationHistory) backupIfDue(now time.Time) {
	if !h.backup.Enabled || now.Sub(h.backup.LastTime) < 30*time.Minute {
		return
	}
	data, err := json.Marshal(h.records)
	if err == nil && fmt.Sprintf("%x", sha256.Sum256(data)) != h.backup.Fingerprint {
		_, err = h.backupNow()
	}
	if err != nil {
		h.backupError = err.Error()
	} else {
		h.backupError = ""
	}
}

func (a *app) historyBackupStatus(w http.ResponseWriter, r *http.Request) {
	h := a.historyStore()
	if h == nil {
		writeError(w, 503, "記錄儲存未初始化")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	writeJSON(w, 200, map[string]any{"directory": h.backup.Directory, "enabled": h.backup.Enabled, "last_time": h.backup.LastTime, "last_file": h.backup.LastFile, "error": h.backupError, "database": h.path})
}

func (a *app) historyBackupAction(w http.ResponseWriter, r *http.Request) {
	// Native requests have no Origin. Cross-origin web pages cannot choose file paths.
	if r.Header.Get("X-DJ4Hub-Audio") != "1" || r.Header.Get("Origin") != "" {
		writeError(w, 403, "請從 macOS 用戶端的備份設定操作")
		return
	}
	var body struct {
		Action    string `json:"action" validate:"required,oneof=configure backup restore"`
		Path      string `json:"path"`
		Enabled   bool   `json:"enabled"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writeError(w, 400, "無效請求")
		return
	}
	h := a.historyStore()
	if h == nil || a.demo {
		writeError(w, 503, "真實記錄儲存不可用")
		return
	}
	// Serialize restore with dialing and call-history observation.
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	var err error
	message := ""
	switch body.Action {
	case "configure":
		info, statErr := os.Stat(body.Path)
		if !filepath.IsAbs(body.Path) || statErr != nil || !info.IsDir() {
			writeError(w, 400, "請選擇本機可用的備份資料夾")
			return
		}
		config := h.backup
		if config.Directory != body.Path {
			config.LastTime = time.Time{}
			config.Fingerprint = ""
			config.LastFile = ""
		}
		config.Directory = body.Path
		config.Enabled = body.Enabled
		err = h.saveBackupConfig(config)
		message = "備份設定已儲存"
	case "backup":
		_, err = h.backupNow()
		message = "完整備份已寫入所選目錄；雲端硬碟上傳進度請在 Finder 檢視"
	case "restore":
		if !body.Confirmed || !filepath.IsAbs(body.Path) {
			writeError(w, 400, "恢復前需要確認")
			return
		}
		var count int
		var safety string
		count, safety, err = h.restoreBackup(body.Path)
		message = fmt.Sprintf("已補回 %d 筆記錄；原資料安全備份：%s", count, safety)
	default:
		writeError(w, 400, "不支援的備份操作")
		return
	}
	if err != nil {
		h.backupError = err.Error()
		writeError(w, 500, err.Error())
		return
	}
	h.backupError = ""
	writeJSON(w, 200, map[string]any{"message": message})
}
