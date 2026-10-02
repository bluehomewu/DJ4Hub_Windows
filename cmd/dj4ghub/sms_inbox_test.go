package main

import (
	"testing"
	"time"
)

func TestSeedSMSFromHistoryRestoresInboxWithoutDuplicates(t *testing.T) {
	received := time.Date(2024, 8, 7, 14, 25, 57, 0, time.FixedZone("", 8*3600))
	h := &communicationHistory{active: map[int]string{}, records: []historyRecord{
		{ID: "sms-1", Kind: "sms", Direction: "incoming", Number: "10001", Content: "辦理提醒", Started: received.UTC()},
		{ID: "sms-2", Kind: "sms", Direction: "outgoing", Number: "10001", Content: "已傳送", Started: received},
		{ID: "call-1", Kind: "call", Direction: "incoming", Number: "10001", Started: received},
	}}
	a := &app{history: h}
	a.seedSMSFromHistory()
	if len(a.sms) != 1 || a.sms[0].Sender != "10001" {
		t.Fatalf("inbox = %+v", a.sms)
	}
	// The same message read again from the module carries the PDU zone.
	if added, total := a.mergeSMS([]receivedSMS{{Memory: "ME", Sender: "10001", Content: "辦理提醒", Timestamp: received}}); added != 0 || total != 1 {
		t.Fatalf("duplicate inbox entry: added=%d total=%d", added, total)
	}
}
