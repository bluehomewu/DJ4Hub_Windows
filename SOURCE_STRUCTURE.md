# DJ 4G Hub for Windows 原始碼結構

本專案以上游 [WongLoki/DJ4Hub](https://github.com/WongLoki/DJ4Hub) commit `3914cd6` 為基礎，將 macOS 平台層替換為 Windows 實作，並移除只屬於 macOS 版的元件。裝置、簡訊、eSIM 與通訊紀錄等共用邏輯維持與上游一致，方便日後合併上游修正。

## 目錄樹

```text
DJI Cellular/
├── cmd/
│   └── dj4ghub/                  # Windows 主程式與內嵌網頁
│       ├── *_windows.go          # Windows 平台層
│       └── web/                  # go:embed 編譯進執行檔的管理網頁
├── internal/
│   ├── apduarbiter/              # SIM/eUICC APDU 通道並行協調
│   ├── backend/                  # AT、MBIM、QMI 後端統一介面
│   ├── config/                   # 執行與裝置設定
│   ├── esim/                     # eUICC Profile 讀取、下載、切換、刪除
│   ├── modem/                    # 資料機探索、AT 指令與狀態解析
│   └── simaid/                   # SIM 應用 AID 探索
├── pkg/                          # MBIM、簡訊 PDU 編解碼、日誌
├── packaging/                    # 發行包內的說明與授權聲明
├── scripts/
│   ├── build-windows.ps1         # 測試、建置並打包 zip
│   └── phone-audio.test.cjs
├── third_party/                  # 上游保留的第三方原始碼
├── go.mod / go.sum
├── LICENSE
└── THIRD_PARTY_NOTICES.md
```

## Windows 平台層

| 檔案 | 用途 |
| --- | --- |
| `device.go`、`device_windows.go` | 以 SetupAPI 列舉 VID `2ca3` 的 PnP 節點，整理各 USB 介面、COM 埠、驅動問題與網卡 GUID |
| `usbat_windows.go` | 透過 Quectel「AT Port」COM 埠收發 AT 指令（取代上游的 libusb bulk 傳輸），不會探測 DM／NMEA 埠 |
| `hostnet.go`、`hostnet_windows.go` | 以 IP Helper 讀取網卡、預設路由與流量計數；依 NetCfgInstanceId 對應模組網卡；以 netsh 連線行動寬頻，或經 UAC 啟用被停用的網卡 |
| `network_probe_windows.go` | 以 `IP_UNICAST_IF` 將網際網路檢測繫結在模組網卡 |
| `network_activity_windows.go` | 以 `GetExtendedTcpTable` 列出經由行動網路 IP 的 TCP 連線與應用程式 |
| `launcher_windows.go`、`control.go` | `start/stop/status/logs/open` 子指令、背景服務與帶權杖的本機關閉端點 |
| `activate_windows.go` | 一次性確認模組網卡已連線 |
| `filelock_windows.go` | 以 `LockFileEx` 確保只有一個服務持有通訊紀錄 |
| `audio_uplink.go`、`audio_uplink_windows.go`、`audio_uplink_api.go` | 通話上行：以 WASAPI 擷取電腦麥克風、轉成 8 kHz 單聲道後送進模組音效卡（Windows Chrome 無法輸出到該裝置） |
| `audio_standby.go`、`call_window_windows.go` | 背景音訊待機、來電偵測，以及開啟並擺放小型通話視窗 |
| `audio_adb_windows.go` | 實驗通話音訊：以 PnP 位置確認唯一模組，以 adb 序號鎖定目標（Windows adb 不提供 USB 位置） |

## 驗證

```powershell
go test ./...
# 接上模組時的唯讀實機測試（只送查詢類 AT 指令；最後一項會經行動網路送出少量 HTTP 檢測）
go test -tags hardware -run Hardware -v ./cmd/dj4ghub
pwsh -File scripts/build-windows.ps1
```

## 與上游的差異

- 移除 SwiftUI 原生 App、libusb、`ioreg`／`networksetup`／`nettop` 等 macOS 專用程式碼與 GitHub Actions。
- 改用官方 `golang.org/x/sys` 模組；上游裁剪過的 `third_party/x-sys` 不含 Windows 套件。
- `internal/modem` 中以 `fuser` 強制結束佔用串列埠行程的 Linux 邏輯不會編進 Windows 版本。
- 實驗性模組音訊沿用上游流程；Windows 上以 PnP 位置與 adb 序號取代 macOS 的 USB 位置比對。

## 模組與來源

Go module 路徑仍為 `github.com/WongLoki/DJ4Hub`，以便與上游比對。從 DJOneHub、VoHive 與第三方模組演進而來的程式碼保留其原始授權與聲明，詳見 `LICENSE` 與 `THIRD_PARTY_NOTICES.md`。
