package modem

import (
	"errors"
	"strings"
	"time"

	"go.bug.st/serial"
)

// SerialAT 是一個輕量級的串列埠包裝器，用於在不例項化整個 Manager 的情況下執行簡單的 AT 命令
type SerialAT struct {
	port serial.Port
}

// NewSerialAT 開啟串列埠並返回 SerialAT 例項
func NewSerialAT(portName string, baudRate int, dataBits int, stopBits int, parity string) (*SerialAT, error) {
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
	}

	switch stopBits {
	case 1:
		mode.StopBits = serial.OneStopBit
	case 2:
		mode.StopBits = serial.TwoStopBits
	default:
		mode.StopBits = serial.OneStopBit
	}

	switch strings.ToUpper(parity) {
	case "N":
		mode.Parity = serial.NoParity
	case "O":
		mode.Parity = serial.OddParity
	case "E":
		mode.Parity = serial.EvenParity
	default:
		mode.Parity = serial.NoParity
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return nil, err
	}

	// 設定預設超時
	if err := port.SetReadTimeout(time.Second); err != nil {
		port.Close()
		return nil, err
	}

	return &SerialAT{port: port}, nil
}

// Close 關閉串列埠
func (s *SerialAT) Close() error {
	return s.port.Close()
}

// Execute 傳送 AT 命令並等待響應
func (s *SerialAT) Execute(cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	// 臨時調整超時
	if err := s.port.SetReadTimeout(100 * time.Millisecond); err != nil {
		return "", err
	}

	// 清空緩衝區
	buf := make([]byte, 1024)
	for {
		n, _ := s.port.Read(buf)
		if n == 0 {
			break
		}
	}

	if err := s.port.SetReadTimeout(200 * time.Millisecond); err != nil {
		return "", err
	}

	if !strings.HasSuffix(cmd, "\r\n") {
		cmd += "\r\n"
	}

	if _, err := s.port.Write([]byte(cmd)); err != nil {
		return "", err
	}

	deadline := time.Now().Add(timeout)
	var response strings.Builder

	for time.Now().Before(deadline) {
		n, err := s.port.Read(buf)
		if n > 0 {
			response.Write(buf[:n])
			str := response.String()
			if strings.Contains(str, "OK\r\n") || strings.Contains(str, "ERROR\r\n") {
				return str, nil
			}
		}
		if err != nil {
			// ignore read errors (timeouts)
			continue
		}
	}

	return response.String(), errors.New("timeout")
}
