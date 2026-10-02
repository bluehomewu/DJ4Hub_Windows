# DJ 4G Hub for Windows

免安裝的可攜版本，只有一個 `dj4ghub.exe`，不需要安裝 Go、Node.js 或 .NET。

## 事前準備

1. Windows 10／11（x64 或 ARM64）。
2. 安裝 Quectel USB 驅動程式。裝置管理員中應出現「Quectel USB AT Port (COMx)」。
   - `usbnet=0`（預設）：安裝 Quectel NDIS 或 MBIM 驅動後，Windows 會多出一個「行動電話」網路介面，可直接以行動寬頻上網，同時保留 AT 埠收發簡訊。
   - `usbnet=1`：模組改為 CDC ECM 網卡，需要另外安裝 ECM 驅動。
3. 同一時間只能有一個程式開啟 AT 埠；使用前請關閉 QCOM、QNavigator 等工具。

## 啟動

直接雙擊 `dj4ghub.exe`，或在終端機執行：

```powershell
.\dj4ghub.exe start
```

服務會在背景執行，瀏覽器自動開啟 `http://127.0.0.1:7575/`。

## 常用指令

```text
dj4ghub start          背景啟動並開啟管理網頁
dj4ghub start --demo   不接硬體的示範模式
dj4ghub stop           停止背景服務
dj4ghub status         檢視執行狀態
dj4ghub logs           即時檢視日誌（Ctrl+C 離開）
dj4ghub open           重新開啟管理網頁
dj4ghub activate       檢查模組網卡，必要時連線 Windows 行動寬頻後結束
dj4ghub serve --port COM17
                       在前景執行並指定 AT 埠（除錯用）
```

## 資料位置

```text
%LOCALAPPDATA%\DJ 4G Hub\Logs\dj4ghub.log         日誌
%LOCALAPPDATA%\DJ 4G Hub\service.json             背景服務狀態
%APPDATA%\DJ4Hub\communication-history.sqlite      簡訊與通話紀錄
%APPDATA%\DJ 4G Hub\profile-notes.json            eSIM Profile 備註
```

## 已知限制

- 電腦通話音訊為實驗功能，需要 adb 與 3 個第三方檔案（`dj4ghub audio-install DIR`），第一次使用會永久開啟模組 ADB 並重啟一次；詳見原始碼 README。未準備時仍可撥號、接聽、掛斷，但電腦端沒有聲音。
- 「連網活動」只列出經由模組網卡的 TCP 連線與應用程式，Windows 不提供單條連線的流量。
- 執行檔未經程式碼簽章，首次執行時 SmartScreen 可能出現提示。
