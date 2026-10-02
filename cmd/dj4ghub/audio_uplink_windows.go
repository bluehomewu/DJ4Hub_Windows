//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Chrome on Windows cannot open the module's 8 kHz mono render endpoint and
// silently falls back to the default speaker, so the computer-to-module
// direction is bridged natively with WASAPI instead of in the browser.

var (
	modOle32             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
	procPropVariantClear = modOle32.NewProc("PropVariantClear")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = windows.GUID{Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	iidIAudioCaptureClient  = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	pkeyDeviceFriendlyName  = propertyKey{fmtid: windows.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, pid: 14}
)

const (
	eRender                 = 0
	eCapture                = 1
	eConsole                = 0
	eCommunications         = 2
	deviceStateActive       = 1
	clsctxAll               = 23
	bufferDuration100ns     = 1_000_000 // 100 ms
	audclntBufferFlagSilent = 0x2
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

type propVariant struct {
	vt       uint16
	reserved [6]byte
	val      unsafe.Pointer
	pad      uintptr
}

// comObject is a raw COM interface pointer owned by Windows.
type comObject unsafe.Pointer

func comCall(o comObject, method int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(o)
	fn := *(*uintptr)(unsafe.Add(vtable, method*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(o)}, args...)...)
	return r
}

func comRelease(o comObject) {
	if o != nil {
		comCall(o, 2)
	}
}

func hresult(name string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s 失敗（HRESULT 0x%08X）", name, uint32(hr))
	}
	return nil
}

func endpointName(device comObject) string {
	var store comObject
	if int32(comCall(device, 4, 0, uintptr(unsafe.Pointer(&store)))) < 0 {
		return ""
	}
	defer comRelease(store)
	var value propVariant
	if int32(comCall(store, 5, uintptr(unsafe.Pointer(&pkeyDeviceFriendlyName)), uintptr(unsafe.Pointer(&value)))) < 0 {
		return ""
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&value)))
	if value.vt != 31 || value.val == nil { // VT_LPWSTR
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(value.val))
}

// findEndpoint returns an active endpoint. A named role matches the
// friendly name; the default roles use Windows' current defaults.
func findEndpoint(enumerator comObject, flow int, name string, role endpointRole) (comObject, string, error) {
	if role != roleNamed {
		windowsRole := eConsole
		if role == roleCommunications {
			windowsRole = eCommunications
		}
		var device comObject
		if err := hresult("讀取預設音訊裝置", comCall(enumerator, 4, uintptr(flow), uintptr(windowsRole), uintptr(unsafe.Pointer(&device)))); err != nil {
			return nil, "", err
		}
		return device, endpointName(device), nil
	}
	var collection comObject
	if err := hresult("列舉音訊裝置", comCall(enumerator, 3, uintptr(flow), deviceStateActive, uintptr(unsafe.Pointer(&collection)))); err != nil {
		return nil, "", err
	}
	defer comRelease(collection)
	var count uint32
	comCall(collection, 3, uintptr(unsafe.Pointer(&count)))
	for i := uint32(0); i < count; i++ {
		var device comObject
		if int32(comCall(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&device)))) < 0 {
			continue
		}
		if found := endpointName(device); found == name {
			return device, found, nil
		}
		comRelease(device)
	}
	return nil, "", fmt.Errorf("找不到音訊裝置「%s」", name)
}

// findModuleRender returns the module's render endpoint, which must be unique.
func findModuleRender(enumerator comObject) (comObject, string, error) {
	var collection comObject
	if err := hresult("列舉音訊裝置", comCall(enumerator, 3, eRender, deviceStateActive, uintptr(unsafe.Pointer(&collection)))); err != nil {
		return nil, "", err
	}
	defer comRelease(collection)
	var count uint32
	comCall(collection, 3, uintptr(unsafe.Pointer(&count)))
	var match comObject
	var matchName string
	for i := uint32(0); i < count; i++ {
		var device comObject
		if int32(comCall(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&device)))) < 0 {
			continue
		}
		if name := endpointName(device); isModuleAudioEndpoint(name) {
			if match != nil {
				comRelease(device)
				comRelease(match)
				return nil, "", errors.New("偵測到多個模組音效卡，請只連線一台模組")
			}
			match, matchName = device, name
			continue
		}
		comRelease(device)
	}
	if match == nil {
		return nil, "", errors.New("Windows 尚未出現模組音效卡（AC Interface）")
	}
	return match, matchName, nil
}

type wasapiStream struct {
	client  comObject
	service comObject
	format  sampleFormat
	frames  uint32
}

func (s *wasapiStream) close() {
	if s.client != nil {
		comCall(s.client, 11) // Stop
	}
	comRelease(s.service)
	comRelease(s.client)
}

func openStream(device comObject, serviceIID *windows.GUID) (*wasapiStream, error) {
	stream := &wasapiStream{}
	if err := hresult("啟用音訊用戶端", comCall(device, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&stream.client)))); err != nil {
		return nil, err
	}
	var mix unsafe.Pointer
	if err := hresult("讀取裝置格式", comCall(stream.client, 8, uintptr(unsafe.Pointer(&mix)))); err != nil {
		stream.close()
		return nil, err
	}
	defer windows.CoTaskMemFree(mix)
	size := 18 + int(*(*uint16)(unsafe.Add(mix, 16)))
	format, ok := parseWaveFormat(unsafe.Slice((*byte)(mix), size))
	if !ok {
		stream.close()
		return nil, errors.New("不支援的裝置音訊格式")
	}
	stream.format = format
	if err := hresult("初始化音訊串流", comCall(stream.client, 3, 0, 0, bufferDuration100ns, 0, uintptr(mix), 0)); err != nil {
		stream.close()
		return nil, err
	}
	comCall(stream.client, 4, uintptr(unsafe.Pointer(&stream.frames)))
	if err := hresult("取得音訊服務", comCall(stream.client, 14, uintptr(unsafe.Pointer(serviceIID)), uintptr(unsafe.Pointer(&stream.service)))); err != nil {
		stream.close()
		return nil, err
	}
	return stream, nil
}

