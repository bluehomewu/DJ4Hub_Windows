package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	audioUSBPattern       = regexp.MustCompile(`(?im)^\+QCFG: "usbcfg",(0x[0-9a-f]+),(0x[0-9a-f]+),([01]),([01]),([01]),([01]),([01]),([01]),([01])\s*$`)
	audioIMEIPattern      = regexp.MustCompile(`(?m)^\s*([0-9]{15})\s*$`)
	audioChallengePattern = regexp.MustCompile(`(?m)^\+QADBKEY: ([0-9]{8})\s*$`)
)

// Legacy MD5-crypt protocol, not password storage. Never log input or result.
func legacyADBPassword(challenge string) (string, error) {
	if !regexp.MustCompile(`^[0-9]{8}$`).MatchString(challenge) {
		return "", errors.New("不支援的 ADB 授權協定；未修改裝置")
	}
	pw := []byte("SH_adb_quectel")
	salt := []byte(challenge)
	h := md5.New()
	h.Write(pw)
	h.Write([]byte("$1$"))
	h.Write(salt)
	b := md5.Sum(append(append(append([]byte{}, pw...), salt...), pw...))
	h.Write(b[:len(pw)])
	for n := len(pw); n > 0; n >>= 1 {
		if n&1 != 0 {
			h.Write([]byte{0})
		} else {
			h.Write(pw[:1])
		}
	}
	digest := h.Sum(nil)
	for i := 0; i < 1000; i++ {
		h.Reset()
		if i&1 != 0 {
			h.Write(pw)
		} else {
			h.Write(digest)
		}
		if i%3 != 0 {
			h.Write(salt)
		}
		if i%7 != 0 {
			h.Write(pw)
		}
		if i&1 != 0 {
			h.Write(digest)
		} else {
			h.Write(pw)
		}
		digest = h.Sum(nil)
	}
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	encoded := ""
	for _, triplet := range [][3]int{{0, 6, 12}, {1, 7, 13}, {2, 8, 14}, {3, 9, 15}, {4, 10, 5}} {
		n := uint(digest[triplet[0]])<<16 | uint(digest[triplet[1]])<<8 | uint(digest[triplet[2]])
		for j := 0; j < 4; j++ {
			encoded += string(alphabet[n&63])
			n >>= 6
		}
	}
	n := uint(digest[11])
	encoded += string(alphabet[n&63]) + string(alphabet[(n>>6)&63])
	return encoded[:15], nil
}

func parseAudioUSB(raw string) ([]string, error) {
	m := audioUSBPattern.FindStringSubmatch(raw)
	if len(m) != 10 {
		return nil, errors.New("無法嚴格解析 USB 設定；未修改裝置")
	}
	vid, _ := strconv.ParseUint(m[1][2:], 16, 16)
	pid, _ := strconv.ParseUint(m[2][2:], 16, 16)
	if vid != 0x2ca3 || pid != 0x4006 || m[5] != "1" {
		return nil, errors.New("USB 身分或 AT 介面不符合初始化條件")
	}
	return m[1:], nil
}

func audioIdentity(at func(string) (string, error)) (string, error) {
	raw, err := at("AT+GSN")
	if err != nil {
		return "", err
	}
	m := audioIMEIPattern.FindStringSubmatch(raw)
	if len(m) != 2 {
		return "", errors.New("無法讀取裝置 IMEI，禁止自動初始化")
	}
	return m[1], nil
}

// Initializes only a confirmed legacy QDC507. Caller holds audioMu against dialing.
// ADB authorization persists; configuration backups cannot revoke that authorization.
func initializeAudioADB(ctx context.Context, at func(string) (string, error), backupDir string, expected string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	raw, err := at(`AT+QCFG="usbcfg"`)
	if err != nil {
		return false, err
	}
	config, err := parseAudioUSB(raw)
	if err != nil {
		return false, err
	}
	if config[7] == "1" {
		return false, nil
	}
	id, err := audioIdentity(at)
	if err != nil || id != expected {
		return false, errors.New("裝置身分變化，已停止初始化")
	}
	firmware, err := at("AT+CVERSION")
	if err != nil || !strings.Contains(firmware, "VERSION: QDC507GLEFM21\r") && !strings.Contains(firmware, "VERSION: QDC507GLEFM21\n") {
		return false, errors.New("未驗證的韌體，禁止自動初始化")
	}
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return false, err
	}
	backup := struct {
		Schema   int       `json:"schema"`
		IMEI     string    `json:"imei"`
		Firmware string    `json:"firmware"`
		USB      []string  `json:"usbcfg"`
		Created  time.Time `json:"created_at"`
		Kind     string    `json:"kind"`
	}{1, id, firmware, config, time.Now(), "configuration-only-not-firmware"}
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(backupDir, id+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return false, err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return false, err
	}
	if err = authorizeAudioADB(at, expected); err != nil {
		return false, err
	}
	// Read again before writing to avoid overwriting configuration changed by another client.
	check, err := at(`AT+QCFG="usbcfg"`)
	if err != nil {
		return false, err
	}
	current, err := parseAudioUSB(check)
	if err != nil || strings.Join(current, ",") != strings.Join(config, ",") {
		return false, errors.New("USB 設定發生變化；未覆蓋")
	}
	config[7] = "1"
	if _, err = at(`AT+QCFG="usbcfg",` + strings.Join(config, ",")); err != nil {
		return false, errors.New("啟用 ADB 介面失敗；設定備份已保留")
	}
	if err = audioInitializationGuard(at, expected); err != nil {
		return false, err
	}
	// Reboot can drop its own acknowledgement; verify by later identity/config readback.
	_, _ = at("AT+CFUN=1,1")
	for i := 0; i < 25; i++ {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(time.Second):
		}
		got, e := audioIdentity(at)
		if e != nil {
			continue
		}
		if got != expected {
			return true, errors.New("重新連線的是另一台裝置，已停止")
		}
		check, e = at(`AT+QCFG="usbcfg"`)
		if e != nil {
			continue
		}
		actual, e := parseAudioUSB(check)
		if e == nil && strings.Join(actual, ",") == strings.Join(config, ",") {
			return true, nil
		}
	}
	return true, errors.New("ADB 設定已提交，等待重新連線逾時；未重複重啟，請在裝置恢復後重試")
}

func audioInitializationGuard(at func(string) (string, error), expected string) error {
	id, err := audioIdentity(at)
	if err != nil || id != expected {
		return errors.New("裝置身分變化，已停止")
	}
	raw, err := at("AT+CLCC")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+CLCC:") {
			fields := strings.Split(line, ",")
			if len(fields) < 5 || strings.TrimSpace(fields[3]) != "1" {
				return errors.New("目前有語音通話或未知通話狀態，不能初始化")
			}
		}
	}
	return nil
}

func authorizeAudioADB(at func(string) (string, error), expected string) error {
	if err := audioInitializationGuard(at, expected); err != nil {
		return err
	}
	raw, err := at("AT+QADBKEY?")
	if err != nil {
		return errors.New("無法查詢舊式 ADB 授權；未嘗試其他協定")
	}
	m := audioChallengePattern.FindStringSubmatch(raw)
	if len(m) != 2 {
		return errors.New("不是已驗證的舊式 ADB 挑戰格式")
	}
	password, err := legacyADBPassword(m[1])
	if err != nil {
		return err
	}
	if _, err = at(fmt.Sprintf(`AT+QADBKEY="%s"`, password)); err != nil {
		return errors.New("裝置拒絕 ADB 授權（授權資訊不記錄）")
	}
	return audioInitializationGuard(at, expected)
}
