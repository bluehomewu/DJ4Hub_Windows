//go:build windows

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

const (
	defaultListenAddress = "127.0.0.1:7575"
	controlTokenEnv      = "DJ4GHUB_CONTROL_TOKEN"
	controlTokenHeader   = "X-DJ4Hub-Control"
	maxLogBytes          = 5 << 20
)

// serviceState is written by `start` so later commands can find the
// background service. The token authorizes the local shutdown endpoint.
type serviceState struct {
	PID     int       `json:"pid"`
	URL     string    `json:"url"`
	Token   string    `json:"token"`
	Started time.Time `json:"started"`
}

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	command := "start"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	var err error
	switch command {
	case "start":
		err = startService(args)
	case "serve":
		log.SetOutput(os.Stdout)
		err = runServer(args)
	case "stop":
		err = stopService()
	case "status":
		err = printServiceStatus()
	case "logs":
		err = followLogs()
	case "open":
		err = openConsole()
	case "activate":
		err = activateDJINetwork(os.Stdout)
	case "audio-check":
		if _, _, err = moduleAudioRuntime(); err == nil {
			fmt.Println("本機音訊執行檔案及 ADB 已就緒；硬體相容性將在準備時檢查。")
		}
	case "audio-install":
		if len(args) != 1 {
			err = errors.New("用法：dj4ghub audio-install DIR")
		} else if err = installAudioRuntime(args[0]); err == nil {
			fmt.Println("音訊執行檔案已匯入；執行 dj4ghub audio-check 檢查 ADB。未連線裝置或載入驅動。")
		}
	case "version", "--version", "-v":
		fmt.Printf("DJ 4G Hub for Windows %s\n", appVersion)
	case "help", "--help", "-h", "/?":
		printUsage(os.Stdout)
	default:
		if strings.HasPrefix(command, "-") {
			// Accept the upstream flag style, e.g. `dj4ghub -demo`.
			log.SetOutput(os.Stdout)
			err = runServer(append([]string{command}, args...))
			break
		}
		printUsage(os.Stderr)
		err = fmt.Errorf("未知命令：%s", command)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "錯誤：%v\n", err)
		pauseIfOwnConsole(true)
		return 1
	}
	if command == "start" && len(args) == 0 {
		pauseIfOwnConsole(false)
	}
	return 0
}

func printUsage(out io.Writer) {
	fmt.Fprintf(out, `DJ 4G Hub for Windows %s

用法：
  dj4ghub start [--demo] [--port COMx] [--no-open] [--sms-cleanup]
                         在背景啟動並開啟管理網頁（直接雙擊 exe 等同 start）
  dj4ghub stop           停止背景服務
  dj4ghub status         檢視執行狀態
  dj4ghub logs           檢視即時日誌（Ctrl+C 退出）
  dj4ghub open           開啟管理網頁
  dj4ghub activate       檢查模組網卡並連線 Windows 行動寬頻後退出
  dj4ghub serve [--demo] [--port COMx] [--listen 127.0.0.1:7575]
                         在前臺執行服務（除錯用）
  dj4ghub version        顯示版本
`, appVersion)
}

func validateListenAddress(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("無效的監聽位址 %q: %w", listen, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Printf("警告：%s 不是本機迴環位址，區域網內其他裝置將能讀取簡訊並控制模組", listen)
	}
	return nil
}

func appDataDir() (string, error) {
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "DJ 4G Hub")
	return dir, os.MkdirAll(dir, 0700)
}

func statePath() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "service.json"), nil
}

func logPath() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dir, "Logs")
	return filepath.Join(logDir, "dj4ghub.log"), os.MkdirAll(logDir, 0700)
}

