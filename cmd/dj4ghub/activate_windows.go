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
		return errors.New("未检测到受支持的 DJI 4G 模块（USB 2ca3:4006）")
	}
	fmt.Fprintf(out, "已检测到模块：%s（%s:%s）\n", device.Product, device.VendorID, device.ProductID)
	for _, issue := range device.DriverIssues {
		fmt.Fprintf(out, "驱动提示：%s\n", issue)
	}

	service := currentDJINetworkService(device, discoverHostNetworkInterfaces())
	if networkServiceReady(service) {
		fmt.Fprintf(out, "上网网卡已经可用：%s，IP %s\n", service.Name, service.IPv4)
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
		return fmt.Errorf("Windows 没有识别到模块网卡（usbnet=%s）。usbnet=0 需要 Quectel NDIS 或 MBIM 驱动，usbnet=1 需要 ECM 驱动", mode)
	}

	if service.Disabled {
		fmt.Fprintf(out, "%s 已在 Windows 中停用，正在请求管理员权限启用...\n", service.Name)
	} else {
		fmt.Fprintf(out, "%s 尚未连接，正在请求 Windows 连接行动宽带...\n", service.Name)
	}
	if err := enableDJINetworkService(service); err != nil {
		return err
	}
	service = waitForDJINetworkService(device, activationWaitTimeout)
	if networkServiceReady(service) {
		fmt.Fprintf(out, "激活完成：%s，IP %s\n", service.Name, service.IPv4)
		return nil
	}
	return fmt.Errorf("已请求连接，但 %s 内网卡没有取得可用 IPv4 地址；请检查 APN 与 SIM 数据权限", activationWaitTimeout)
}
