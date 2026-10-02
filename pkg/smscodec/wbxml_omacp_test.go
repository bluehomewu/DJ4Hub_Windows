package smscodec

import (
	"encoding/hex"
	"strings"
	"testing"
)

// 構造一個標準的 OMA CP WBXML 測試用例（APN 設定）
// 等價 XML：
//
//	<wap-provisioningdoc>
//	  <characteristic type="NAPDEF">
//	    <parm name="NAPID" value="internet"/>
//	    <parm name="NAP-ADDRESS" value="internet.telekom"/>
//	    <parm name="BEARER" value="GSM-GPRS"/>
//	    <parm name="NAP-ADDRTYPE" value="APN"/>
//	  </characteristic>
//	  <characteristic type="APPLICATION">
//	    <parm name="APPID" value="w4"/>
//	    <parm name="NAME" value="Telekom"/>
//	    <parm name="TO-NAPID" value="internet"/>
//	  </characteristic>
//	</wap-provisioningdoc>
func buildTestWBXML() []byte {
	var buf []byte
	// WBXML Header
	buf = append(buf, 0x03) // 版本 1.3
	buf = append(buf, 0x0B) // 公共 ID: OMA CP
	buf = append(buf, 0x6A) // 字元集: UTF-8 (106)
	buf = append(buf, 0x00) // 字串表長度: 0

	// <wap-provisioningdoc> (tag 0x05, has content)
	buf = append(buf, 0x05|byte(wbxmlHasContent))

	// --- <characteristic type="NAPDEF"> ---
	// tag 0x06 (characteristic), has content + has attributes
	buf = append(buf, 0x06|byte(wbxmlHasContent)|byte(wbxmlHasAttrs))
	// type="NAPDEF" — 使用 ATTRSTART 0xA4 (type=NAPDEF)
	buf = append(buf, 0xA4)
	buf = append(buf, byte(wbxmlEnd)) // 結束屬性清單

	// <parm name="NAPID" value="internet"/> (tag 0x07, has attributes, no content)
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x11) // name="NAPID" (ATTRSTART 0x11)
	buf = append(buf, 0x06) // value="" (ATTRSTART 0x06, 需要 STR_I 提供值)
	buf = append(buf, byte(wbxmlStrI))
	buf = append(buf, []byte("internet")...)
	buf = append(buf, 0x00)           // null 終止符
	buf = append(buf, byte(wbxmlEnd)) // 結束屬性清單

	// <parm name="NAP-ADDRESS" value="internet.telekom"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x08) // name="NAP-ADDRESS"
	buf = append(buf, 0x06) // value=""
	buf = append(buf, byte(wbxmlStrI))
	buf = append(buf, []byte("internet.telekom")...)
	buf = append(buf, 0x00)
	buf = append(buf, byte(wbxmlEnd))

	// <parm name="BEARER" value="GSM-GPRS"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x10) // name="BEARER"
	buf = append(buf, 0x06) // value=""
	buf = append(buf, 0x73) // ATTRVALUE 0x73 = "GSM-GPRS"
	buf = append(buf, byte(wbxmlEnd))

	// <parm name="NAP-ADDRTYPE" value="APN"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x09) // name="NAP-ADDRTYPE"
	buf = append(buf, 0x06) // value=""
	buf = append(buf, 0x49) // ATTRVALUE 0x49 = "APN"
	buf = append(buf, byte(wbxmlEnd))

	buf = append(buf, byte(wbxmlEnd)) // 結束 characteristic NAPDEF

	// --- <characteristic type="APPLICATION"> ---
	// 切換到 attribute code page 1
	buf = append(buf, 0x06|byte(wbxmlHasContent)|byte(wbxmlHasAttrs))
	buf = append(buf, byte(wbxmlSwitchPage), 0x01) // 切換 attr code page 到 1
	buf = append(buf, 0xA4)                        // type="APPLICATION" (code page 1 的 0xA4)
	buf = append(buf, byte(wbxmlEnd))              // 結束屬性清單

	// <parm name="APPID" value="w4"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x1C) // name="APPID" (code page 1)
	buf = append(buf, 0x06) // value=""
	buf = append(buf, 0x91) // ATTRVALUE 0x91 = "w4" (code page 1)
	buf = append(buf, byte(wbxmlEnd))

	// <parm name="NAME" value="Telekom"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x07) // name="NAME" (code page 1)
	buf = append(buf, 0x06) // value=""
	buf = append(buf, byte(wbxmlStrI))
	buf = append(buf, []byte("Telekom")...)
	buf = append(buf, 0x00)
	buf = append(buf, byte(wbxmlEnd))

	// <parm name="TO-NAPID" value="internet"/>
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x11) // name="TO-NAPID" (code page 1)
	buf = append(buf, 0x06) // value=""
	buf = append(buf, byte(wbxmlStrI))
	buf = append(buf, []byte("internet")...)
	buf = append(buf, 0x00)
	buf = append(buf, byte(wbxmlEnd))

	buf = append(buf, byte(wbxmlEnd)) // 結束 characteristic APPLICATION

	buf = append(buf, byte(wbxmlEnd)) // 結束 wap-provisioningdoc

	return buf
}