func readServiceState() (*serviceState, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state serviceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeServiceState(state serviceState) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func removeServiceState() {
	if path, err := statePath(); err == nil {
		_ = os.Remove(path)
	}
}

// runningService returns the recorded service when its process is alive and
// its HTTP endpoint answers.
func runningService() *serviceState {
	state, err := readServiceState()
	if err != nil || state.PID <= 0 || !processAlive(state.PID) {
		return nil
	}
	if !serviceHealthy(state.URL, 2*time.Second) {
		return nil
	}
	return state
}

func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

func serviceHealthy(baseURL string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	response, err := client.Get(strings.TrimRight(baseURL, "/") + "/api/health")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func startService(args []string) error {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	demo := flags.Bool("demo", false, "run without hardware")
	port := flags.String("port", "", "AT COM port")
	listen := flags.String("listen", defaultListenAddress, "HTTP listen address")
	noOpen := flags.Bool("no-open", false, "do not open the browser")
	smsCleanup := flags.Bool("sms-cleanup", false, "delete module SMS after archiving")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := validateListenAddress(*listen); err != nil {
		return err
	}

	if state := runningService(); state != nil {
		fmt.Printf("DJ 4G Hub 已在執行（PID %d）：%s\n", state.PID, state.URL)
		if !*noOpen {
			return openURL(state.URL)
		}
		return nil
	}
	removeServiceState()

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := openLogFile()
	if err != nil {
		return err
	}
	defer logFile.Close()

	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	serveArgs := []string{"serve", "--listen", *listen}
	if *demo {
		serveArgs = append(serveArgs, "--demo")
	}
	if *smsCleanup {
		serveArgs = append(serveArgs, "--sms-cleanup")
	}
	if strings.TrimSpace(*port) != "" {
		serveArgs = append(serveArgs, "--port", *port)
	}
	child := exec.Command(executable, serveArgs...)
	child.Env = append(os.Environ(), controlTokenEnv+"="+token)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := child.Start(); err != nil {
		return fmt.Errorf("啟動背景服務失敗: %w", err)
	}
	state := serviceState{
		PID:     child.Process.Pid,
		URL:     "http://" + browserHost(*listen),
		Token:   token,
		Started: time.Now(),
	}
	if err := writeServiceState(state); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	deadline := time.After(25 * time.Second)
	for !serviceHealthy(state.URL, time.Second) {
		select {
		case err := <-exited:
			removeServiceState()
			path, _ := logPath()
			return fmt.Errorf("背景服務啟動後立即退出（%v），請檢視日誌 %s", err, path)
		case <-deadline:
			return errors.New("背景服務在 25 秒內沒有響應，請執行 dj4ghub logs 檢視原因")
		case <-time.After(300 * time.Millisecond):
		}
	}
	_ = child.Process.Release()
	mode := ""
	if *demo {
		mode = "（示範模式）"
	}
	fmt.Printf("DJ 4G Hub %s 已在背景啟動%s：%s\n", appVersion, mode, state.URL)
	fmt.Println("停止服務：dj4ghub stop")
	if !*noOpen {
		return openURL(state.URL)
	}
	return nil
}

func browserHost(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func openLogFile() (*os.File, error) {
	path, err := logPath()
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

func stopService() error {
	state, err := readServiceState()
	if err != nil || state.PID <= 0 || !processAlive(state.PID) {
		removeServiceState()
		fmt.Println("DJ 4G Hub 未在執行")
		return nil
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(state.URL, "/")+"/api/control/shutdown", nil)
	if err != nil {
		return err
	}
	request.Header.Set(controlTokenHeader, state.Token)
	client := &http.Client{Timeout: 5 * time.Second}
	if response, err := client.Do(request); err == nil {
		response.Body.Close()
	}
	if waitForExit(state.PID, 10*time.Second) {
		removeServiceState()
		fmt.Println("DJ 4G Hub 已停止")
		return nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(state.PID))
	if err != nil {
		return fmt.Errorf("無法結束 PID %d: %w", state.PID, err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("無法結束 PID %d: %w", state.PID, err)
	}
	removeServiceState()
	fmt.Println("DJ 4G Hub 未在 10 秒內正常退出，已強制結束")
	return nil
}

func waitForExit(pid int, timeout time.Duration) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	return err == nil && event == windows.WAIT_OBJECT_0
}

func printServiceStatus() error {
	path, _ := logPath()
	if state := runningService(); state != nil {
		fmt.Printf("執行中：PID %d，%s（啟動於 %s）\n", state.PID, state.URL, state.Started.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Println("未執行")
	}
	fmt.Printf("日誌：%s\n", path)
	return nil
}

func openConsole() error {
	state := runningService()
	if state == nil {
		return errors.New("DJ 4G Hub 未在執行；請先執行 dj4ghub start")
	}
	return openURL(state.URL)
}

func openURL(target string) error {
	if os.Getenv("DJ4GHUB_NO_OPEN") == "1" {
		return nil
	}
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		fmt.Printf("請在瀏覽器中開啟：%s\n", target)
	}
	return nil
}

func followLogs() error {
	path, err := logPath()
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("尚無日誌：%s", path)
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 64*1024 {
		_, _ = file.Seek(-64*1024, io.SeekEnd)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	reader := bufio.NewReader(file)
	for ctx.Err() == nil {
		line, err := reader.ReadString('\n')
		if line != "" {
			fmt.Print(line)
		}
		if errors.Is(err, io.EOF) {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// pauseIfOwnConsole keeps the window open briefly when the exe was started by
// double-clicking it, so the user can read the result before it closes.
func pauseIfOwnConsole(waitForEnter bool) {
	processes := make([]uint32, 4)
	count, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	if count != 1 {
		return
	}
	if !waitForEnter {
		time.Sleep(4 * time.Second)
		return
	}
	fmt.Println()
	fmt.Println("按 Enter 關閉此視窗…")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
