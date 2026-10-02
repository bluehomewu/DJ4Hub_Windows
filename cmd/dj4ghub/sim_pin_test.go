package main

import "testing"

func TestParsePINCountersAndState(t *testing.T) {
	pin, puk := parsePINCounters("AT+QPINC=\"SC\"\r\n+QPINC: \"SC\",3,10\r\nOK")
	if pin == nil || puk == nil || *pin != 3 || *puk != 10 {
		t.Fatalf("counters = %v %v", pin, puk)
	}
	if pin, puk := parsePINCounters("+CME ERROR: 10"); pin != nil || puk != nil {
		t.Fatal("error response parsed as counters")
	}
	for resp, want := range map[string]string{
		"+CPIN: SIM PIN\r\nOK": "SIM PIN",
		"+CPIN: READY\r\nOK":   "READY",
		"+CPIN: SIM PUK\r\nOK": "SIM PUK",
		"+CME ERROR: 10":       "",
	} {
		if got := parseCPINState(resp); got != want {
			t.Fatalf("parseCPINState(%q) = %q, want %q", resp, got, want)
		}
	}
	for _, pin := range []string{"", "123", "123456789", "12a4", "0000\r"} {
		if simPINPattern.MatchString(pin) {
			t.Fatalf("accepted invalid PIN %q", pin)
		}
	}
}
