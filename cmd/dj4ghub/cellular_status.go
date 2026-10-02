package main

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/modem"
)

// cellularStatus is a read-only snapshot of SIM, IMS/VoLTE and serving-cell
// details. Every field comes from query commands; nothing is written.
type cellularStatus struct {
	SampledAtMS int64             `json:"sampled_at_ms"`
	IMS         imsStatus         `json:"ims"`
	SIM         simDetails        `json:"sim"`
	Network     networkDetails    `json:"network"`
	Errors      map[string]string `json:"errors,omitempty"`
}

type imsStatus struct {
	Enabled       *bool  `json:"enabled"`
	Registered    *bool  `json:"registered"`
	VoLTEDisabled *bool  `json:"volte_disabled"`
	PDNCID        int    `json:"pdn_cid,omitempty"`
	PDNActive     *bool  `json:"pdn_active"`
	PDNAddress    string `json:"pdn_address,omitempty"`
	MBN           string `json:"mbn,omitempty"`
	State         string `json:"state"`
	Summary       string `json:"summary"`
}

type simDetails struct {
	PIN          string `json:"pin,omitempty"`
	Inserted     *bool  `json:"inserted"`
	InitStatus   int    `json:"init_status"`
	InitText     string `json:"init_text,omitempty"`
	ICCID        string `json:"iccid,omitempty"`
	IMSI         string `json:"imsi,omitempty"`
	HomePLMN     string `json:"home_plmn,omitempty"`
	HomeOperator string `json:"home_operator,omitempty"`
	SMSC         string `json:"smsc,omitempty"`
	PINRemaining *int   `json:"pin_remaining,omitempty"`
	PUKRemaining *int   `json:"puk_remaining,omitempty"`
}

type networkDetails struct {
	RegStatus    int      `json:"reg_status"`
	Registration string   `json:"registration"`
	Roaming      bool     `json:"roaming"`
	PSAttached   *bool    `json:"ps_attached"`
	Operator     string   `json:"operator,omitempty"`
	PLMN         string   `json:"plmn,omitempty"`
	ConnState    string   `json:"conn_state,omitempty"`
	RAT          string   `json:"rat,omitempty"`
	Duplex       string   `json:"duplex,omitempty"`
	Band         int      `json:"band,omitempty"`
	EARFCN       int      `json:"earfcn,omitempty"`
	BandwidthMHz float64  `json:"bandwidth_mhz,omitempty"`
	TAC          string   `json:"tac,omitempty"`
	CellID       string   `json:"cell_id,omitempty"`
	ENodeB       int      `json:"enodeb,omitempty"`
	Sector       int      `json:"sector,omitempty"`
	PCI          *int     `json:"pci,omitempty"`
	RSRP         *int     `json:"rsrp,omitempty"`
	RSRQ         *int     `json:"rsrq,omitempty"`
	RSSI         *int     `json:"rssi,omitempty"`
	SINR         *float64 `json:"sinr,omitempty"`
}

var (
	imsConfigPattern    = regexp.MustCompile(`\+QCFG:\s*"ims",(\d+)(?:,(\d+))?`)
	volteDisablePattern = regexp.MustCompile(`\+QCFG:\s*"volte[/_]disable",(\d+)`)
	mbnSelectPattern    = regexp.MustCompile(`\+QMBNCFG:\s*"Select",\s*"?([^"\r\n]+?)"?\s*$`)
	simStatPattern      = regexp.MustCompile(`\+QSIMSTAT:\s*\d+,(\d+)`)
	iniStatPattern      = regexp.MustCompile(`\+QINISTAT:\s*(\d+)`)
	cscaPattern         = regexp.MustCompile(`\+CSCA:\s*"([^"]*)"`)
	cgattPattern        = regexp.MustCompile(`\+CGATT:\s*(\d)`)
	qspnPattern         = regexp.MustCompile(`\+QSPN:\s*"([^"]*)","([^"]*)","[^"]*",\d+,"(\d+)"`)
	cpinPattern         = regexp.MustCompile(`\+CPIN:\s*([A-Z ]+)`)
)

