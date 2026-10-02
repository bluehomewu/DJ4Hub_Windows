package mbim

import (
	"context"
	"fmt"
)

// UICCApplication 是 MS UICC Application List 裡的一個應用項。
type UICCApplication struct {
	Type uint32 // MbimUiccApplicationType(2=USIM,3=ISIM,...)
	AID  []byte // 完整應用標識(ApplicationId)
}

// QueryUICCApplicationList 透過 MS UICC Low Level Access 的 APPLICATION_LIST(CID 7)直讀
// 卡上應用列表(含完整 AID),無需手動開邏輯通道/選 EF_DIR。
func QueryUICCApplicationList(ctx context.Context, d *Device) ([]UICCApplication, error) {
	resp, err := d.Command(ctx, UUIDMSUICCLowLevelAccess, CIDUICCApplicationList, CommandTypeQuery, nil)
	if err != nil {
		return nil, err
	}
	if resp.Status != 0 {
		return nil, &StatusError{Op: "UICC_APPLICATION_LIST", Status: resp.Status}
	}
	return parseUICCApplicationList(resp.InfoBuffer)
}

// UICCFileResult 是直讀檔案(ReadBinary/ReadRecord)的結果:卡狀態字 + 資料。
type UICCFileResult struct {
	SW1  uint32
	SW2  uint32
	Data []byte
}

// encodeUICCReadBinary 按 libmbim Read Binary 的 query 佈局編碼(固定 44 位元組 + 變長區)。
// 欄位:Version, AppId(ref), FilePath(ref), ReadOffset, ReadSize, LocalPin(string,空), Data(ref,空)。
func encodeUICCReadBinary(aid, filePath []byte, readOffset, readSize uint32) []byte {
	const fixed = 44
	aidPad := pad4(len(aid))
	pathPad := pad4(len(filePath))
	info := make([]byte, fixed+aidPad+pathPad)
	le.PutUint32(info[0:], 1) // Version
	aidOff := fixed
	le.PutUint32(info[4:], uint32(aidOff))
	le.PutUint32(info[8:], uint32(len(aid)))
	copy(info[aidOff:], aid)
	pathOff := fixed + aidPad
	le.PutUint32(info[12:], uint32(pathOff))
	le.PutUint32(info[16:], uint32(len(filePath)))
	copy(info[pathOff:], filePath)
	le.PutUint32(info[20:], readOffset)
	le.PutUint32(info[24:], readSize)
	// LocalPin(28/32)與 Data(36/40)留空:offset=0,size=0。
	return info
}

// UICCReadBinary 透過 READ_BINARY(CID 9)直讀透明 EF:給出完整 AID 與檔案路徑,
// 模組內部完成選應用/選檔案,無需手動開邏輯通道。
func UICCReadBinary(ctx context.Context, d *Device, aid, filePath []byte, readOffset, readSize uint32) (UICCFileResult, error) {
	resp, err := d.Command(ctx, UUIDMSUICCLowLevelAccess, CIDUICCReadBinary, CommandTypeQuery, encodeUICCReadBinary(aid, filePath, readOffset, readSize))
	if err != nil {
		return UICCFileResult{}, err
	}
	if resp.Status != 0 {
		return UICCFileResult{}, &StatusError{Op: "UICC_READ_BINARY", Status: resp.Status}
	}
	return parseUICCFileResponse(resp.InfoBuffer)
}

// encodeUICCReadRecord 按 libmbim Read Record 的 query 佈局編碼(固定 40 位元組 + 變長區)。
// 欄位:Version, AppId(ref), FilePath(ref), RecordNumber, LocalPin(string,空), Data(ref,空)。
func encodeUICCReadRecord(aid, filePath []byte, recordNumber uint32) []byte {
	const fixed = 40
	aidPad := pad4(len(aid))
	pathPad := pad4(len(filePath))
	info := make([]byte, fixed+aidPad+pathPad)
	le.PutUint32(info[0:], 1) // Version
	aidOff := fixed
	le.PutUint32(info[4:], uint32(aidOff))
	le.PutUint32(info[8:], uint32(len(aid)))
	copy(info[aidOff:], aid)
	pathOff := fixed + aidPad
	le.PutUint32(info[12:], uint32(pathOff))
	le.PutUint32(info[16:], uint32(len(filePath)))
	copy(info[pathOff:], filePath)
	le.PutUint32(info[20:], recordNumber)
	// LocalPin(24/28)與 Data(32/36)留空。
	return info
}

// UICCReadRecord 透過 READ_RECORD(CID 10)直讀線性記錄 EF(如 EF_MSISDN)。
func UICCReadRecord(ctx context.Context, d *Device, aid, filePath []byte, recordNumber uint32) (UICCFileResult, error) {
	resp, err := d.Command(ctx, UUIDMSUICCLowLevelAccess, CIDUICCReadRecord, CommandTypeQuery, encodeUICCReadRecord(aid, filePath, recordNumber))
	if err != nil {
		return UICCFileResult{}, err
	}
	if resp.Status != 0 {
		return UICCFileResult{}, &StatusError{Op: "UICC_READ_RECORD", Status: resp.Status}
	}
	return parseUICCFileResponse(resp.InfoBuffer)
}

// parseUICCFileResponse 解析 ReadBinary/ReadRecord 應答:Version, SW1, SW2, Data(ref)。
func parseUICCFileResponse(info []byte) (UICCFileResult, error) {
	r := newInfoReader(info)
	sw1, err := r.u32At(4)
	if err != nil {
		return UICCFileResult{}, err
	}
	sw2, err := r.u32At(8)
	if err != nil {
		return UICCFileResult{}, err
	}
	data, err := r.byteArrayAt(12)
	if err != nil {
		return UICCFileResult{}, err
	}
	return UICCFileResult{SW1: sw1, SW2: sw2, Data: data}, nil
}

func parseUICCApplicationList(info []byte) ([]UICCApplication, error) {
	r := newInfoReader(info)
	count, err := r.u32At(4) // ApplicationCount
	if err != nil {
		return nil, err
	}
	const ptrStart = 16 // Version+Count+ActiveIndex+ListSizeBytes
	apps := make([]UICCApplication, 0, count)
	for i := uint32(0); i < count; i++ {
		off, err := r.u32At(ptrStart + int(i)*8)
		if err != nil {
			return nil, err
		}
		size, err := r.u32At(ptrStart + int(i)*8 + 4)
		if err != nil {
			return nil, err
		}
		if uint64(off)+uint64(size) > uint64(len(info)) {
			return nil, fmt.Errorf("mbim: UICC application %d struct out of range off=%d size=%d", i, off, size)
		}
		// 結構內欄位的 offset 相對結構起始,故對結構 blob 單獨建 reader。
		sr := newInfoReader(info[off : uint64(off)+uint64(size)])
		appType, err := sr.u32At(0)
		if err != nil {
			return nil, err
		}
		aid, err := sr.byteArrayAt(4) // ApplicationId: ref-byte-array(offset,size)
		if err != nil {
			return nil, err
		}
		apps = append(apps, UICCApplication{Type: appType, AID: aid})
	}
	return apps, nil
}
