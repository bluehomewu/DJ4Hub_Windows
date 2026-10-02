package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIncomingCallWatcherAnnouncesEachCallOnce(t *testing.T) {
	var watcher incomingCallWatcher
	ringing := []voiceCall{{ID: 1, Direction: 1, State: 4, Number: "0900000000"}}
	if got := watcher.newIncoming(ringing); len(got) != 1 {
		t.Fatalf("first ring = %v", got)
	}
	if got := watcher.newIncoming(ringing); len(got) != 0 {
		t.Fatalf("repeated ring announced again: %v", got)
	}
	answered := []voiceCall{{ID: 1, Direction: 1, State: 0}}
	if got := watcher.newIncoming(answered); len(got) != 0 {
		t.Fatalf("answered call announced: %v", got)
	}
	if got := watcher.newIncoming([]voiceCall{{ID: 2, Direction: 0, State: 3}}); len(got) != 0 {
		t.Fatalf("outgoing call announced: %v", got)
	}
	watcher.newIncoming(nil)
	if got := watcher.newIncoming(ringing); len(got) != 1 {
		t.Fatalf("new call with a reused id not announced: %v", got)
	}
	if got := watcher.newIncoming([]voiceCall{{ID: 1, Direction: 1, State: 0}, {ID: 3, Direction: 1, State: 5}}); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("waiting call = %v", got)
	}
}

func TestStandbyBackoffGrowsAndResets(t *testing.T) {
	var backoff standbyBackoff
	now := time.Now()
	if !backoff.ready(now) {
		t.Fatal("fresh backoff not ready")
	}
	for _, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		if got := backoff.fail(now); got != want {
			t.Fatalf("delay = %s, want %s", got, want)
		}
	}
	if backoff.ready(now.Add(9 * time.Minute)) {
		t.Fatal("ready before the delay elapsed")
	}
	backoff.reset()
	if !backoff.ready(now) {
		t.Fatal("not ready after reset")
	}
}

func TestADBEnabledInUSBConfig(t *testing.T) {
	if adbEnabledInUSBConfig(`+QCFG: "usbcfg",0x2CA3,0x4006,1,1,1,1,1,0,0` + "\r\nOK") {
		t.Fatal("ADB bit 0 reported enabled")
	}
	if !adbEnabledInUSBConfig(`+QCFG: "usbcfg",0x2CA3,0x4006,1,1,1,1,1,1,0` + "\r\nOK") {
		t.Fatal("ADB bit 1 reported disabled")
	}
	if adbEnabledInUSBConfig("ERROR") {
		t.Fatal("error reported enabled")
	}
}

func TestPhoneSettingsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phone-settings.json")
	store := newPhoneSettingsStore(path)
	if got := store.Get(); !got.AudioStandby || !got.CallWindow {
		t.Fatalf("defaults = %+v", got)
	}
	if err := store.Set(phoneSettings{AudioStandby: false, CallWindow: true}); err != nil {
		t.Fatal(err)
	}
	if got := newPhoneSettingsStore(path).Get(); got.AudioStandby || !got.CallWindow {
		t.Fatalf("reloaded = %+v", got)
	}
}

func TestCallWindowURL(t *testing.T) {
	if got := callWindowURL("127.0.0.1:7575"); got != "http://127.0.0.1:7575/?window=call#calls" {
		t.Fatalf("url = %q", got)
	}
}
