//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modUser32                = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows          = modUser32.NewProc("EnumWindows")
	procGetWindowTextW       = modUser32.NewProc("GetWindowTextW")
	procIsWindowVisible      = modUser32.NewProc("IsWindowVisible")
	procSetWindowPos         = modUser32.NewProc("SetWindowPos")
	procGetDpiForWindow      = modUser32.NewProc("GetDpiForWindow")
	procSystemParametersInfo = modUser32.NewProc("SystemParametersInfoW")
	procSetForegroundWindow  = modUser32.NewProc("SetForegroundWindow")
)

const (
	callWindowTitle  = "DJ 4G Hub · 通話"
	callWindowWidth  = 420 // device-independent pixels
	callWindowHeight = 720
	spiGetWorkArea   = 0x0030
	swpNoZOrder      = 0x0004
	swpShowWindow    = 0x0040
)

// openCallWindow shows the compact call page in a Chrome or Edge app
// window, which has no tabs or address bar and is sized like a messaging
// app. Chrome is preferred because the user's microphone permission and
// audio choices for this site live in that profile.
func openCallWindow(url string) error {
	browser := findAppBrowser()
	if browser == "" {
		return errNoBrowser
	}
	existing := make(map[uintptr]bool)
	for _, hwnd := range findWindowsByExactTitle(callWindowTitle) {
		existing[hwnd] = true
	}
	command := exec.Command(browser, "--app="+url, "--window-size=420,720")
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	// A running Chrome ignores --window-size for new app windows, so size and
	// place the window once it appears.
	go placeCallWindow(existing)
	return nil
}

// placeCallWindow resizes the call window that appeared after launch to a
// messaging-app size in the bottom-right corner of the work area. Only an
// exact title match counts: an ordinary browser window showing the same
// page is titled "... - Google Chrome" and must never be touched.
func placeCallWindow(existing map[uintptr]bool) {
	for range 40 {
		time.Sleep(250 * time.Millisecond)
		var hwnd uintptr
		for _, candidate := range findWindowsByExactTitle(callWindowTitle) {
			if !existing[candidate] {
				hwnd = candidate
				break
			}
		}
		if hwnd == 0 {
			continue
		}
		dpi, _, _ := procGetDpiForWindow.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		width := int32(callWindowWidth * int(dpi) / 96)
		height := int32(callWindowHeight * int(dpi) / 96)
		var area struct{ left, top, right, bottom int32 }
		procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&area)), 0)
		x, y := area.right-width-16, area.bottom-height-16
		if x < area.left {
			x = area.left
		}
		if y < area.top {
			y = area.top
		}
		procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpShowWindow)
		procSetForegroundWindow.Call(hwnd)
		return
	}
}

// EnumWindows callbacks cannot be freed, so one callback serves every search.
var (
	windowSearchMu    sync.Mutex
	windowSearchTitle string
	windowSearchFound []uintptr
	windowSearch      = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 && windows.UTF16ToString(buf[:n]) == windowSearchTitle {
			windowSearchFound = append(windowSearchFound, hwnd)
		}
		return 1
	})
)

// findWindowsByExactTitle lists visible top-level windows titled exactly
// title.
func findWindowsByExactTitle(title string) []uintptr {
	windowSearchMu.Lock()
	defer windowSearchMu.Unlock()
	windowSearchTitle, windowSearchFound = title, nil
	procEnumWindows.Call(windowSearch, 0)
	return append([]uintptr(nil), windowSearchFound...)
}

func findAppBrowser() string {
	for _, exe := range []string{"chrome.exe", "msedge.exe"} {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			key, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			path, _, err := key.GetStringValue("")
			key.Close()
			if err == nil && fileExists(path) {
				return path
			}
		}
	}
	for _, path := range []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
	} {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