func TestDecodeWBXMLBasic(t *testing.T) {
	data := buildTestWBXML()
	cfg, err := decodeWBXML(data)
	if err != nil {
		t.Fatalf("解碼失敗: %v", err)
	}
	if cfg == nil {
		t.Fatal("解碼結果為 nil")
	}

	t.Logf("版本: %s", cfg.Version)
	t.Logf("特徵項數量: %d", len(cfg.Characteristics))

	// 應該有一個頂層 wap-provisioningdoc，包含 2 個子 characteristic
	if len(cfg.Characteristics) == 0 {
		t.Fatal("沒有解碼到任何特徵項")
	}

	root := cfg.Characteristics[0]
	if root.Type != "wap-provisioningdoc" {
		t.Fatalf("根元素型別應為 wap-provisioningdoc，實際: %s", root.Type)
	}

	if len(root.Subs) < 2 {
		t.Fatalf("應有至少 2 個子特徵，實際: %d", len(root.Subs))
	}

	// 驗證 NAPDEF
	napdef := root.Subs[0]
	if napdef.Type != "NAPDEF" {
		t.Errorf("第一個子特徵應為 NAPDEF，實際: %s", napdef.Type)
	}
	if napdef.Params["NAPID"] != "internet" {
		t.Errorf("NAPID 應為 internet，實際: %q", napdef.Params["NAPID"])
	}
	if napdef.Params["NAP-ADDRESS"] != "internet.telekom" {
		t.Errorf("NAP-ADDRESS 應為 internet.telekom，實際: %q", napdef.Params["NAP-ADDRESS"])
	}
	if napdef.Params["BEARER"] != "GSM-GPRS" {
		t.Errorf("BEARER 應為 GSM-GPRS，實際: %q", napdef.Params["BEARER"])
	}
	if napdef.Params["NAP-ADDRTYPE"] != "APN" {
		t.Errorf("NAP-ADDRTYPE 應為 APN，實際: %q", napdef.Params["NAP-ADDRTYPE"])
	}

	// 驗證 APPLICATION
	app := root.Subs[1]
	if app.Type != "APPLICATION" {
		t.Errorf("第二個子特徵應為 APPLICATION，實際: %s", app.Type)
	}
	if app.Params["APPID"] != "w4" {
		t.Errorf("APPID 應為 w4，實際: %q", app.Params["APPID"])
	}
	if app.Params["NAME"] != "Telekom" {
		t.Errorf("NAME 應為 Telekom，實際: %q", app.Params["NAME"])
	}
}

func TestDecodeOmaCPFromTPDU_WithWSPHeader(t *testing.T) {
	// 模擬帶 WSP Push header 的資料：[header_len=1] [content_type=0xB0] [WBXML...]
	wbxml := buildTestWBXML()
	// WSP Push header: headers_len=1, content-type=0xB0 (application/vnd.wap.connectivity-wbxml)
	data := append([]byte{0x01, 0xB0}, wbxml...)

	cfg, err := DecodeOmaCPFromTPDU(data)
	if err != nil {
		t.Fatalf("帶 WSP header 的 OMA CP 解碼失敗: %v", err)
	}
	if cfg == nil {
		t.Fatal("解碼結果為 nil")
	}
	t.Logf("帶 WSP header 解碼成功，特徵項: %d", len(cfg.Characteristics))
}

func TestDecodeOmaCPFromTPDU_WithLongWSPHeader(t *testing.T) {
	wbxml := buildTestWBXML()
	// 模擬較長 WSP 頭，驗證 WBXML 起始定位不依賴前 16 位元組視窗。
	longWSPHeader := make([]byte, 24)
	for i := range longWSPHeader {
		longWSPHeader[i] = byte(i + 1)
	}
	data := append(longWSPHeader, wbxml...)

	cfg, err := DecodeOmaCPFromTPDU(data)
	if err != nil {
		t.Fatalf("長 WSP header 的 OMA CP 解碼失敗: %v", err)
	}
	if cfg == nil || len(cfg.Characteristics) == 0 {
		t.Fatal("解碼結果為空")
	}
}

