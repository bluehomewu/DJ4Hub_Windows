package modem

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/warthog618/sms/encoding/gsm7"
)

// DecodeEFSPN 解密並解碼來自 SIM 卡 EF_SPN (Elementary File - Service Provider Name) 的原始二進位資料。
// 該檔案結構可能包含 UCS2、壓縮 UCS2、ASCII 或 GSM 7-bit 格式的服務提供商名稱。
func DecodeEFSPN(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("EF_SPN data empty")
	}
	name := data
	if len(name) > 1 {
		name = name[1:] // 跳過第一個位元組（指示在 HPLMN/RPLMN 下的顯示要求）
	}
	name = trimSPNPadding(name)
	if len(name) == 0 {
		return "", fmt.Errorf("EF_SPN name empty")
	}

	var (
		decoded string
		err     error
	)
	switch name[0] {
	case 0x80:
		// 0x80 表示標準的 16-bit UCS2 編碼方式
		decoded, err = decodeSPNUCS2(name[1:])
	case 0x81:
		// 0x81 表示帶 1 位元組基準的壓縮 UCS2 編碼方式
		decoded, err = decodeSPNCompressedUCS2(name, 1)
	case 0x82:
		// 0x82 表示帶 2 位元組基準的壓縮 UCS2 編碼方式
		decoded, err = decodeSPNCompressedUCS2(name, 2)
	default:
		// 預設檢測，若是可列印 ASCII 字符集則直接轉換，否則走 GSM 7-bit 編碼解析
		if isPrintableASCII(name) {
			decoded = string(name)
		} else {
			decoded, err = decodeSPNGSM(name)
		}
	}
	if err != nil {
		return "", err
	}
	decoded = strings.TrimSpace(strings.ReplaceAll(decoded, "\x00", ""))
	if decoded == "" {
		return "", fmt.Errorf("EF_SPN name empty")
	}
	return decoded, nil
}

// trimSPNPadding 去除 SIM 卡記錄中填充的無效尾部字元（如常用的 0xFF 和 0x00）
func trimSPNPadding(data []byte) []byte {
	end := len(data)
	for end > 0 && (data[end-1] == 0xFF || data[end-1] == 0x00) {
		end--
	}
	return data[:end]
}

// decodeSPNUCS2 解碼標準的 Big-Endian 16-bit UCS2 文字
func decodeSPNUCS2(data []byte) (string, error) {
	data = trimSPNPadding(data)
	if len(data) == 0 {
		return "", fmt.Errorf("UCS2 SPN empty")
	}
	if len(data)%2 != 0 {
		return "", fmt.Errorf("UCS2 SPN odd length: %d", len(data))
	}
	runes := make([]uint16, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		runes = append(runes, binary.BigEndian.Uint16(data[i:i+2]))
	}
	return string(utf16.Decode(runes)), nil
}

// decodeSPNCompressedUCS2 根據基準頁解壓並解碼 8-bit 指標壓縮的 UCS2 資料（符合 TS 31.101 技術規範）
func decodeSPNCompressedUCS2(data []byte, baseBytes int) (string, error) {
	if len(data) < 2+baseBytes {
		return "", fmt.Errorf("compressed UCS2 SPN too short")
	}
	count := int(data[1]) // 第 2 個位元組記錄待解碼的字元數量
	payloadStart := 2 + baseBytes
	if len(data) < payloadStart {
		return "", fmt.Errorf("compressed UCS2 SPN missing payload")
	}
	payload := data[payloadStart:]
	if count < len(payload) {
		payload = payload[:count]
	}

	var base uint16
	if baseBytes == 1 {
		base = uint16(data[2]) << 7
	} else {
		base = binary.BigEndian.Uint16(data[2:4])
	}

	var b strings.Builder
	for _, c := range payload {
		if c < 0x80 {
			// 小於 0x80 的直接作為標準的 GSM 7-bit 字元編碼解析
			decoded, err := decodeSPNGSM([]byte{c})
			if err != nil {
				return "", err
			}
			b.WriteString(decoded)
			continue
		}
		// 大於等於 0x80 的透過基準偏移還原為雙位元組 UCS2 字元
		b.WriteRune(rune(base + uint16(c&0x7F)))
	}
	return b.String(), nil
}

// decodeSPNGSM 解碼 GSM 7-bit 預設字符集編碼的文字並驗證生成的 UTF-8 是否合法
func decodeSPNGSM(data []byte) (string, error) {
	decoded, err := gsm7.Decode(data)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", fmt.Errorf("GSM SPN decoded invalid UTF-8")
	}
	return string(decoded), nil
}

// isPrintableASCII 檢查資料是否全是可列印的 ASCII 字元 (0x20 到 0x7E)
func isPrintableASCII(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		if b < 0x20 || b > 0x7E {
			return false
		}
	}
	return true
}
