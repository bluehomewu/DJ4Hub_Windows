//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"time"
)

const activationWaitTimeout = 40 * time.Second

// activateDJINetwork brings the module's Windows adapter online and exits.
// Unlike the macOS flow it never rewrites usbnet: with the Quectel NDIS/MBIM
// driver Windows already gets cellular data in usbnet=0, so changing the USB
// composition would only risk losing a working driver binding.
func activateDJINetwork(out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	device := discoverDJIUSBDevice()
	if device == nil {
		return errors.New("未偵測到受支援的 DJI 4G 模組（USB 2ca3:4006）")
	}
	fmt.Fprintf(out, "已偵測到模組：%s（%s:%s）\n", device.Product, device.VendorID, device.ProductID)
	for _, issue := range device.DriverIssues {
		fmt.Fprintf(out, "驅動提示：%s\n", issue)
	}

	service := currentDJINetworkService(device, discoverHostNetworkInterfaces())
	if networkServiceReady(service) {
		fmt.Fprintf(out, "上網網卡已經可用：%s，IP %s\n", service.Name, service.IPv4)
		return nil
	}
	if service == nil {
		mode := "未知"
		if at, err := openDJIUSBAT(); err == nil {
			if response, err := at.Command(`AT+QCFG="usbnet"`, 3*time.Second); err == nil {
				if parsed := parseUSBNetMode(response); parsed != "" {
					mode = parsed
				}
			}
			at.Close()
		}
		return fmt.Errorf("Windows 沒有偵測到模組網卡（usbnet=%s）。usbnet=0 需要 Quectel NDIS 或 MBIM 驅動，usbnet=1 需要 ECM 驅動", mode)
	}

	if service.Disabled {
		fmt.Fprintf(out, "%s 已在 Windows 中停用，正在請求管理員權限啟用...\n", service.Name)
	} else {
		fmt.Fprintf(out, "%s 尚未連線，正在請求 Windows 連線行動寬頻...\n", service.Name)
	}
	if err := enableDJINetworkService(service); err != nil {
		return err
	}
	service = waitForDJINetworkService(device, activationWaitTimeout)
	if networkServiceReady(service) {
		fmt.Fprintf(out, "啟用完成：%s，IP %s\n", service.Name, service.IPv4)
		return nil
	}
	return fmt.Errorf("已請求連線，但 %s 內網卡沒有取得可用 IPv4 位址；請檢查 APN 與 SIM 資料權限", activationWaitTimeout)
}