func TestDecodeOmaCPFromTPDU_EncryptedFallback(t *testing.T) {
	// 模擬加密的 OMA CP 資料（不含有效 WBXML header）
	data, _ := hex.DecodeString("0048150e221515b00011a8e388e4673a650e13c5cc6d22e1e78eb152d2ef139c27513b6abe5b130adb9b43f8e78eb863e07c27b195cf4571505558a485d9c3dc0c68a5cea1c0b35845c5")
	_, err := DecodeOmaCPFromTPDU(data)
	if err == nil {
		t.Fatal("加密資料應回傳錯誤")
	}
	if !strings.Contains(err.Error(), "加密") {
		t.Errorf("錯誤資訊應提及加密，實際: %s", err.Error())
	}
	t.Logf("加密資料正確回傳錯誤: %v", err)
}

func TestFormatOmaCPSummary(t *testing.T) {
	data := buildTestWBXML()
	cfg, err := decodeWBXML(data)
	if err != nil {
		t.Fatalf("解碼失敗: %v", err)
	}

	summary := FormatOmaCPSummary(cfg)
	t.Logf("摘要輸出:\n%s", summary)

	// 驗證摘要中包含關鍵資訊
	checks := []string{
		"NAPDEF",
		"internet.telekom",
		"GSM-GPRS",
		"APN",
		"APPLICATION",
		"瀏覽器書籤", // w4 的中文名
		"Telekom",
	}
	for _, check := range checks {
		if !strings.Contains(summary, check) {
			t.Errorf("摘要中應包含 %q", check)
		}
	}
}

func TestIsWBXMLHeader(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		expect bool
	}{
		{"有效 OMA CP", []byte{0x03, 0x0B, 0x6A, 0x00}, true},
		{"太短", []byte{0x03}, false},
		{"版本錯誤", []byte{0x04, 0x0B, 0x6A, 0x00}, false},
		{"公共 ID 錯誤", []byte{0x03, 0x0C, 0x6A, 0x00}, false},
		{"全零", []byte{0x00, 0x00, 0x00, 0x00}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWBXMLHeader(tt.data)
			if got != tt.expect {
				t.Errorf("isWBXMLHeader(%X) = %v, 預期 %v", tt.data, got, tt.expect)
			}
		})
	}
}

func TestIsWBXMLHeader_MultiBytePublicID(t *testing.T) {
	// publicID 0x0B 的非最短 mb_uint32 編碼：0x80 0x0B
	data := []byte{0x03, 0x80, 0x0B, 0x6A, 0x00}
	if !isWBXMLHeader(data) {
		t.Fatalf("多位元組 publicID 編碼應被識別為合法 WBXML 頭: %X", data)
	}
}

func TestDecodeWBXML_AttrValueTokenAmbiguityAndEmptyParmName(t *testing.T) {
	var buf []byte
	// WBXML Header
	buf = append(buf, 0x03, 0x0B, 0x6A, 0x00)
	// <wap-provisioningdoc>
	buf = append(buf, 0x05|byte(wbxmlHasContent))
	// <characteristic type="NAPDEF">
	buf = append(buf, 0x06|byte(wbxmlHasContent)|byte(wbxmlHasAttrs))
	buf = append(buf, 0xA4, byte(wbxmlEnd))

	// <parm name="NAME" value="NAPDEF"/>，value token 0xA4 同時也可能被誤解釋為 ATTRSTART
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x07) // name="NAME"
	buf = append(buf, 0x06) // value=""
	buf = append(buf, 0xA4) // ATTRVALUE -> "NAPDEF"
	buf = append(buf, byte(wbxmlEnd))

	// <parm value="orphan"/>（無 name），應被忽略，不寫入空 key
	buf = append(buf, 0x07|byte(wbxmlHasAttrs))
	buf = append(buf, 0x06) // value=""
	buf = append(buf, byte(wbxmlStrI))
	buf = append(buf, []byte("orphan")...)
	buf = append(buf, 0x00)
	buf = append(buf, byte(wbxmlEnd))

	// 結束 characteristic / doc
	buf = append(buf, byte(wbxmlEnd), byte(wbxmlEnd))

	cfg, err := decodeWBXML(buf)
	if err != nil {
		t.Fatalf("解碼失敗: %v", err)
	}
	if len(cfg.Characteristics) == 0 || cfg.Characteristics[0].Type != "wap-provisioningdoc" || len(cfg.Characteristics[0].Subs) == 0 {
		t.Fatalf("解碼結構異常: %+v", cfg)
	}

	ch := cfg.Characteristics[0].Subs[0]
	if got := ch.Params["NAME"]; got != "NAPDEF" {
		t.Fatalf("NAME 引數解析錯誤: got=%q want=%q", got, "NAPDEF")
	}
	if _, ok := ch.Params[""]; ok {
		t.Fatalf("不應出現空引數名 key，params=%v", ch.Params)
	}
}