// lteBandwidthMHz maps the Quectel LTE bandwidth code (0..5).
var lteBandwidthMHz = []float64{1.4, 3, 5, 10, 15, 20}

func (a *app) cellularStatus(w http.ResponseWriter, _ *http.Request) {
	if a.demo {
		writeJSON(w, http.StatusOK, demoCellularStatus())
		return
	}
	errs := map[string]string{}
	run := func(key, command string) string {
		response, err := a.runATCommand(command, 5*time.Second)
		if err != nil {
			errs[key] = err.Error()
			return ""
		}
		return response
	}
	status := cellularStatus{SampledAtMS: time.Now().UnixMilli()}

	status.IMS = parseIMSStatus(
		run("ims", `AT+QCFG="ims"`),
		run("volte", `AT+QCFG="volte_disable"`),
		run("mbn", `AT+QMBNCFG="select"`),
		run("cgdcont", "AT+CGDCONT?"),
		run("cgact", "AT+CGACT?"),
	)
	if status.IMS.PDNCID > 0 && status.IMS.PDNActive != nil && *status.IMS.PDNActive {
		status.IMS.PDNAddress = firstPDPAddress(run("ims_addr", "AT+CGPADDR="+strconv.Itoa(status.IMS.PDNCID)))
	}

	status.SIM = parseSIMDetails(
		run("cpin", "AT+CPIN?"),
		run("simstat", "AT+QSIMSTAT?"),
		run("inistat", "AT+QINISTAT"),
		parseUSBATPrefixed(run("iccid", "AT+QCCID"), "+QCCID:"),
		parseUSBATIMSI(run("imsi", "AT+CIMI")),
		run("csca", "AT+CSCA?"),
	)
	status.SIM.PINRemaining, status.SIM.PUKRemaining = parsePINCounters(run("pinc", `AT+QPINC="SC"`))

	status.Network = parseNetworkDetails(
		run("cereg", "AT+CEREG?"),
		run("creg", "AT+CREG?"),
		run("cgatt", "AT+CGATT?"),
		run("qspn", "AT+QSPN"),
		run("qeng", `AT+QENG="servingcell"`),
	)
	if len(errs) > 0 {
		status.Errors = errs
	}
	writeJSON(w, http.StatusOK, status)
}

func boolPtr(value bool) *bool { return &value }

func parseIMSStatus(imsResp, volteResp, mbnResp, cgdcontResp, cgactResp string) imsStatus {
	status := imsStatus{}
	if m := imsConfigPattern.FindStringSubmatch(imsResp); m != nil {
		status.Enabled = boolPtr(m[1] == "1")
		if m[2] != "" {
			status.Registered = boolPtr(m[2] == "1")
		}
	}
	if m := volteDisablePattern.FindStringSubmatch(volteResp); m != nil {
		status.VoLTEDisabled = boolPtr(m[1] == "1")
	}
	for _, line := range splitATLines(mbnResp) {
		if m := mbnSelectPattern.FindStringSubmatch(line); m != nil {
			status.MBN = strings.TrimSpace(m[1])
		}
	}
	for _, context := range parsePDPContexts(cgdcontResp) {
		if strings.EqualFold(context.APN, "ims") {
			status.PDNCID = context.ID
			break
		}
	}
	if status.PDNCID > 0 && cgactResp != "" {
		active := false
		for _, id := range parseActivePDPContexts(cgactResp) {
			if id == status.PDNCID {
				active = true
			}
		}
		status.PDNActive = boolPtr(active)
	}

	switch {
	case status.Enabled == nil:
		status.State, status.Summary = "unknown", "無法讀取 IMS 狀態"
	case !*status.Enabled:
		status.State, status.Summary = "disabled", "IMS 未啟用，無法使用 VoLTE 通話"
	case status.VoLTEDisabled != nil && *status.VoLTEDisabled:
		status.State, status.Summary = "disabled", "VoLTE 已被模組設定停用"
	case status.Registered != nil && *status.Registered:
		status.State, status.Summary = "ready", "VoLTE 可用：IMS 已註冊"
	default:
		status.State, status.Summary = "unregistered", "IMS 已啟用但尚未註冊；SIM、電信業者或漫遊可能不支援 VoLTE"
	}
	return status
}

