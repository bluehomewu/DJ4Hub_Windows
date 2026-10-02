# QDC507 通話音訊研究

2026-09-13 更新：新用戶端已增加舊式 QDC507 的首次 ADB 授權及設定備份，見 [初始化方案與新裝置實測](QDC507_INITIALIZATION.md)。以下歷史記錄中「不自動開啟 ADB」描述的是此前版本；韌體不刷寫、音訊執行時臨時載入的邊界不變。

核驗日期：2026-09-12。本文按階段記錄研究證據；最新測試範圍如下，不代表所有裝置相容或完整雙向通話品質認證。

## 最新實機測試與本機整合

最新互動改為自動音訊：首次授權後，撥號前自動初始化並連線音訊，進入電話頁可提前初始化；掛斷關閉電腦媒體流但保持模組待機，取消一小時硬上限，仍保留失聯 45 秒恢復。正在響鈴而音訊未就緒時拒絕重新連線，可選擇僅電話控制。以下「一小時上限」為早期實驗記錄，不是目前行為。新自動流程通過模擬迴歸測試；未額外撥號，長期待機與上網並行尚未實測。

常規程式與便攜包已包含控制程式碼、本機依賴檢查和安全匯入指令，不再要求每次啟動傳環境變數。預設目錄為 `~/Library/Application Support/DJ4Hub/experimental-audio`；`dj4ghub audio-install DIR` 僅匯入雜湊吻合的三個檔案，`dj4ghub audio-check` 只檢查本機檔案、不操作硬體。ADB 來自該目錄的 platform-tools/adb 或 PATH。第三方二進位未隨包分發。

除下方兩個核心模組外，還需同一固定版本的 `mavo-pcm-bridge.armv7`，SHA-256 為 `88d47c15e61d1428a59c821fed804c2e6490e82859a085062f21966b58d167fc`。

後續瀏覽器雙向通話驗證已完成（2026-09-12）：本機瀏覽器透過 getUserMedia / setSinkId 將 MacBook 麥克風送至模組 AS Interface，將模組 AC Interface 送至 MacBook 喇叭。裝置先報告響鈴，再報告通話中；測試者分別對電腦和手機說話後明確反饋「兩邊都清楚」。掛斷後網頁顯示無語音通話、音訊已中斷。未儲存錄音。這是本台 QDC507GLEFM21 的短時實際雙向驗證，尚未覆蓋長時間通話、所有瀏覽器、藍芽耳機或其他韌體。下面合成提示測試為較早階段，保留其證據邊界。

在使用者明確授權的第二次電話測試中，AT 先報告呼叫中再報告接通，接聽者確認聽清英文合成測試提示。電腦向模組播放 8 kHz / 16-bit / mono PCM 成功。模組回傳的 12 秒取樣包含 96,768 個樣本，RMS -37.31 dBFS、峰值 -5.99 dBFS，沒有儲存通話錄音。這裡只證明回傳非零，尚未驗證電腦喇叭可聽品質或瀏覽器即時全雙工表現。第一次測試雖然 AT 報告接通，使用者未收到來電，不能作為成功證據。所有測試均已掛斷，不內建號碼或自動撥號。

本地新增可選的準備、心跳續期、停止恢復介面：固定檔案雜湊、韌體與 USB 識別碼驗證、單裝置及啟動 ID 繫結、本機同源限制、隨機工作階段權杖。裝置端獨立看門狗在失去續期約 45 秒後恢復 USB；工作階段上限一小時。第三方執行檔案由操作者在本機提供，不進入儲存庫或發行包。已驗證手動停止恢復，以及不續心跳後狀態進入 closed、USB 功能恢復 diag,serial,rmnet,ffs；長期穩定性仍需持續驗證。

以下保留較早階段的結論，不能混同最新測試範圍。

## 目前結論

通話控制與聲音傳輸是兩條不同路徑。USB 網卡本身不能當作麥克風；需要模組將通話音訊匯出為 PCM 或 USB 音訊。目前不能靠選擇 MacBook 麥克風作為「模組裝置」達成通話。

最初唯讀研究未載入驅動。隨後使用者明確接受繼續實驗，進行了下述臨時載入測試；沒有刷寫韌體或撥打電話。

## 後續受控載入測試（2026-09-12）

- 載入前確認 root ADB、核心 3.18.44、無已載入模組、無音效卡、無目前通話。
- 從上述 MaVo 固定提交下載兩個模組；Mac 與裝置端 SHA-256 均與表中一致。
- 檔案僅放在裝置 `/tmp/dj4hub-voice-audit-20260912`，沒有安裝啟動項。
- APR 和 voice 模組均進入 Live 狀態；ALSA card 0 出現，包含 VoLTE D4、AFE-PROXY D5 播放和 D6 擷取。
- 裝置節點經過短暫延遲出現，不能只依據 insmod 後立即執行的 ls 判斷失敗。
- 載入後 AT 通話狀態查詢仍成功回傳空清單，未觀察到裝置重啟。
- 日誌包含 APR IPC log context 建立失敗，以及多項 ASoC DAPM 路由和 debugfs 建立錯誤。尚未判斷哪些是裁剪音效卡的非關鍵警告，哪些影響實際通話。
- 未開啟 UAC、未啟動音訊流、未擷取麥克風，未驗證非零樣本或雙向聲音。
- 因這些異常停止進一步啟用音訊，使用重啟清除臨時驅動，不嘗試可能存在 DSP 回呼風險的熱解除安裝。
- 重啟後複核：`/proc/modules` 為空、ALSA 恢復無音效卡、audio_enable 為 0、裝置臨時檔案已清除，AT 狀態查詢成功。實驗用本機 ADB server 已停止，此前授權啟用的裝置 ADB 介面保持不變。

