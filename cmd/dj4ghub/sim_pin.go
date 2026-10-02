package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	simPINPattern     = regexp.MustCompile(`^[0-9]{4,8}$`)
	pinCounterPattern = regexp.MustCompile(`\+QPINC:\s*"SC",\s*(\d+),\s*(\d+)`)
)

type simPINResult struct {
	Unlocked     bool   `json:"unlocked"`
	State        string `json:"state"`
	PINRemaining *int   `json:"pin_remaining,omitempty"`
	PUKRemaining *int   `json:"puk_remaining,omitempty"`
	Message      string `json:"message"`
}

// parsePINCounters reads AT+QPINC="SC": remaining PIN and PUK attempts.
func parsePINCounters(resp string) (pin, puk *int) {
	m := pinCounterPattern.FindStringSubmatch(resp)
	if m == nil {
		return nil, nil
	}
	pinLeft, _ := strconv.Atoi(m[1])
	pukLeft, _ := strconv.Atoi(m[2])
	return &pinLeft, &pukLeft
}

func parseCPINState(resp string) string {
	if m := cpinPattern.FindStringSubmatch(resp); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// unlockSIMPIN submits the SIM PIN once. It never retries, never logs the
// PIN and never returns the raw modem response, which echoes the command.
func (a *app) unlockSIMPIN(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PIN                string `json:"pin"`
		ConfirmLastAttempt bool   `json:"confirm_last_attempt"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !simPINPattern.MatchString(body.PIN) {
		writeError(w, http.StatusBadRequest, "PIN 碼須為 4 到 8 位數字")
		return
	}
	if a.demo {
		writeJSON(w, http.StatusOK, simPINResult{Unlocked: true, State: "READY", Message: "示範：SIM 已解鎖"})
		return
	}
	query := func(command string) string {
		resp, _ := a.runATCommand(command, 5*time.Second)
		return resp
	}

	state := parseCPINState(query("AT+CPIN?"))
	pinLeft, pukLeft := parsePINCounters(query(`AT+QPINC="SC"`))
	result := simPINResult{State: state, PINRemaining: pinLeft, PUKRemaining: pukLeft}
	switch state {
	case "READY":
		result.Unlocked, result.Message = true, "SIM 已經解鎖，不需要 PIN"
		writeJSON(w, http.StatusOK, result)
		return
	case "SIM PIN":
	case "SIM PUK":
		writeError(w, http.StatusConflict, "SIM 已被鎖定，需要 PUK 碼；請向電信業者取得 PUK 後在手機上解鎖")
		return
	case "":
		writeError(w, http.StatusConflict, "無法讀取 SIM 狀態；請確認已插入 SIM 卡。若是開機後才插卡，模組不會自動偵測，請先重啟模組")
		return
	default:
		writeError(w, http.StatusConflict, fmt.Sprintf("SIM 目前狀態為 %s，此功能只處理 SIM PIN", state))
		return
	}
	if pinLeft != nil && *pinLeft <= 0 {
		writeError(w, http.StatusConflict, "已無 PIN 嘗試次數")
		return
	}
	if pinLeft != nil && *pinLeft == 1 && !body.ConfirmLastAttempt {
		result.Message = "只剩最後 1 次嘗試；輸入錯誤會鎖卡並需要 PUK"
		writeJSON(w, http.StatusConflict, result)
		return
	}

	resp, err := a.runATCommand(fmt.Sprintf(`AT+CPIN="%s"`, body.PIN), 10*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "送出 PIN 時與模組的連線中斷，請重新讀取狀態後再試")
		return
	}
	if atResponseIsError(resp) {
		pinLeft, pukLeft = parsePINCounters(query(`AT+QPINC="SC"`))
		result.PINRemaining, result.PUKRemaining = pinLeft, pukLeft
		result.State = parseCPINState(query("AT+CPIN?"))
		result.Message = "PIN 碼錯誤"
		if pinLeft != nil {
			result.Message = fmt.Sprintf("PIN 碼錯誤，剩餘 %d 次嘗試", *pinLeft)
		}
		writeJSON(w, http.StatusUnprocessableEntity, result)
		return
	}

	// The SIM reports READY shortly after a correct PIN.
	for range 20 {
		if state = parseCPINState(query("AT+CPIN?")); state == "READY" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	result.State = state
	result.PINRemaining, result.PUKRemaining = parsePINCounters(query(`AT+QPINC="SC"`))
	result.Unlocked = state == "READY"
	result.Message = "SIM 已解鎖"
	if !result.Unlocked {
		result.Message = "PIN 已接受，SIM 仍在初始化"
	}
	writeJSON(w, http.StatusOK, result)
}