func parseSIMDetails(cpinResp, simStatResp, iniStatResp, iccid, imsi, cscaResp string) simDetails {
	sim := simDetails{ICCID: strings.TrimSpace(iccid), IMSI: strings.TrimSpace(imsi)}
	if m := cpinPattern.FindStringSubmatch(cpinResp); m != nil {
		sim.PIN = strings.TrimSpace(m[1])
	}
	if m := simStatPattern.FindStringSubmatch(simStatResp); m != nil {
		sim.Inserted = boolPtr(m[1] == "1")
	}
	if m := iniStatPattern.FindStringSubmatch(iniStatResp); m != nil {
		sim.InitStatus, _ = strconv.Atoi(m[1])
		sim.InitText = simInitText(sim.InitStatus)
	}
	if m := cscaPattern.FindStringSubmatch(cscaResp); m != nil {
		sim.SMSC = decodeSMSCAddress(m[1])
	}
	sim.HomePLMN, sim.HomeOperator = homeNetworkFromIMSI(sim.IMSI)
	return sim
}

// simInitText explains AT+QINISTAT bits: 1 PIN ready, 2 SMS, 4 phonebook.
func simInitText(status int) string {
	switch {
	case status&7 == 7:
		return "初始化完成"
	case status&1 == 0:
		return "等待 SIM 解鎖或初始化"
	default:
		var pending []string
		if status&2 == 0 {
			pending = append(pending, "簡訊")
		}
		if status&4 == 0 {
			pending = append(pending, "電話簿")
		}
		return "正在初始化" + strings.Join(pending, "、")
	}
}

// decodeSMSCAddress returns a dialable number. Some modules report the
// address as UCS2 hex when the TE character set is UCS2.
func decodeSMSCAddress(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 8 && len(value)%4 == 0 && strings.HasPrefix(value, "00") {
		var out strings.Builder
		for i := 0; i+4 <= len(value); i += 4 {
			code, err := strconv.ParseUint(value[i:i+4], 16, 16)
			if err != nil {
				return value
			}
			out.WriteRune(rune(code))
		}
		return out.String()
	}
	return value
}

// homeNetworkFromIMSI derives the issuing network. MNC length is not stored
// in the IMSI, so a known three-digit PLMN is preferred over two digits.
func homeNetworkFromIMSI(imsi string) (string, string) {
	if len(imsi) < 6 {
		return "", ""
	}
	for _, length := range []int{6, 5} {
		if name, ok := modem.LookupServingOperatorNameFromPLMN(imsi[:length]); ok {
			return imsi[:3] + "-" + imsi[3:length], name
		}
	}
	mncLength := 2
	if imsi[0] == '3' { // North American PLMNs use three-digit MNCs.
		mncLength = 3
	}
	return imsi[:3] + "-" + imsi[3:3+mncLength], ""
}

func parseNetworkDetails(ceregResp, cregResp, cgattResp, qspnResp, qengResp string) networkDetails {
	net := networkDetails{RegStatus: firstNonZeroRegistration(ceregResp, cregResp)}
	net.Registration = registrationText(net.RegStatus)
	net.Roaming = net.RegStatus == 5
	if m := cgattPattern.FindStringSubmatch(cgattResp); m != nil {
		net.PSAttached = boolPtr(m[1] == "1")
	}
	if m := qspnPattern.FindStringSubmatch(qspnResp); m != nil {
		net.Operator = m[1]
		if net.Operator == "" {
			net.Operator = m[2]
		}
		if plmn := m[3]; len(plmn) >= 5 {
			net.PLMN = plmn[:3] + "-" + plmn[3:]
			if name, ok := modem.LookupServingOperatorNameFromPLMN(plmn); ok {
				net.Operator = name
			}
		}
	}
	parseServingCell(qengResp, &net)
	return net
}