這證明該二進位能夠在本次裝置上建立內部音效卡，並不證明聲音已通或驅動適合自動整合。

## 校準及臨時 UAC 列舉測試（2026-09-12）

- 固定版本 PCM helper 的 SHA-256 為 `88d47c15e61d1428a59c821fed804c2e6490e82859a085062f21966b58d167fc`；`--check` 成功解析裝置廠商庫符號。
- 在沒有校準時，15 秒限時 `--voice-route-session` 能使 D4 收發端進入 RUNNING，但核心報告缺少 cal 2/3/4/7 和音量指令 ADSP_EFAILED。RUNNING 不能等同於正常音訊。
- 使用裝置內建 `alsaucm_test`，選擇 Tomtom I2S / VoLTE / Auxpcm Rx、Tx，出現 `Sent VocProc Cal!`。仍存在部分 AFE/ANC 資料缺失提示，不應隱藏。
- 校準後第二次 10 秒限時路由測試仍可啟動；本次啟動期間沒有新增上述缺校準及音量失敗日誌。未驗證實際取樣。
- 原 USB functions 為 `diag,serial,rmnet,ffs`，沒有 audio。僅設定 audio_enable=1 不會讓 Mac 列舉音效卡。
- 透過 sysfs 臨時將 functions 改為 `diag,serial,rmnet,ffs,audio`（不改 AT 持久設定），使用 30 秒自動恢復腳本保護。
- macOS `system_profiler SPAudioDataType` 實測出現 BAIWANG `AC Interface` 輸入和 `AS Interface` 輸出，均為 USB、8 kHz、單聲道。Mac 內建預設輸入/輸出未改變。
- 沒有撥號、錄音或使用電腦麥克風。USB 列舉成功與實際雙向語音成功是不同驗收項。
- 預設自動恢復腳本在隨後檢查時未恢復 functions；不能依賴 ADB 啟動的背景 shell 在 USB 重列舉後可靠執行清理。改用裝置重啟清理；後續整合必須另有可驗證的恢復機制。
- 重啟後已確認 functions 恢復 `diag,serial,rmnet,ffs`、audio_enable=0、無載入模組及音效卡，AT 狀態介面正常；本機實驗 ADB server 已停止。

## 音訊流與恢復複測（2026-09-12）

採用 `setsid` 脫離 ADB 工作階段後，12 秒限時 USB 列舉實驗成功自行恢復：裝置日誌記錄了 timer-complete、restore-start 和 restore-confirmed，functions 與 enable 的讀取值及 AT 查詢均正常。單次成功不能代替故障注入與長期穩定性測試。

隨後進行了 70 秒完整工作階段，順序為載入驅動、廠商校準、啟動 VoLTE hostless 路由、臨時新增 USB audio。到期停止本次建立的 helper 和校準程序，關閉 audio_enable，恢復原 USB functions。結果：

- Mac 從明確選取的 `AC Interface` 讀取 3 秒、8 kHz 單聲道音訊；分析器報告 24,576 個樣本，全部為零。未發生實際通話，這隻證明資料流可讀，不證明下行聲音正常。
- 獨立 CoreAudio 探針按名稱 `AS Interface` 和廠商 `BAIWANG` 唯一匹配輸出，再核對 AudioQueue 的裝置 UID，傳送 3 秒數字靜音，回傳成功。沒有輸出到系統預設喇叭。
- 裝置 D5 播放和 D6 擷取均進入 RUNNING，硬體指標推進。USB/PCM 傳輸路徑已得到比「音效卡出現」更強的證據。
- 70 秒後日志出現 cleanup-confirmed，D4 收發均為 closed、audio_enable=0、原 functions 恢復，AT 查詢正常。
- 同一次開機內再次建立 15 秒工作階段，Mac 同時向模組傳送數字靜音並讀取模組音訊，兩個行程均正常完成。仍然沒有使用電腦麥克風或儲存錄音。
- 第二次工作階段也自動 cleanup-confirmed，收發通道關閉，AT usbcfg 持久設定未改變。本次開機日誌未匹配到 BUG/Oops/panic/Unable to handle、ADSP_EFAILED 或 Voice_get_cal failed。最後重啟並複核無已載入模組、無音效卡、原 USB functions 和 audio_enable=0，結束實驗 ADB server。

