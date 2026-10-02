// Package pki 提供 eUICC 晶片的 PKI 公開資料查詢能力
// 資料來源：https://euicc-manual.osmocom.org
// 使用 go:generate 更新內嵌的 JSON 字典：
//
//go:generate curl -sL -o ci.json https://euicc-manual.osmocom.org/docs/pki/ci/manifest.json
//go:generate curl -sL -o accredited.json https://euicc-manual.osmocom.org/docs/pki/eum/accredited.json
package pki

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/WongLoki/DJ4Hub/pkg/logger"
)

//go:embed ci.json
var ciData []byte

//go:embed accredited.json
var accreditedData []byte

// CertificateIssuer eSIM 證書籤發機構
type CertificateIssuer struct {
	KeyID   string `json:"key-id"`
	Country string `json:"country"`
	Name    string `json:"name"`
}

// Accredited 認證供應商字典
type Accredited struct {
	Version   uint8      `json:"version"`
	Suppliers []Supplier `json:"suppliers"`
}

// Supplier eUICC 晶片供應商
type Supplier struct {
	Name      string            `json:"name"`
	Abbr      string            `json:"abbr,omitempty"`
	Region    string            `json:"country"`
	EUM       []string          `json:"eum,omitempty"`
	Locations map[string]string `json:"locations"`
}

var (
	issuers []CertificateIssuer
	sites   Accredited
)

func init() {
	if err := json.Unmarshal(ciData, &issuers); err != nil {
		logger.Error("解析 CI 證書籤發機構資料失敗", "err", err)
	}
	if err := json.Unmarshal(accreditedData, &sites); err != nil {
		logger.Error("解析 Accredited 供應商資料失敗", "err", err)
	}
}

// LookupCertificateIssuer 根據 keyID（hex 字串）查詢證書籤發機構名稱
func LookupCertificateIssuer(keyID string) string {
	for _, ci := range issuers {
		if strings.HasPrefix(keyID, ci.KeyID) {
			return ci.Name
		}
	}
	return keyID
}

// LookupCertificateIssuers 從 EUICCInfo2 中的 euiccCiPKIdListForSigning 欄位批次查詢
// 入參是原始二進位 keyID 清單，回傳人類可讀的簽發機構名稱清單
func LookupCertificateIssuers(keyIDs [][]byte) []string {
	result := make([]string, 0, len(keyIDs))
	for _, kid := range keyIDs {
		result = append(result, LookupCertificateIssuer(hex.EncodeToString(kid)))
	}
	return result
}

// LookupManufacturer 根據 EID 前 8 位（EUM 字首）查詢晶片製造商名稱
// sasAccreditationNumber 可選，來自 EUICCInfo2 中的 sasAccreditationNumber 欄位
// 回傳格式如 "Kigen 🇬🇧" 或 "Thales 🇫🇷"
func LookupManufacturer(eid string, sasAccreditationNumber string) string {
	if len(eid) < 8 {
		return ""
	}
	eum := eid[:8]
	for _, supplier := range sites.Suppliers {
		if slices.Contains(supplier.EUM, eum) {
			flag := regionFlag(supplier.Region)
			fallback := fmt.Sprintf("%s %s", supplier.Name, flag)
			if len(sasAccreditationNumber) < 5 {
				return fallback
			}
			if value, ok := supplier.Locations[sasAccreditationNumber[:5]]; ok {
				return fmt.Sprintf("%s %s", supplier.Name, regionFlag(value))
			}
			return fallback
		}
	}
	return ""
}

// regionFlag 將兩字母國碼轉換為 emoji 國旗
func regionFlag(code string) string {
	if len(code) < 2 {
		return ""
	}
	return string(0x1F1E6+rune(code[0])-'A') + string(0x1F1E6+rune(code[1])-'A')
}
