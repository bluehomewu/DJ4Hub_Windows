package main

import "testing"

func TestExtractSMSCode(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "Chinese", content: "【服務】您的驗證碼為 482913，5 分鐘內有效。", want: "482913"},
		{name: "English", content: "Your verification code is 123456. Do not share it.", want: "123456"},
		{name: "Code before keyword", content: "登入動態碼：778899", want: "778899"},
		{name: "No keyword", content: "您的號碼 13800138000，本月餘額 128.50 元。", want: ""},
		{name: "Too short", content: "驗證碼 123", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractSMSCode(test.content); got != test.want {
				t.Fatalf("extractSMSCode(%q) = %q, want %q", test.content, got, test.want)
			}
		})
	}
}