// audioUplink streams one computer microphone into the module's render
// endpoint until stopped.
type audioUplink struct {
	Microphone string
	Module     string
	muted      atomic.Bool
	stop       chan struct{}
	done       chan struct{}
	once       sync.Once
}

func (u *audioUplink) SetMuted(muted bool) { u.muted.Store(muted) }

func (u *audioUplink) Stop() {
	if u == nil {
		return
	}
	u.once.Do(func() { close(u.stop) })
	<-u.done
}

func (u *audioUplink) Running() bool {
	if u == nil {
		return false
	}
	select {
	case <-u.done:
		return false
	default:
		return true
	}
}

// startAudioUplink opens both endpoints on a dedicated COM thread and
// returns once streaming started or failed.
func startAudioUplink(microphoneLabel string) (*audioUplink, error) {
	uplink := &audioUplink{stop: make(chan struct{}), done: make(chan struct{})}
	ready := make(chan error, 1)
	go uplink.run(microphoneLabel, ready)
	if err := <-ready; err != nil {
		<-uplink.done
		return nil, err
	}
	return uplink, nil
}

func (u *audioUplink) run(microphoneLabel string, ready chan<- error) {
	defer close(u.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		ready <- fmt.Errorf("初始化 COM 失敗：%w", err)
		return
	}
	defer windows.CoUninitialize()

	var enumerator comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)))
	if err := hresult("建立音訊裝置列舉器", hr); err != nil {
		ready <- err
		return
	}
	defer comRelease(enumerator)

	name, role := normalizeBrowserDeviceLabel(microphoneLabel)
	micDevice, micName, err := findEndpoint(enumerator, eCapture, name, role)
	if err != nil {
		ready <- err
		return
	}
	defer comRelease(micDevice)
	if isModuleAudioEndpoint(micName) {
		ready <- errors.New("麥克風選到了模組音效卡；請在「電腦裝置」選擇電腦的麥克風")
		return
	}
	moduleDevice, moduleName, err := findModuleRender(enumerator)
	if err != nil {
		ready <- err
		return
	}
	defer comRelease(moduleDevice)

	capture, err := openStream(micDevice, &iidIAudioCaptureClient)
	if err != nil {
		ready <- fmt.Errorf("開啟麥克風「%s」：%w", micName, err)
		return
	}
	defer capture.close()
	render, err := openStream(moduleDevice, &iidIAudioRenderClient)
	if err != nil {
		ready <- fmt.Errorf("開啟模組音效卡：%w", err)
		return
	}
	defer render.close()
	if err := hresult("啟動麥克風", comCall(capture.client, 10)); err != nil {
		ready <- err
		return
	}
	if err := hresult("啟動模組音效卡", comCall(render.client, 10)); err != nil {
		ready <- err
		return
	}
	u.Microphone, u.Module = micName, moduleName
	ready <- nil
	u.pump(capture, render)
}

// pump moves captured audio to the module every 10 ms, keeping at most
// 300 ms queued so clock drift between the two devices cannot build up.
func (u *audioUplink) pump(capture, render *wasapiStream) {
	converter := &rateConverter{in: capture.format.Rate, out: render.format.Rate}
	maxQueue := render.format.Rate * 3 / 10
	keepQueue := render.format.Rate / 10
	var mono, queue []float32
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-u.stop:
			return
		case <-ticker.C:
		}
		for {
			var packet uint32
			if int32(comCall(capture.service, 5, uintptr(unsafe.Pointer(&packet)))) < 0 || packet == 0 {
				break
			}
			var data unsafe.Pointer
			var frames, flags uint32
			if int32(comCall(capture.service, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0)) < 0 {
				break
			}
			mono = mono[:0]
			if flags&audclntBufferFlagSilent != 0 || u.muted.Load() {
				for range frames {
					mono = append(mono, 0)
				}
			} else {
				size := int(frames) * capture.format.Channels * capture.format.BytesPerSample
				mono = decodeMono(capture.format, unsafe.Slice((*byte)(data), size), int(frames), mono)
			}
			comCall(capture.service, 4, uintptr(frames))
			queue = converter.convert(mono, queue)
		}
		if len(queue) > maxQueue {
			queue = append(queue[:0], queue[len(queue)-keepQueue:]...)
		}
		var padding uint32
		if int32(comCall(render.client, 6, uintptr(unsafe.Pointer(&padding)))) < 0 {
			continue
		}
		writable := int(render.frames - padding)
		if writable > len(queue) {
			writable = len(queue)
		}
		if writable <= 0 {
			continue
		}
		var buffer unsafe.Pointer
		if int32(comCall(render.service, 3, uintptr(writable), uintptr(unsafe.Pointer(&buffer)))) < 0 {
			continue
		}
		size := writable * render.format.Channels * render.format.BytesPerSample
		encodeFrames(render.format, queue[:writable], unsafe.Slice((*byte)(buffer), size))
		comCall(render.service, 4, uintptr(writable), 0)
		queue = append(queue[:0], queue[writable:]...)
	}
}
