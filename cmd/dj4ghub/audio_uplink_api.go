package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
)

// moduleAudioUplink starts, mutes or stops the native microphone-to-module
// stream. It shares the audio session token, so only the page that
// prepared module audio can control it.
func (a *app) moduleAudioUplink(w http.ResponseWriter, r *http.Request) {
	if !allowModuleAudio(r) {
		writeError(w, http.StatusForbidden, "音訊控制僅允許本機同源存取")
		return
	}
	var body struct {
		Action     string `json:"action"`
		Microphone string `json:"microphone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	a.audioMu.Lock()
	session := a.audioSession
	valid := session != nil && r.Header.Get("X-DJ4Hub-Audio-Token") == session.token && time.Since(session.lastLease) < 50*time.Second
	a.audioMu.Unlock()
	if !valid && body.Action != "stop" {
		writeError(w, http.StatusConflict, "音訊工作階段已結束，請重新準備")
		return
	}

	a.uplinkMu.Lock()
	defer a.uplinkMu.Unlock()
	switch body.Action {
	case "start":
		if strings.TrimSpace(body.Microphone) == "" {
			writeError(w, http.StatusBadRequest, "請選擇電腦麥克風")
			return
		}
		if a.audioUplink != nil {
			a.audioUplink.Stop()
			a.audioUplink = nil
		}
		uplink, err := startAudioUplink(body.Microphone)
		if err != nil {
			log.Printf("module audio uplink failed: %v", err)
			writeError(w, http.StatusBadGateway, "無法把電腦麥克風送到模組："+err.Error())
			return
		}
		a.audioUplink = uplink
		log.Printf("module audio uplink: %s -> %s", uplink.Microphone, uplink.Module)
		writeJSON(w, http.StatusOK, map[string]any{"running": true, "microphone": uplink.Microphone, "module": uplink.Module})
	case "mute", "unmute":
		if a.audioUplink == nil {
			writeError(w, http.StatusConflict, "尚未連線電腦麥克風")
			return
		}
		a.audioUplink.SetMuted(body.Action == "mute")
		writeJSON(w, http.StatusOK, map[string]any{"running": true, "muted": body.Action == "mute"})
	case "stop":
		if a.audioUplink != nil {
			a.audioUplink.Stop()
			a.audioUplink = nil
			log.Printf("module audio uplink stopped")
		}
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
	default:
		writeError(w, http.StatusBadRequest, "未知的上行操作")
	}
}

func (a *app) stopAudioUplink() {
	a.uplinkMu.Lock()
	defer a.uplinkMu.Unlock()
	if a.audioUplink != nil {
		a.audioUplink.Stop()
		a.audioUplink = nil
	}
}

// monitorAudioUplink stops the microphone when its audio session ends
// without an explicit stop, for example when the page was closed.
func (a *app) monitorAudioUplink(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		a.audioMu.Lock()
		active := a.audioSession != nil && time.Since(a.audioSession.lastLease) < 50*time.Second
		a.audioMu.Unlock()
		a.uplinkMu.Lock()
		stale := a.audioUplink != nil && (!active || !a.audioUplink.Running())
		a.uplinkMu.Unlock()
		if stale {
			log.Printf("module audio uplink stopped: audio session ended")
			a.stopAudioUplink()
		}
	}
}
