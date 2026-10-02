//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// preferredATPort is set by `serve -port COMx` and bypasses discovery.
var preferredATPort string

// errATPortGone marks I/O failures that mean the COM port disappeared,
// typically because the module was unplugged or is re-enumerating.
var errATPortGone = errors.New("AT port NO_DEVICE")

// usbAT talks to the module's AT command interface. On Windows the Quectel
// USB serial driver exposes it as a COM port ("Quectel USB AT Port").
type usbAT struct {
	port serial.Port
	name string
	mu   sync.Mutex
}

func openDJIUSBAT() (*usbAT, error) {
	var candidates []string
	if preferredATPort != "" {
		candidates = []string{preferredATPort}
	} else {
		device := discoverDJIUSBDevice()
		if device == nil {
			return nil, errors.New("DJI USB device 2ca3:4006 not found")
		}
		candidates = atPortCandidates(device.Interfaces)
		if len(candidates) == 0 {
			if len(device.DriverIssues) > 0 {
				return nil, fmt.Errorf("未找到模块 AT 串口：%s；请安装 Quectel USB 驱动", strings.Join(device.DriverIssues, "；"))
			}
			return nil, errors.New("未找到模块 AT 串口；请确认已安装 Quectel USB 串口驱动")
		}
	}
	var lastErr error
	for _, name := range candidates {
		dev, err := openATPort(name)
		if err != nil {
			lastErr = err
			continue
		}
		response, err := dev.Command("AT", 1500*time.Millisecond)
		if err == nil && atProbeSucceeded(response) {
			return dev, nil
		}
		if err == nil {
			err = fmt.Errorf("unexpected AT probe response %q", response)
		}
		lastErr = fmt.Errorf("probe %s: %w", name, err)
		dev.Close()
	}
	return nil, lastErr
}

func openATPort(name string) (*usbAT, error) {
	port, err := serial.Open(name, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		var portErr *serial.PortError
		if errors.As(err, &portErr) && portErr.Code() == serial.PortBusy {
			return nil, fmt.Errorf("%s 正被其他程序占用（例如 QCOM、QNavigator 或另一个 DJ 4G Hub）", name)
		}
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	return &usbAT{port: port, name: name}, nil
}

func (u *usbAT) Close() {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.port == nil {
		return
	}
	_ = u.port.Close()
	u.port = nil
}

func (u *usbAT) Description() string {
	if u == nil {
		return "AT port"
	}
	return fmt.Sprintf("AT 串口 %s · 2ca3:4006", u.name)
}

func (u *usbAT) Command(cmd string, timeout time.Duration) (string, error) {
	if u == nil {
		return "", errors.New("AT port is not open")
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", errors.New("AT command is empty")
	}
	if !strings.HasPrefix(strings.ToUpper(cmd), "AT") {
		return "", errors.New("command must start with AT")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if u.port == nil {
		return "", errors.New("AT port is not open")
	}

	u.drainLocked()
	if err := u.writeLocked([]byte(cmd + "\r")); err != nil {
		return "", err
	}

	deadline := time.Now().Add(timeout)
	var response strings.Builder
	for time.Now().Before(deadline) {
		data, err := u.readLocked(minDuration(time.Until(deadline), 900*time.Millisecond))
		if err != nil {
			return normalizeATResponse(response.String()), err
		}
		if len(data) == 0 {
			continue
		}
		response.Write(data)
		if joined := response.String(); atResponseComplete(joined) {
			return normalizeATResponse(joined), nil
		}
	}
	if response.Len() == 0 {
		return "", errors.New("USB AT command timed out without response")
	}
	return normalizeATResponse(response.String()), nil
}

// CommandWithPrompt executes an AT command that enters an interactive input
// state, then submits followUp after the modem returns its ">" prompt.
func (u *usbAT) CommandWithPrompt(cmd string, followUp []byte, timeout time.Duration) (string, error) {
	if u == nil {
		return "", errors.New("AT port is not open")
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", errors.New("AT command is empty")
	}
	if !strings.HasPrefix(strings.ToUpper(cmd), "AT") {
		return "", errors.New("command must start with AT")
	}
	if len(followUp) == 0 {
		return "", errors.New("interactive AT follow-up is empty")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	if u.port == nil {
		return "", errors.New("AT port is not open")
	}

	u.drainLocked()
	if err := u.writeLocked([]byte(cmd + "\r")); err != nil {
		return "", err
	}

	deadline := time.Now().Add(timeout)
	var response strings.Builder
	promptReceived := false
	for time.Now().Before(deadline) {
		data, err := u.readLocked(minDuration(time.Until(deadline), 900*time.Millisecond))
		if err != nil {
			return normalizeATResponse(response.String()), err
		}
		if len(data) == 0 {
			continue
		}
		response.Write(data)
		joined := response.String()

		if !promptReceived {
			if atResponseIsError(joined) {
				return normalizeATResponse(joined), nil
			}
			if !atResponseHasPrompt(joined) {
				continue
			}
			if err := u.writeLocked(followUp); err != nil {
				return normalizeATResponse(joined), err
			}
			promptReceived = true
			continue
		}

		if atResponseComplete(joined) {
			return normalizeATResponse(joined), nil
		}
	}

	if promptReceived {
		// ESC cancels a pending message editor on modems that still accept input.
		_ = u.writeLocked([]byte{0x1b})
	}
	if response.Len() == 0 {
		return "", errors.New("USB interactive AT command timed out without response")
	}
	return normalizeATResponse(response.String()), errors.New("USB interactive AT command timed out before completion")
}

// drainLocked discards unsolicited result codes that arrived between commands.
func (u *usbAT) drainLocked() {
	for range 32 {
		data, err := u.readLocked(60 * time.Millisecond)
		if err != nil || len(data) == 0 {
			return
		}
	}
}

func (u *usbAT) writeLocked(payload []byte) error {
	for len(payload) > 0 {
		written, err := u.port.Write(payload)
		if err != nil {
			return fmt.Errorf("%w: write %s: %v", errATPortGone, u.name, err)
		}
		if written <= 0 {
			return fmt.Errorf("write %s: short write", u.name)
		}
		payload = payload[written:]
	}
	return nil
}

// readLocked returns no data and no error when the timeout elapses.
func (u *usbAT) readLocked(timeout time.Duration) ([]byte, error) {
	if timeout < 10*time.Millisecond {
		timeout = 10 * time.Millisecond
	}
	if err := u.port.SetReadTimeout(timeout); err != nil {
		return nil, fmt.Errorf("%w: configure %s: %v", errATPortGone, u.name, err)
	}
	buf := make([]byte, 1024)
	n, err := u.port.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", errATPortGone, u.name, err)
	}
	return buf[:n], nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