正式功能的剩餘驗收：授權測試號碼的實際雙向通話、DTMF/掛斷、執行中插拔及故障恢復。不能將空閒全零音訊或 RUNNING 狀態作為真實通話成功的標準；在這些驗收通過前不自動安裝第三方執行檔案，也不宣稱軟體音訊功能已完成。

## 授權號碼撥號測試：未通過（2026-09-12）

向使用者提供的測試號碼發起一次撥號，設定最長 45 秒後自動掛斷。AT 報告 active，Mac 嚮明確選取的 BAIWANG 輸出傳送合成測試提示，同時僅對模組輸入進行 12 秒即時統計，不儲存錄音、不擷取電腦麥克風。下行約 96,768 個樣本出現非零訊號（RMS 約 -14 dBFS，峰值達到滿幅），但使用者明確反饋手機沒有收到來電，要求暫停換卡。

因此不能將本次 active 或非零音訊認定為對端接聽或雙向語音成功；可能是電信業者提示音，未做內容識別，原因未確認。自動掛斷已完成，AT 確認無通話。暫停進一步撥號與正式整合，等待換卡後的測試。

## 此前實機唯讀結果

裝置為 QDC507GLEFM21，核心為 3.18.44。使用者授權啟用 ADB 後的檢查發現：

- 存在廠商音訊庫 `libql_lib_audio.so.1` 和 `/dev/ttyGS0`。
- 存在 `/sys/class/android_usb/f_audio/audio_enable`，讀取值為 0。
- 存在 `alsaucm_test` 和 `/run/voc_svr`。
- `/proc/asound/cards` 無音效卡；`/proc/asound/pcm` 為空；`/dev/snd` 僅有 timer。

這些是當時的裝置快照，不保證裝置重新插拔後仍保持相同狀態。廠商庫和 USB 端點存在，不等於內部 PCM 音訊路徑已就緒。

## 公開實作溯源

MaVo 固定版本：`0443dfdaf8aec086fd76ba2ee9152fd908114524`。

本輪檢查了 CellDock main 的檔案樹、模組報告，以及以下兩個檔案的 SHA-256；它們與上述 MaVo 版本一致：

| 檔案 | SHA-256 |
| --- | --- |
| qdc507_aprv3.ko | `3d82d3dec4f1e323201bba87156df9d41438e08314097353f2607f9117211d4a` |
| qdc507_voice.ko | `ed3821682d5309969a01c764192c83feff9669c61ef237c69475cd1619cf296c` |

因此它們不是兩套獨立的驅動相容性證據。CellDock main 是可變引用，後續應重新核驗。

檢查到的兩個專案公開樹有使用者態 PCM bridge 原始碼，但未找到與這兩個核心模組對應的完整移植原始碼、補丁、設定和建置輸入。不能據此斷言作者未在其他位置釋出。

另一個重要缺口：公開 MODULE-REPORT 描述的是舊版 `qdc507_afe.ko`，不是目前打包的 `qdc507_voice.ko`。報告記載舊版初始化曾觸發 NULL dereference；修訂後的建置仍需實機驗證。不能將這份舊報告視為目前二進位的安全驗證。

## 自行建置的可行性邊界

公開基礎核心提交 `82ed00908b3e8efc3ff0de27d2b5a7c0524ecd7f` 包含 `mdm9607.c`、Q6 AFE、voice、hostless PCM 等基礎程式碼。

但其 Makefile 將部分音訊物件設為內建（`obj-y`），並未提供現成的 QDC507 APR/voice 模組建置目標。報告還提到了自訂初始化封裝、音效卡索引固定、裝置樹繫結和錯誤復原修改。僅編譯公開核心不能復現目前兩個模組。

下一步所需材料：

1. 與目前二進位版本一致的移植原始碼、補丁和 Makefile。
2. 實際核心設定、符號表及可復現工具鏈。
3. 離線建置、匯入符號和初始化/結束路徑審查。
4. 最後才是受控的裝置記憶體載入、非零 PCM 樣本和雙向通話驗證。

核心版本字串相同、符號名稱匹配，都不能單獨證明私有 ABI 或 DSP 路由相容。即使不刷寫韌體，載入不相容驅動也可能使模組崩潰。目前不應自動下載並載入這些檔案，也不應向使用者宣稱電腦通話音訊已可用。

## 來源

- [MaVo 固定版本執行檔案](https://github.com/moluncn/mavo/tree/0443dfdaf8aec086fd76ba2ee9152fd908114524/Resources/ModuleVoice)
- [CellDock 模組執行檔案](https://github.com/celldock/celldock-for-mac/tree/main/Resources/ModuleVoice)
- [CellDock 模組報告](https://github.com/celldock/celldock-for-mac/blob/main/docs/MODULE-REPORT.md)
- [CellDock 第三方說明](https://github.com/celldock/celldock-for-mac/blob/main/docs/THIRD_PARTY_NOTICES.md)
- [公開基礎核心](https://github.com/the-modem-distro/quectel_eg25_kernel/tree/82ed00908b3e8efc3ff0de27d2b5a7c0524ecd7f)
