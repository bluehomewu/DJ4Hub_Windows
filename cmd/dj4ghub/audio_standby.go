package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// phoneSettings are the user's call preferences, stored next to the history.
type phoneSettings struct {
	// AudioStandby keeps module audio prepared by the service so a call can
	// be answered with computer audio without an open phone page.
	AudioStandby bool `json:"audio_standby"`
	// CallWindow opens a compact call window when a call comes in.
	CallWindow bool `json:"call_window"`
}

func defaultPhoneSettings() phoneSettings {
	return phoneSettings{AudioStandby: true, CallWindow: true}
}

type phoneSettingsStore struct {
	mu       sync.Mutex
	path     string
	settings phoneSettings
}

func newPhoneSettingsStore(path string) *phoneSettingsStore {
	store := &phoneSettingsStore{path: path, settings: defaultPhoneSettings()}
	if path == "" {
		return store
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &store.settings); err != nil {
			log.Printf("phone settings unreadable, using defaults: %v", err)
			store.settings = defaultPhoneSettings()
		}
	}
	return store
}

func (s *phoneSettingsStore) Get() phoneSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *phoneSettingsStore) Set(settings phoneSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		temp := s.path + ".tmp"
		if err := os.WriteFile(temp, data, 0600); err != nil {
			return err
		}
		if err := os.Rename(temp, s.path); err != nil {
			return err
		}
	}
	s.settings = settings
	return nil
}

func (a *app) phoneSettingsStore() *phoneSettingsStore {
	a.settingsOnce.Do(func() {
		path := ""
		if !a.demo {
			if base, err := os.UserConfigDir(); err == nil {
				path = filepath.Join(base, "DJ4Hub", "phone-settings.json")
			}
		}
		a.settings = newPhoneSettingsStore(path)
	})
	return a.settings
}

func (a *app) getPhoneSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.phoneSettingsStore().Get())
}

func (a *app) putPhoneSettings(w http.ResponseWriter, r *http.Request) {
	if !allowModuleAudio(r) {
		writeError(w, http.StatusForbidden, "設定僅允許本機同源存取")
		return
	}
	var settings phoneSettings
	if !decodeJSON(w, r, &settings) {
		return
	}
	if err := a.phoneSettingsStore().Set(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "儲存設定失敗："+err.Error())
		return
	}
	if !settings.AudioStandby {
		a.stopServiceAudioSession("background standby turned off")
	}
	a.standbyBackoff.reset()
	writeJSON(w, http.StatusOK, settings)
}

// standbyBackoff spaces out failed background preparations.
type standbyBackoff struct {
	mu    sync.Mutex
	next  time.Time
	delay time.Duration
}

func (b *standbyBackoff) ready(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !now.Before(b.next)
}

func (b *standbyBackoff) fail(now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.delay == 0:
		b.delay = time.Minute
	case b.delay < 10*time.Minute:
		b.delay *= 2
		if b.delay > 10*time.Minute {
			b.delay = 10 * time.Minute
		}
	}
	b.next = now.Add(b.delay)
	return b.delay
}

func (b *standbyBackoff) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.delay, b.next = 0, time.Time{}
}

// adbEnabledInUSBConfig reports whether the one-time ADB authorization was
// already done. Background standby never performs it, because it rewrites
// the USB configuration and reboots the module.
func adbEnabledInUSBConfig(raw string) bool {
	config, err := parseAudioUSB(raw)
	return err == nil && config[7] == "1"
}

// monitorAudioStandby keeps a service-owned audio session prepared and
// renewed while background standby is enabled.
func (a *app) monitorAudioStandby(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		a.tendAudioStandby(ctx)
	}
}