// parseServingCell reads AT+QENG="servingcell". The LTE layout is:
// state,"LTE",duplex,MCC,MNC,cellID,PCID,EARFCN,band,UL_bw,DL_bw,TAC,RSRP,RSRQ,RSSI,SINR,srxlev
func parseServingCell(resp string, net *networkDetails) {
	for _, line := range splitATLines(resp) {
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
		fields := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "+QENG:")), ",")
		for i := range fields {
			fields[i] = strings.Trim(strings.TrimSpace(fields[i]), `"`)
		}
		if len(fields) < 2 || fields[0] != "servingcell" {
			continue
		}
		net.ConnState = fields[1]
		if len(fields) > 2 {
			net.RAT = fields[2]
		}
		if net.RAT != "LTE" || len(fields) < 17 {
			return
		}
		net.Duplex = fields[3]
		net.CellID = strings.ToUpper(fields[6])
		if id, err := strconv.ParseUint(fields[6], 16, 32); err == nil {
			net.ENodeB, net.Sector = int(id>>8), int(id&0xff)
		}
		net.PCI = atoiPtr(fields[7])
		net.EARFCN, _ = strconv.Atoi(fields[8])
		net.Band, _ = strconv.Atoi(fields[9])
		if code, err := strconv.Atoi(fields[11]); err == nil && code >= 0 && code < len(lteBandwidthMHz) {
			net.BandwidthMHz = lteBandwidthMHz[code]
		}
		net.TAC = strings.ToUpper(fields[12])
		net.RSRP = atoiPtr(fields[13])
		net.RSRQ = atoiPtr(fields[14])
		net.RSSI = atoiPtr(fields[15])
		if sinr, err := strconv.ParseFloat(fields[16], 64); err == nil {
			net.SINR = &sinr
		}
		return
	}
}

func atoiPtr(value string) *int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func firstPDPAddress(resp string) string {
	for _, address := range parsePDPAddresses(resp) {
		if address != "0.0.0.0" && !strings.HasPrefix(address, "0.0.0.0.") {
			return address
		}
	}
	return ""
}

func demoCellularStatus() cellularStatus {
	rsrp, rsrq, rssi, pci := -92, -10, -63, 271
	sinr := 12.0
	return cellularStatus{
		SampledAtMS: time.Now().UnixMilli(),
		IMS: imsStatus{
			Enabled: boolPtr(true), Registered: boolPtr(true), VoLTEDisabled: boolPtr(false),
			PDNCID: 2, PDNActive: boolPtr(true), PDNAddress: "2001:db8::42", MBN: "Volte_OpenMkt-Commercial-CMCC",
			State: "ready", Summary: "VoLTE 可用：IMS 已註冊",
		},
		SIM: simDetails{
			PIN: "READY", Inserted: boolPtr(true), InitStatus: 7, InitText: simInitText(7),
			ICCID: "89860123456789012345", IMSI: "460001234567890", HomePLMN: "460-00", HomeOperator: "中國移動",
			SMSC: "+8613800100500",
		},
		Network: networkDetails{
			RegStatus: 1, Registration: registrationText(1), PSAttached: boolPtr(true),
			Operator: "中國移動", PLMN: "460-00", ConnState: "CONNECT", RAT: "LTE", Duplex: "FDD",
			Band: 3, EARFCN: 1650, BandwidthMHz: 20, TAC: "5A1F", CellID: "1B2C3D4", ENodeB: 0x1B2C3D, Sector: 4,
			PCI: &pci, RSRP: &rsrp, RSRQ: &rsrq, RSSI: &rssi, SINR: &sinr,
		},
	}
}
