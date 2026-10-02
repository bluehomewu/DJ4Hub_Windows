package main

import "testing"

// Responses captured from a QDC507GLEFM21 roaming on Chunghwa Telecom.
const (
	observedIMS     = "AT+QCFG=\"ims\"\r\n+QCFG: \"ims\",1,0\r\nOK"
	observedVoLTE   = "AT+QCFG=\"volte_disable\"\r\n+QCFG: \"volte/disable\",0\r\nOK"
	observedMBN     = "AT+QMBNCFG=\"select\"\r\n+QMBNCFG: \"Select\",ROW_Generic_3GPP\r\nOK"
	observedCGDCONT = "+CGDCONT: 1,\"IPV4V6\",\"\",\"0.0.0.0\",0,0,0,0\r\n+CGDCONT: 2,\"IPV4V6\",\"ims\",\"0.0.0.0\",0,0,0,0\r\n+CGDCONT: 3,\"IPV4V6\",\"SOS\",\"0.0.0.0\",0,0,0,1\r\nOK"
	observedCGACT   = "+CGACT: 1,1\r\n+CGACT: 2,0\r\n+CGACT: 3,0\r\nOK"
	observedQENG    = "AT+QENG=\"servingcell\"\r\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",466,92,4C7A60E,0,3050,7,5,5,3458,-89,-10,-60,7,34\r\nOK"
)

func TestParseIMSStatusEnabledButUnregistered(t *testing.T) {
	ims := parseIMSStatus(observedIMS, observedVoLTE, observedMBN, observedCGDCONT, observedCGACT)
	if ims.Enabled == nil || !*ims.Enabled || ims.Registered == nil || *ims.Registered {
		t.Fatalf("enabled/registered = %v/%v", ims.Enabled, ims.Registered)
	}
	if ims.VoLTEDisabled == nil || *ims.VoLTEDisabled || ims.MBN != "ROW_Generic_3GPP" {
		t.Fatalf("volte/mbn = %v %q", ims.VoLTEDisabled, ims.MBN)
	}
	if ims.PDNCID != 2 || ims.PDNActive == nil || *ims.PDNActive {
		t.Fatalf("IMS PDN = cid %d active %v", ims.PDNCID, ims.PDNActive)
	}
	if ims.State != "unregistered" {
		t.Fatalf("state = %q", ims.State)
	}
}

func TestParseIMSStatusStates(t *testing.T) {
	ready := parseIMSStatus(`+QCFG: "ims",1,1`, observedVoLTE, "", "", "")
	disabled := parseIMSStatus(`+QCFG: "ims",0,0`, observedVoLTE, "", "", "")
	blocked := parseIMSStatus(`+QCFG: "ims",1,1`, `+QCFG: "volte/disable",1`, "", "", "")
	unknown := parseIMSStatus("ERROR", "", "", "", "")
	for name, got := range map[string]string{"ready": ready.State, "disabled": disabled.State, "blocked": blocked.State, "unknown": unknown.State} {
		want := map[string]string{"ready": "ready", "disabled": "disabled", "blocked": "disabled", "unknown": "unknown"}[name]
		if got != want {
			t.Fatalf("%s state = %q, want %q", name, got, want)
		}
	}
}

func TestParseSIMDetails(t *testing.T) {
	sim := parseSIMDetails("+CPIN: READY\r\nOK", "+QSIMSTAT: 0,1\r\nOK", "+QINISTAT: 7\r\nOK",
		"19135200100000164834", "454006109056834", "+CSCA: \"+447797704000\",145\r\nOK")
	if sim.PIN != "READY" || sim.Inserted == nil || !*sim.Inserted || sim.InitText != "初始化完成" {
		t.Fatalf("sim = %+v", sim)
	}
	if sim.HomePLMN != "454-00" || sim.HomeOperator != "CSL" || sim.SMSC != "+447797704000" {
		t.Fatalf("home/smsc = %q %q %q", sim.HomePLMN, sim.HomeOperator, sim.SMSC)
	}
	if got := simInitText(3); got != "正在初始化電話簿" {
		t.Fatalf("simInitText(3) = %q", got)
	}
	if got := decodeSMSCAddress("002B0038003600310033"); got != "+8613" {
		t.Fatalf("UCS2 SMSC = %q", got)
	}
}

func TestParseNetworkDetailsLTE(t *testing.T) {
	net := parseNetworkDetails("+CEREG: 0,5\r\nOK", "+CREG: 0,5\r\nOK", "+CGATT: 1\r\nOK",
		"+QSPN: \"Chunghwa Telecom\",\"Chunghwa\",\"\",0,\"46692\"\r\nOK", observedQENG)
	if !net.Roaming || net.PSAttached == nil || !*net.PSAttached || net.Operator != "中華電信" || net.PLMN != "466-92" {
		t.Fatalf("registration = %+v", net)
	}
	if net.RAT != "LTE" || net.Duplex != "FDD" || net.Band != 7 || net.EARFCN != 3050 || net.BandwidthMHz != 20 {
		t.Fatalf("cell = %+v", net)
	}
	if net.CellID != "4C7A60E" || net.ENodeB != 0x4C7A6 || net.Sector != 0x0E || net.TAC != "3458" {
		t.Fatalf("cell identity = %q %d %d %q", net.CellID, net.ENodeB, net.Sector, net.TAC)
	}
	if *net.RSRP != -89 || *net.RSRQ != -10 || *net.RSSI != -60 || *net.SINR != 7 || *net.PCI != 0 {
		t.Fatalf("signal = %d %d %d %v", *net.RSRP, *net.RSRQ, *net.RSSI, *net.SINR)
	}
}

func TestParseServingCellIgnoresNonLTE(t *testing.T) {
	var net networkDetails
	parseServingCell(`+QENG: "servingcell","SEARCH"`, &net)
	if net.ConnState != "SEARCH" || net.RSRP != nil {
		t.Fatalf("net = %+v", net)
	}
}
