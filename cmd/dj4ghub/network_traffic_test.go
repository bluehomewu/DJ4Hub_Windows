package main

import "testing"

func TestSessionTrafficIsDownloadPlusUpload(t *testing.T) {
	rx, tx, total := sessionTrafficFromCounters(
		networkByteCounters{RX: 8192, TX: 4096},
		networkByteCounters{RX: 2048, TX: 1024},
	)
	if rx != 6144 || tx != 3072 || total != 9216 {
		t.Fatalf("session traffic = rx:%d tx:%d total:%d", rx, tx, total)
	}
}

func TestUsableIPv4RejectsLinkLocal(t *testing.T) {
	for value, want := range map[string]bool{
		"10.18.22.217":   true,
		"192.168.225.23": true,
		"169.254.183.9":  false,
		"0.0.0.0":        false,
		"127.0.0.1":      false,
		"":               false,
		"fe80::1":        false,
	} {
		if got := usableIPv4(value); got != want {
			t.Fatalf("usableIPv4(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseWWANProfilesIgnoresLocalizedHeading(t *testing.T) {
	output := "\r\n介面 行動電話 5 上的設定檔:  \r\n-------------------------------------\r\n" +
		"    !!##MBIMModemProvisionedContextInternetProfile##19135200100000164834\r\n" +
		"    A1656AB0-BED7-4B96-A076-CD96651786BB\r\n\r\n"
	got := parseWWANProfiles(output)
	if len(got) != 2 || got[1] != "A1656AB0-BED7-4B96-A076-CD96651786BB" {
		t.Fatalf("profiles = %q", got)
	}
	if got := parseWWANProfiles("Profile list on interface X:\r\n----\r\n\r\n"); len(got) != 0 {
		t.Fatalf("empty list = %q", got)
	}
}

func TestClassifyInterfaceType(t *testing.T) {
	for ifType, want := range map[uint32]string{6: "ethernet", 71: "wifi", 243: "wwan", 131: "tunnel", 24: "loopback", 1: "other"} {
		if got := classifyInterfaceType(ifType); got != want {
			t.Fatalf("classifyInterfaceType(%d) = %q, want %q", ifType, got, want)
		}
	}
}
