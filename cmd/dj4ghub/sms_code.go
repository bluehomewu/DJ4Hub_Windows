package main

import "regexp"

var smsCodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:驗證碼|校驗碼|動態碼|驗證程式碼|verification\s*code|security\s*code|one[-\s]?time\s*(?:password|code)|otp|passcode|login\s*code|code)\D{0,12}([0-9]{4,8})`),
	regexp.MustCompile(`(?i)([0-9]{4,8})\D{0,12}(?:驗證碼|校驗碼|動態碼|verification\s*code|security\s*code|one[-\s]?time\s*(?:password|code)|otp|passcode|login\s*code)`),
}

// extractSMSCode only accepts a short number adjacent to an OTP-style keyword.
// This avoids treating ordinary phone numbers, balances, and order IDs as codes.
func extractSMSCode(content string) string {
	for _, pattern := range smsCodePatterns {
		if match := pattern.FindStringSubmatch(content); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}
