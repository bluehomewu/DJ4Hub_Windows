package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	moduleAudioPort         = "5039"
	moduleAudioReadyTimeout = 90 * time.Second
)

var (
	//go:embed audio_session.sh
	moduleAudioScript string
	moduleAudioHashes = map[string]string{
		"qdc507_aprv3.ko":       "3d82d3dec4f1e323201bba87156df9d41438e08314097353f2607f9117211d4a",
		"qdc507_voice.ko":       "ed3821682d5309969a01c764192c83feff9669c61ef237c69475cd1619cf296c",
		"mavo-pcm-bridge.armv7": "88d47c15e61d1428a59c821fed804c2e6490e82859a085062f21966b58d167fc",
	}
	moduleAudioBootPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)
)

type moduleAudioSession struct {
	token     string
	usb       string
	boot      string
	dir       string
	adb       string
	owner     string
	lastLease time.Time
}

const (
	audioOwnerPage    = "page"
	audioOwnerService = "service"
)

type moduleAudioReply struct {
	Configured bool   `json:"configured"`
	Active     bool   `json:"active"`
	Token      string `json:"token,omitempty"`
	Summary    string `json:"summary,omitempty"`
	// NativeUplink tells the page that the service, not the browser, sends
	// computer microphone audio to the module.
	NativeUplink bool `json:"native_uplink"`
	// Owner is "service" for background standby; such a session is shared
	// with any local page and must not be stopped when a page closes.
	Owner string `json:"owner,omitempty"`
}

// Resolve locally supplied files only. Never download drivers or enable device ADB.
func moduleAudioPaths() (string, string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := os.Getenv("DJ4GHUB_MODULE_VOICE_DIR")
	if dir == "" {
		dir = filepath.Join(base, "DJ4Hub", "experimental-audio")
	}
	adb := os.Getenv("DJ4GHUB_ADB_PATH")
	if adb == "" {
		adb = filepath.Join(dir, "platform-tools", adbExecutableName)
		if _, err := os.Stat(adb); os.IsNotExist(err) {
			adb, _ = exec.LookPath("adb")
		}
	}
	return dir, adb, nil
}

func moduleAudioRuntime() (string, string, error) {
	dir, adb, err := moduleAudioPaths()
	if err != nil {
		return "", "", err
	}
	if _, err = audioRuntimeFiles(dir); err != nil {
		return "", "", fmt.Errorf("%w；使用 dj4ghub audio-install 匯入本機執行檔案", err)
	}
	info, err := os.Stat(adb)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return "", "", errors.New("未找到可執行 adb；請安裝官方 Android Platform Tools，並加入 PATH 或設定 DJ4GHUB_ADB_PATH")
	}
	return dir, adb, nil
}

// Writes require a non-simple header and a loopback origin. No remote hardware control.
func allowModuleAudio(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return false
	}
	hostURL, err := url.Parse("http://" + r.Host)
	if err != nil {
		return false
	}
	host = hostURL.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, parseErr := url.Parse(origin)
		if parseErr != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			return false
		}
	}
	return r.Method == http.MethodGet || r.Header.Get("X-DJ4Hub-Audio") == "1"
}

func audioQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// Serial-less modules are selected by USB location; never fall back to another device.
func moduleAudioTarget(list string, expectedUSB string) (string, string, error) {
	var targets [][2]string
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		device, usb, transport := false, "", ""
		for _, field := range fields {
			if field == "device" {
				device = true
			}
			if strings.HasPrefix(field, "usb:") {
				usb = strings.TrimPrefix(field, "usb:")
			}
			if strings.HasPrefix(field, "transport_id:") {
				transport = strings.TrimPrefix(field, "transport_id:")
			}
		}
		if usb == "" && device && len(fields) > 0 {
			usb = adbSerialIdentity(fields[0])
		}
		if device && usb != "" && transport != "" {
			targets = append(targets, [2]string{usb, transport})
		}
	}
	if expectedUSB != "" {
		// A pinned identity may coexist with other adb devices; it must
		// still match exactly one of them.
		var matched [][2]string
		for _, target := range targets {
			if target[0] == expectedUSB {
				matched = append(matched, target)
			}
		}
		targets = matched
	}
	if len(targets) != 1 {
		return "", "", errors.New("找不到唯一的模組 ADB 裝置；裝置發生變化時不會自動切換目標")
	}
	return targets[0][0], targets[0][1], nil
}