func (a *app) tendAudioStandby(ctx context.Context) {
	if a.demo || !a.audioMu.TryLock() {
		return
	}
	defer a.audioMu.Unlock()
	session := a.audioSession
	if session != nil && time.Since(session.lastLease) < 50*time.Second {
		if session.owner == audioOwnerService {
			a.renewServiceSessionLocked(ctx, session)
		}
		return
	}
	if !a.phoneSettingsStore().Get().AudioStandby || !a.standbyBackoff.ready(time.Now()) {
		return
	}
	if _, _, err := moduleAudioRuntime(); err != nil {
		return
	}
	if a.currentUSBDevice() == nil {
		return
	}
	raw, err := a.phoneCommand(`AT+QCFG="usbcfg"`)
	if err != nil || !adbEnabledInUSBConfig(raw) {
		return // Initialization is done once from the phone page.
	}
	prepareCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	if _, err := a.prepareModuleAudioLocked(prepareCtx, false, audioOwnerService); err != nil {
		delay := a.standbyBackoff.fail(time.Now())
		log.Printf("module audio standby failed, retrying in %s: %v", delay, err)
		return
	}
	a.standbyBackoff.reset()
}

func (a *app) renewServiceSessionLocked(ctx context.Context, s *moduleAudioSession) {
	if time.Since(s.lastLease) < 15*time.Second {
		return
	}
	renewCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := "test \"$(cat " + audioQuote(s.dir+"/state") + ")\" = ready && cut -d . -f 1 /proc/uptime > " + audioQuote(s.dir+"/lease")
	if _, err := s.shell(renewCtx, command); err != nil {
		if time.Since(s.lastLease) > 40*time.Second {
			log.Printf("module audio standby lost: %v", err)
			a.audioSession = nil
			a.stopAudioUplink()
		}
		return
	}
	s.lastLease = time.Now()
}

// stopServiceAudioSession ends background standby before operations that
// need the original USB configuration, such as a usbnet switch or reboot.
func (a *app) stopServiceAudioSession(reason string) {
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	a.stopServiceAudioSessionLocked(reason)
}

func (a *app) stopServiceAudioSessionLocked(reason string) {
	s := a.audioSession
	if s == nil || s.owner != audioOwnerService {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.stopAudioUplink()
	_, _ = s.shell(ctx, "touch "+audioQuote(s.dir+"/stop"))
	for range 20 {
		state, _ := s.shell(ctx, "cat "+audioQuote(s.dir+"/state"))
		if state == "closed" || state == "reboot_required" || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	a.audioSession = nil
	log.Printf("module audio standby stopped: %s", reason)
}

// incomingCallWatcher opens one call window per incoming call.
type incomingCallWatcher struct {
	mu     sync.Mutex
	opened map[int]bool
}

// newIncoming returns the incoming or waiting calls not announced yet and
// forgets calls that ended.
func (w *incomingCallWatcher) newIncoming(calls []voiceCall) []voiceCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opened == nil {
		w.opened = map[int]bool{}
	}
	present := map[int]bool{}
	var fresh []voiceCall
	for _, call := range calls {
		present[call.ID] = true
		if call.Direction == 1 && (call.State == 4 || call.State == 5) && !w.opened[call.ID] {
			w.opened[call.ID] = true
			fresh = append(fresh, call)
		}
	}
	for id := range w.opened {
		if !present[id] {
			delete(w.opened, id)
		}
	}
	return fresh
}

func (a *app) announceIncomingCalls(calls []voiceCall) {
	fresh := a.incomingCalls.newIncoming(calls)
	if len(fresh) == 0 || a.callWindowURL == "" || !a.phoneSettingsStore().Get().CallWindow {
		return
	}
	if err := openCallWindow(a.callWindowURL); err != nil {
		log.Printf("call window: %v", err)
		return
	}
	log.Printf("call window opened for incoming call")
}

// callWindowURL builds the compact call page URL for a listen address.
func callWindowURL(listen string) string {
	return "http://" + strings.TrimSuffix(browserHost(listen), "/") + "/?window=call#calls"
}

var errNoBrowser = errors.New("找不到 Chrome 或 Edge，無法開啟通話視窗")
