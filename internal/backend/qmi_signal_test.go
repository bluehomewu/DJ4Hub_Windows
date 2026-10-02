package backend

import "testing"

// QMI NAS 的 LTE RSSNR / 5G NR SINR 單位是 0.1 dB 縮放整數，qmiSNRToDB 應四捨五入為 dB 整數。
func TestQMISNRToDB(t *testing.T) {
	cases := []struct {
		raw  int16
		want int
	}{
		{134, 13}, // 13.4 → 13（截圖裡的異常值，修復前顯示 134）
		{135, 14}, // 13.5 → 14（四捨五入進位）
		{0, 0},    // 守衛處已過濾，但保證安全
		{4, 0},    // 0.4 → 0
		{5, 1},    // 0.5 → 1
		{300, 30}, // 30.0 dB
		{-15, -2}, // -1.5 → -2
		{-23, -2}, // -2.3 → -2
	}
	for _, c := range cases {
		if got := qmiSNRToDB(c.raw); got != c.want {
			t.Errorf("qmiSNRToDB(%d)=%d want %d", c.raw, got, c.want)
		}
	}
}