func audioExec(ctx context.Context, adb string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, adb, append([]string{"-P", moduleAudioPort}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ADB 操作失敗：%w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *moduleAudioSession) target(ctx context.Context) (string, error) {
	list, err := audioExec(ctx, s.adb, "devices", "-l")
	if err != nil {
		return "", err
	}
	usb, transport, err := moduleAudioTarget(list, s.usb)
	if err == nil {
		s.usb = usb
	}
	return transport, err
}

func (s *moduleAudioSession) shell(ctx context.Context, command string) (string, error) {
	transport, err := s.target(ctx)
	if err != nil {
		return "", err
	}
	if s.boot != "" {
		command = "test \"$(cat /proc/sys/kernel/random/boot_id)\" = " + audioQuote(s.boot) + " && ( " + command + " )"
	}
	return audioExec(ctx, s.adb, "-t", transport, "shell", command)
}

func audioRuntimeFiles(dir string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for name, expected := range moduleAudioHashes {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return nil, fmt.Errorf("缺少有效音訊執行檔案：%s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != expected {
			return nil, fmt.Errorf("音訊執行檔案驗證失敗：%s", name)
		}
		files[name] = data
	}
	files["session.sh"] = []byte(moduleAudioScript)
	return files, nil
}

func (a *app) moduleAudioStatus(w http.ResponseWriter, r *http.Request) {
	if !allowModuleAudio(r) {
		writeError(w, 403, "音訊控制僅允許本機同源存取")
		return
	}
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	active := a.audioSession != nil && time.Since(a.audioSession.lastLease) < 50*time.Second
	reply := moduleAudioReply{Active: active, NativeUplink: true}
	if active && a.audioSession.owner == audioOwnerService {
		// Background standby is shared with every local page of this origin.
		reply.Owner, reply.Token = audioOwnerService, a.audioSession.token
	}
	_, _, err := moduleAudioRuntime()
	summary := "本機音訊依賴已驗證，可在撥號前準備模組音訊。"
	if err != nil {
		summary = err.Error()
	}
	if a.demo {
		summary = "示範模式不操作真實音訊硬體"
	}
	reply.Configured, reply.Summary = err == nil && !a.demo, summary
	writeJSON(w, 200, reply)
}

// audioPrepareError carries the HTTP status that matches a failure.
type audioPrepareError struct {
	status  int
	message string
}

func (e *audioPrepareError) Error() string { return e.message }

func prepareFailure(status int, message string) error {
	return &audioPrepareError{status: status, message: message}
}

func (a *app) moduleAudioPrepare(w http.ResponseWriter, r *http.Request) {
	if !allowModuleAudio(r) {
		writeError(w, 403, "音訊控制僅允許本機同源存取")
		return
	}
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	s, err := a.prepareModuleAudioLocked(ctx, r.Header.Get("X-DJ4Hub-Initialize") == "1", audioOwnerPage)
	if err != nil {
		status := 502
		var prepareErr *audioPrepareError
		if errors.As(err, &prepareErr) {
			status = prepareErr.status
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, moduleAudioReply{Configured: true, Active: true, Token: s.token, Owner: s.owner, Summary: "模組音訊已準備；請選擇電腦裝置後連線音訊。關閉頁面後自動恢復，驅動在模組重啟後清除。"})
}

// prepareModuleAudioLocked loads the module audio runtime and installs the
// session. initialize allows the one-time ADB authorization, which may
// reboot the module; background standby never passes it. The caller holds
// audioMu.
func (a *app) prepareModuleAudioLocked(ctx context.Context, initialize bool, owner string) (*moduleAudioSession, error) {
	dir, adb, runtimeErr := moduleAudioRuntime()
	if runtimeErr != nil || a.demo {
		if runtimeErr != nil {
			return nil, prepareFailure(409, runtimeErr.Error())
		}
		return nil, prepareFailure(409, "示範模式不操作真實音訊硬體")
	}
	if a.audioSession != nil && time.Since(a.audioSession.lastLease) < 50*time.Second {
		return nil, prepareFailure(409, "已有音訊工作階段，請先停止或等待自動恢復")
	}
	raw, err := a.phoneCommand("AT+CLCC")
	if err != nil || len(parseVoiceCalls(raw)) != 0 {
		return nil, prepareFailure(409, "請在無通話時準備模組音訊")
	}
	raw, err = a.phoneCommand("ATI")
	if err != nil || !strings.Contains(raw, "QDC507GLEFM21") {
		return nil, prepareFailure(409, "目前僅驗證了 QDC507GLEFM21 韌體")
	}
	files, err := audioRuntimeFiles(dir)
	if err != nil {
		return nil, prepareFailure(409, err.Error())
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return nil, prepareFailure(500, "無法建立音訊工作階段")
	}
	s := &moduleAudioSession{token: hex.EncodeToString(token), adb: adb, owner: owner}
	s.dir = "/tmp/dj4hub-audio-" + s.token
	location, locationErr := audioUSBLocation(ctx)
	if locationErr != nil {
		return nil, prepareFailure(409, locationErr.Error())
	}
	s.usb = adbIdentityForLocation(location)
	if initialize {
		identity, identityErr := audioIdentity(a.phoneCommand)
		if identityErr != nil {
			return nil, prepareFailure(409, "無法讀取穩定裝置身分，未初始化 ADB")
		}
		base, pathErr := os.UserConfigDir()
		if pathErr != nil {
			return nil, prepareFailure(500, "無法定位設定備份目錄")
		}
		_, initErr := initializeAudioADB(ctx, a.phoneCommand, filepath.Join(base, "DJ4Hub", "device-backups"), identity)
		if initErr != nil {
			return nil, prepareFailure(409, initErr.Error())
		}
		if currentLocation, e := audioUSBLocation(ctx); e != nil || currentLocation != location {
			return nil, prepareFailure(409, "USB 位置發生變化，請重新準備；未載入驅動")
		}
		if _, targetErr := s.target(ctx); targetErr != nil {
			// Some legacy builds need the current challenge resubmitted after reboot.
			if authErr := authorizeAudioADB(a.phoneCommand, identity); authErr != nil {
				return nil, prepareFailure(409, authErr.Error())
			}
			for i := 0; i < 8; i++ {
				if _, targetErr = s.target(ctx); targetErr == nil {
					break
				}
				select {
				case <-ctx.Done():
					return nil, prepareFailure(409, "等待 ADB 逾時")
				case <-time.After(time.Second):
				}
			}
			if targetErr != nil {
				return nil, prepareFailure(409, "ADB 設定已啟用，但連線不可用；可能有其他 ADB 服務佔用模組（例如執行過 adb devices，可執行 adb kill-server），或請重新插拔。不會自動終止其他程式")
			}
		}
		if current, e := audioIdentity(a.phoneCommand); e != nil || current != identity {
			return nil, prepareFailure(409, "裝置身分變化，未載入驅動")
		}
	}
	if err = s.prepare(ctx, files); err != nil {
		log.Printf("module audio prepare failed: %v", err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = s.shell(cleanupCtx, "test ! -d "+audioQuote(s.dir)+" || touch "+audioQuote(s.dir+"/stop"))
		cleanupCancel()
		return nil, prepareFailure(502, "音訊準備失敗："+err.Error()+"；已請求停止，若裝置未恢復請重新插拔模組")
	}
	s.lastLease = time.Now()
	a.audioSession = s
	log.Printf("module audio: session prepared for %s", owner)
	return s, nil
}

func (s *moduleAudioSession) prepare(ctx context.Context, files map[string][]byte) error {
	preflight := `test "$(uname -r)" = 3.18.44 && test "$(cat /sys/class/android_usb/android0/idVendor)" = 2ca3 && test "$(cat /sys/class/android_usb/android0/idProduct)" = 4006 && cat /proc/sys/kernel/random/boot_id`
	boot, err := s.shell(ctx, preflight)
	if err != nil || !moduleAudioBootPattern.MatchString(boot) {
		return errors.New("裝置身分、核心、USB 設定或 ADB 權限不符合已驗證條件")
	}
	s.boot = boot
	functions, err := s.shell(ctx, "cat /sys/class/android_usb/android0/functions")
	if err != nil {
		return fmt.Errorf("無法讀取裝置 USB 功能設定：%w", err)
	}
	if !supportedAudioFunctions(functions) {
		return fmt.Errorf("目前 USB 功能設定不支援音訊初始化：%q；僅支援 RMNET 或 ECM 組合及其 USB 語音介面", functions)
	}
	if err := ensureAudioRoot(ctx, func() (string, error) { return s.shell(ctx, "id -u") }, func() error {
		transport, err := s.target(ctx)
		if err != nil {
			return err
		}
		output, err := audioExec(ctx, s.adb, "-t", transport, "root")
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(output), "cannot") || strings.Contains(strings.ToLower(output), "denied") {
			return errors.New("裝置韌體不允許 adb root；未上傳或載入驅動")
		}
		return nil
	}); err != nil {
		return err
	}
	// Finished sessions keep about 1 MB of drivers in the module's RAM-backed
	// /tmp until reboot; remove them so repeated calls cannot exhaust it.
	// prepare only runs when the service holds no live session, so any
	// session still running on the module is an orphan, for example from a
	// service that was restarted. Ask it to stop and wait for its cleanup.
	orphans := `n=0; for d in /tmp/dj4hub-audio-*; do test -f "$d/state" || continue; case "$(cat "$d/state")" in preparing|ready) touch "$d/stop"; n=$((n+1));; esac; done; echo $n`
	if count, err := s.shell(ctx, orphans); err == nil && count != "0" && count != "" {
		log.Printf("module audio: stopping %s orphaned session(s)", count)
		// Stopping restores the USB functions, so adb drops briefly while the
		// module re-enumerates; poll from here and tolerate those failures.
		busy := `for d in /tmp/dj4hub-audio-*; do test -f "$d/state" || continue; case "$(cat "$d/state")" in preparing|ready|stopping) echo busy; exit 0;; esac; done; echo idle`
		deadline := time.Now().Add(60 * time.Second)
		for {
			if state, err := s.shell(ctx, busy); err == nil && state == "idle" {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("模組上仍有未結束的音訊工作階段，請稍後再試")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	cleanup := `for d in /tmp/dj4hub-audio-*; do test -d "$d" || continue; test "$(cat "$d/state" 2>/dev/null)" = closed && rm -rf "$d"; done; true`
	if _, err = s.shell(ctx, cleanup); err != nil {
		return err
	}
	if _, err = s.shell(ctx, "mkdir -m 700 "+audioQuote(s.dir)); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "dj4hub-audio-stage-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for name, data := range files {
		local := filepath.Join(stage, name)
		if err = os.WriteFile(local, data, 0600); err != nil {
			return err
		}
		transport, targetErr := s.target(ctx)
		if targetErr != nil {
			return targetErr
		}
		if _, err = audioExec(ctx, s.adb, "-t", transport, "push", local, s.dir+"/"+name); err != nil {
			return err
		}
	}
	for name, expected := range moduleAudioHashes {
		if _, err = s.shell(ctx, "test \"$(sha256sum "+audioQuote(s.dir+"/"+name)+" | cut -d ' ' -f 1)\" = "+audioQuote(expected)); err != nil {
			return errors.New("裝置端音訊檔案驗證失敗")
		}
	}
	launch := "chmod 700 " + audioQuote(s.dir+"/mavo-pcm-bridge.armv7") + "; cut -d . -f 1 /proc/uptime > " + audioQuote(s.dir+"/lease") + "; setsid sh " + audioQuote(s.dir+"/session.sh") + " " + audioQuote(s.dir) + " </dev/null >" + audioQuote(s.dir+"/session.log") + " 2>&1 & sleep 1"
	// Re-enumeration may drop this reply. Readiness is determined by device readback.
	_, _ = s.shell(ctx, launch)
	// Adding the audio function re-enumerates the whole composite device. On
	// Windows, adb is unreachable until the driver stack binds again, which
	// can take well over 20 seconds, so wait on elapsed time, not attempts.
	started := time.Now()
	deadline := started.Add(moduleAudioReadyTimeout)
	lastState := ""
	for time.Now().Before(deadline) {
		state, readErr := s.shell(ctx, "cat "+audioQuote(s.dir+"/state"))
		if readErr == nil && state != lastState {
			log.Printf("module audio: session state %q after %s", state, time.Since(started).Round(time.Second))
			lastState = state
		}
		if readErr == nil && state == "ready" {
			_, err = s.shell(ctx, "grep -q '^state: RUNNING' /proc/asound/card0/pcm4p/sub0/status && grep -q '^state: RUNNING' /proc/asound/card0/pcm4c/sub0/status && test \"$(cat /sys/class/android_usb/f_audio/audio_enable)\" = 1 && cut -d . -f 1 /proc/uptime > "+audioQuote(s.dir+"/lease"))
			if err == nil {
				log.Printf("module audio: ready after %s", time.Since(started).Round(time.Second))
				return nil
			}
		}
		if state == "closed" || state == "reboot_required" {
			failure, _ := s.shell(ctx, "cat "+audioQuote(s.dir+"/failure"))
			if failure != "" && len(failure) < 100 {
				return fmt.Errorf("模組音訊初始化失敗階段：%s", failure)
			}
			return errors.New("模組音訊初始化未完成或已停止，請重試")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	log.Printf("module audio: USB audio not ready after %s (last state %q)", moduleAudioReadyTimeout, lastState)
	return errors.New("等待 USB 音訊介面逾時")
}

func supportedAudioFunctions(functions string) bool {
	base := strings.TrimSuffix(functions, ",audio")
	return base == "diag,serial,rmnet,ffs" || base == "diag,serial,ecm,ffs"
}

func (a *app) moduleAudioLease(w http.ResponseWriter, r *http.Request) {
	a.moduleAudioUpdate(w, r, false)
}

func (a *app) moduleAudioStop(w http.ResponseWriter, r *http.Request) {
	a.moduleAudioUpdate(w, r, true)
}

func (a *app) moduleAudioUpdate(w http.ResponseWriter, r *http.Request, stop bool) {
	if !allowModuleAudio(r) {
		writeError(w, 403, "音訊控制僅允許本機同源存取")
		return
	}
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	s := a.audioSession
	if s == nil || r.Header.Get("X-DJ4Hub-Audio-Token") != s.token {
		writeError(w, 409, "音訊工作階段已結束，請重新準備")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	command := "test \"$(cat " + audioQuote(s.dir+"/state") + ")\" = ready && cut -d . -f 1 /proc/uptime > " + audioQuote(s.dir+"/lease")
	if stop {
		command = "touch " + audioQuote(s.dir+"/stop")
	}
	_, err := s.shell(r.Context(), command)
	if err != nil {
		writeError(w, 502, "無法聯絡音訊工作階段；心跳停止後模組將嘗試自動恢復，必要時重新插拔")
		return
	}
	if stop {
		for i := 0; i < 20; i++ {
			state, _ := s.shell(r.Context(), "cat "+audioQuote(s.dir+"/state"))
			if state == "closed" {
				a.audioSession = nil
				a.stopAudioUplink()
				writeJSON(w, 200, moduleAudioReply{Configured: true, Summary: "模組音訊已停止，USB 設定已恢復；臨時驅動在重啟後清除"})
				return
			}
			if state == "reboot_required" {
				break
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
		writeError(w, 502, "尚未確認恢復完成；請重新插拔模組，不要繼續切換音訊")
		return
	}
	s.lastLease = time.Now()
	writeJSON(w, 200, moduleAudioReply{Configured: true, Active: true})
}
