package modem

import "strings"

var servingOperatorNameByPLMN = map[string]string{
	// 中國大陸
	"46000": "中國移動",
	"46002": "中國移動",
	"46004": "中國移動",
	"46007": "中國移動",
	"46008": "中國移動",
	"46013": "中國移動",
	"46001": "中國聯通",
	"46006": "中國聯通",
	"46009": "中國聯通",
	"46003": "中國電信",
	"46005": "中國電信",
	"46011": "中國電信",
	"46015": "中國廣電",

	// 中國香港
	"45400": "CSL",
	"45402": "CSL",
	"45410": "CSL",
	"45418": "CSL",
	"45403": "電訊盈科",
	"45416": "電訊盈科",
	"45419": "電訊盈科",
	"45404": "3 HK",
	"45406": "數碼通",
	"45415": "數碼通",
	"45407": "中國移動香港",
	"45412": "中國聯通香港",

	// 台灣
	"46601": "遠傳電信",
	"46602": "遠傳電信",
	"46605": "亞太電信",
	"46689": "台灣之星",
	"46692": "中華電信",
	"46693": "台灣大哥大",
	"46697": "台灣大哥大",
	"46699": "台灣大哥大",
}

func normalizeOperatorCode(code string) string {
	code = strings.TrimSpace(code)
	return strings.Trim(code, "\"")
}

// LookupServingOperatorNameFromPLMN returns the mapped serving-network display name when the PLMN is known.
func LookupServingOperatorNameFromPLMN(plmn string) (string, bool) {
	plmn = normalizeOperatorCode(plmn)
	if plmn == "" {
		return "", false
	}
	name, ok := servingOperatorNameByPLMN[plmn]
	return name, ok
}

// ResolveServingOperatorNameFromPLMN returns a serving-network display name when known, otherwise the normalized raw PLMN.
func ResolveServingOperatorNameFromPLMN(plmn string) string {
	plmn = normalizeOperatorCode(plmn)
	if plmn == "" {
		return ""
	}
	if name, ok := LookupServingOperatorNameFromPLMN(plmn); ok {
		return name
	}
	return plmn
}
