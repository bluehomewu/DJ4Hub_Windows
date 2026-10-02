<p align="center">
  <img src="docs/images/dj-4g-hub-icon.png" width="128" alt="DJ 4G Hub icon">
</p>

# DJ 4G Hub for Windows

[DJ 4G Hub](https://github.com/WongLoki/DJ4Hub) 的 Windows 移植版：在 Windows 上透過 **DJI 4G 模組**（USB `2ca3:4006`，Quectel QDC507）原有的 USB 介面，提供裝置狀態、簡訊、SIM 通話控制、eSIM Profile、行動網路、聯網活動與 AT 除錯等功能，不修改模組韌體。

服務與網頁都在本機執行，預設只監聽 `127.0.0.1:7575`，沒有遠端遙測。

> [!IMPORTANT]
> 本專案是非官方的獨立專案，未獲 DJI、Quectel 或電信業者授權、贊助或認可。DJI 及相關產品名稱為其權利人的商標，僅用於說明相容性。

## 與 macOS 版的差異

| 專案 | macOS（上游） | Windows（本專案） |
| --- | --- | --- |
| AT 通道 | libusb 直接佔用 USB bulk 介面 | Quectel 驅動提供的「AT Port」COM 埠 |
| 行動上網 | 切到 `usbnet=1`（ECM）才有網卡 | `usbnet=0` 搭配 Quectel NDIS／MBIM 驅動即為 Windows「行動電話」介面，可同時收發簡訊與上網 |
| 網卡處理 | `networksetup` 啟用服務、重新 DHCP | 以 netsh 連線行動寬頻；網卡被停用時經 UAC 啟用 |
| 聯網活動 | `nettop`，含每條連線流量 | TCP 連線表，列出應用與目標；Windows 不提供單條連線流量 |
| 啟動器 | shell 指令碼 | `dj4ghub.exe` 內建子命令，雙擊即可啟動 |
| 原生 App | SwiftUI App | 不提供，改用網頁 |
| 實驗通話音訊 | ADB 載入驅動，比對 adb 的 USB 位置 | 同一套流程；Windows adb 不提供 USB 位置，改以 PnP 位置核對唯一模組並鎖定 adb 序號 |

## 需求

- Windows 10／11（x64 或 ARM64）
- DJI 4G 模組與可傳輸資料的 USB-C 線
- Quectel Windows USB 驅動程式。安裝後裝置管理員應出現：
  - `Quectel USB AT Port (COMx)`：必要，用於所有模組功能
  - 「行動電話」網路介面（NDIS 或 MBIM 驅動）：在 `usbnet=0` 下上網
  - ECM 驅動：僅在切到 `usbnet=1` 時需要

同一時間只能有一個程式開啟 AT 埠；使用前請關閉 QCOM、QNavigator 等工具。

## 使用

從 Release 或自行建置取得 `DJ-4G-Hub-<版本>-windows-amd64.zip`，解壓後雙擊 `dj4ghub.exe`，瀏覽器會開啟 `http://127.0.0.1:7575`。

```text
dj4ghub start          背景啟動並開啟管理網頁（雙擊 exe 等同此命令）
dj4ghub start --demo   不接硬體的示範模式
dj4ghub stop           停止背景服務
dj4ghub status         檢視執行狀態
dj4ghub logs           即時檢視日誌
dj4ghub open           重新開啟管理網頁
dj4ghub activate       檢查模組網卡，必要時連線 Windows 行動寬頻後結束
dj4ghub serve --port COM17
                       在前景執行並指定 AT 埠（除錯用）
```

## 工作模式

| 模式 | 頁面名稱 | Windows 上的效果 |
| --- | --- | --- |
| `usbnet=0` | 簡訊模式 | AT 埠 + 行動寬頻介面（需 NDIS／MBIM 驅動），建議使用 |
| `usbnet=1` | 上網模式 | CDC ECM 網卡（需 ECM 驅動） |
| `usbnet=2/3` | 實驗模式 | 未驗證 |

切換模式會讓 USB 重新列舉，頁面短暫斷線屬正常現象。不要在 eSIM Profile 寫入過程中拔除模組或切換模式。

## 通話與電腦音訊（實驗）

模組本身沒有喇叭和麥克風。要在電腦上通話，需要讓模組臨時多出一張 USB 音效卡；這沿用上游的實驗流程，已在 Windows 11 + QDC507GLEFM21 上驗證到「音效卡出現、心跳中斷後自動恢復」。

1. 安裝 [Android Platform Tools](https://developer.android.com/tools/releases/platform-tools)，讓 `adb.exe` 位於 PATH（或設定 `DJ4GHUB_ADB_PATH`）。
2. 取得 3 個固定版本的執行檔案（來源：[MaVo 固定 commit](https://github.com/moluncn/mavo/tree/0443dfdaf8aec086fd76ba2ee9152fd908114524/Resources/ModuleVoice)，雜湊見 [音訊研究紀錄](docs/QDC507_AUDIO_RESEARCH.md)），放進同一個資料夾後執行：
   ```powershell
   dj4ghub audio-install C:\path\to\ModuleVoice
   dj4ghub audio-check
   ```
3. 第一次撥號時，網頁會請你同意初始化：程式會備份 USB 設定、以 QADBKEY 授權並**永久開啟模組 ADB**，然後重啟模組一次。Windows 11 會自動以內建 WinUSB 驅動綁定「ADB Interface」。
4. 之後每次準備音訊時，程式會載入暫時性的核心驅動；Windows 會出現「麥克風 (AC Interface)」與「喇叭 (AC Interface)」。驅動在模組重啟後清除。

注意事項：
- 執行時請不要讓其他 adb 服務佔用模組；若執行過 `adb devices`，請先 `adb kill-server`。
- 模組音效卡出現時，Windows 可能自動把它設為預設播放／錄音裝置，請到「設定 → 系統 → 音效」改回原本的裝置。
- 撥號需要 SIM 支援 VoLTE，且模組 `AT+QCFG="ims"` 為 `1,1`（已啟用且已註冊）。
- 長時間通話、休眠恢復與上網並行尚未完成驗收。

## 本機資料

```text
%LOCALAPPDATA%\DJ 4G Hub\Logs\dj4ghub.log          日誌
%LOCALAPPDATA%\DJ 4G Hub\service.json              背景服務狀態
%APPDATA%\DJ4Hub\communication-history.sqlite       簡訊與通話紀錄（依 ICCID 區分）
%APPDATA%\DJ 4G Hub\profile-notes.json             eSIM Profile 備註
```

簡訊輪詢會先把模組 ME 儲存區的簡訊寫入本機紀錄，成功後再清除模組上的副本（沿用上游行為）。發布 Issue、截圖或日誌前，請遮蔽電話號碼、驗證碼、EID、ICCID、IMSI 等個人資訊。

## 從原始碼建置

需要 Go 1.26 以上與 PowerShell。

```powershell
go test ./...
pwsh -File scripts/build-windows.ps1            # 產出 dist\DJ-4G-Hub-<版本>-windows-amd64.zip
pwsh -File scripts/build-windows.ps1 -Arch arm64
```

接上模組時可執行唯讀實機測試：

```powershell
go test -tags hardware -run Hardware -v ./cmd/dj4ghub
```

程式結構與平臺層說明見 [SOURCE_STRUCTURE.md](SOURCE_STRUCTURE.md)。

## 目前限制

- 通話音訊仍屬實驗功能，需要上述第三方檔案與一次性的 ADB 授權。
- 通訊紀錄雲端備份的設定介面原本在 macOS App 中，Windows 版尚未提供。
- 執行檔未簽章，首次執行時 SmartScreen 可能提示。
- 不同 SIM、eUICC、電信業者、漫遊環境與模組韌體可能有差異。

## 來源與授權

本專案移植自 [WongLoki/DJ4Hub](https://github.com/WongLoki/DJ4Hub)（commit `3914cd6`），其程式碼又演進自 [ZenGeekLabs/DJOneHub](https://github.com/ZenGeekLabs/DJOneHub) 與 [iniwex5/vohive](https://github.com/iniwex5/vohive)。

根目錄程式碼遵循 [PolyForm Noncommercial License 1.0.0](LICENSE)，**不得作商業用途**。必須保留的上游宣告：

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

第三方元件授權見 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 與各 `third_party/` 目錄。
