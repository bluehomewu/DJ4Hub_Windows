package mbim

import (
	"context"
	"fmt"
)

// qmiSessionTLV 構造 QMI UIM Session Information TLV(0x01)：
// session_type + aid_len(1) + aid。
//
// session_type 的選擇規則（真機驗證，EM7430 QMI-over-MBIM 隧道）：
//   - aid 為 nil/空 → session_type=0x00 (Primary GW Provisioning)：
//     用於 MF 級別檔案（如 EF_DIR），此時必須在 file TLV 裡提供從 MF 到父級的路徑。
//   - aid 非空 → session_type=0x04 (Non-provisioning on slot 1)：
//     用於 ADF 子檔案（如 ADF_USIM 下的 EF_SPN/EF_AD 等）；
//     session_type=0x00 在 QMI-over-MBIM 隧道里不被 EM7430 支援——無論 AID
//     是否提供，都會被以 qmi_error=0x0030 (INVALID_ARGUMENT) 拒絕。
func qmiSessionTLV(aid []byte) []byte {
	sessionType := byte(0x00) // Primary GW Provisioning（MF 級檔案，path 指明位置）
	if len(aid) > 0 {
		sessionType = 0x04 // Non-provisioning on slot 1（ADF 子檔案，AID 指明應用）
	}
	val := append([]byte{sessionType, byte(len(aid))}, aid...)
	tlv := []byte{0x01, byte(len(val)), byte(len(val) >> 8)}
	return append(tlv, val...)
}

func qmiFileTLV(fileID uint16, path []byte) []byte {
	val := []byte{byte(fileID), byte(fileID >> 8), byte(len(path))}
	val = append(val, path...)
	tlv := []byte{0x02, byte(len(val)), byte(len(val) >> 8)}
	return append(tlv, val...)
}

func qmiOffsetLenTLV(a, b uint16) []byte {
	return []byte{0x03, 0x04, 0x00, byte(a), byte(a >> 8), byte(b), byte(b >> 8)}
}

func buildQMIReadTransparent(clientID uint8, txID uint16, fileID uint16, aid, path []byte, offset, length uint16) []byte {
	tlvs := append(qmiSessionTLV(aid), qmiFileTLV(fileID, path)...)
	tlvs = append(tlvs, qmiOffsetLenTLV(offset, length)...)
	return buildQMIMessage(0x0B, clientID, txID, 0x0020, tlvs)
}

func buildQMIReadRecord(clientID uint8, txID uint16, fileID uint16, aid, path []byte, record, length uint16) []byte {
	tlvs := append(qmiSessionTLV(aid), qmiFileTLV(fileID, path)...)
	tlvs = append(tlvs, qmiOffsetLenTLV(record, length)...)
	return buildQMIMessage(0x0B, clientID, txID, 0x0021, tlvs)
}

func parseQMIReadResult(frame []byte) (data []byte, sw1, sw2 byte, err error) {
	if len(frame) < 13 {
		return nil, 0, 0, fmt.Errorf("qmi read: 回應過短 %d", len(frame))
	}
	tlvs := frame[13:]
	have := false
	qmiFailed := false
	var qmiErrorCode uint16
	for idx := 0; idx+3 <= len(tlvs); {
		typ := tlvs[idx]
		l := int(le.Uint16(tlvs[idx+1 : idx+3]))
		if idx+3+l > len(tlvs) {
			break
		}
		val := tlvs[idx+3 : idx+3+l]
		switch typ {
		case 0x02:
			// QMI 標準強制 Result Code TLV：result(2,LE) + error(2,LE)。
			// result!=0 時模組只會回這個 TLV，不會帶 0x10/0x11。
			if len(val) >= 4 && le.Uint16(val[0:2]) != 0 {
				qmiFailed = true
				qmiErrorCode = le.Uint16(val[2:4])
			}
		case 0x10:
			if len(val) >= 2 {
				sw1, sw2, have = val[0], val[1], true
			}
		case 0x11:
			if len(val) >= 2 {
				n := int(le.Uint16(val[0:2]))
				if 2+n <= len(val) {
					data = val[2 : 2+n]
				}
			}
		}
		idx += 3 + l
	}
	if qmiFailed {
		return nil, 0, 0, fmt.Errorf("qmi read: 請求被模組拒絕，qmi_error=0x%04X", qmiErrorCode)
	}
	if !have && data == nil {
		return nil, 0, 0, fmt.Errorf("qmi read: 回應缺少 card_result/read_result TLV")
	}
	return data, sw1, sw2, nil
}

// QMIReadTransparentEF 讀取一個透明 EF。aid 應是目標檔案所屬應用(如 ADF_USIM)
// 的完整 AID；檔案直接掛在 MF 下(如 EF_DIR)時傳 nil。
func (d *Device) QMIReadTransparentEF(ctx context.Context, fileID uint16, aid, path []byte, offset, length uint16) ([]byte, byte, byte, error) {
	clientID, err := d.allocUIMClient(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	defer d.releaseUIMClient(context.Background(), clientID)
	resp, err := d.SendQMI(ctx, buildQMIReadTransparent(clientID, 2, fileID, aid, path, offset, length))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("qmi read_transparent EF %04X: %w", fileID, err)
	}
	return parseQMIReadResult(resp)
}

// QMIReadRecordEF 讀取一條線性記錄 EF。aid 含義同 QMIReadTransparentEF。
func (d *Device) QMIReadRecordEF(ctx context.Context, fileID uint16, aid, path []byte, record, length uint16) ([]byte, byte, byte, error) {
	clientID, err := d.allocUIMClient(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	defer d.releaseUIMClient(context.Background(), clientID)
	resp, err := d.SendQMI(ctx, buildQMIReadRecord(clientID, 2, fileID, aid, path, record, length))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("qmi read_record EF %04X rec %d: %w", fileID, record, err)
	}
	return parseQMIReadResult(resp)
}
