package main

import "regexp"

// Keywords cover both scripts: messages from mainland senders arrive in
// Simplified Chinese, local ones in Traditional Chinese.
const smsCodeKeywords = `验证码|驗證碼|校验码|校驗碼|动态码|動態碼|认证码|認證碼|verification\s*code|security\s*code|one[-\s]?time\s*(?:password|code)|otp|passcode|login\s*code`

var smsCodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:` + smsCodeKeywords + `|验证代码|驗證代碼|code)\D{0,12}([0-9]{4,8})`),
	regexp.MustCompile(`(?i)([0-9]{4,8})\D{0,12}(?:` + smsCodeKeywords + `)`),
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
