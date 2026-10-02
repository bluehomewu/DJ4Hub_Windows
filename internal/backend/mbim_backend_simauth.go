package backend

import (
	"context"
	"encoding/hex"
	"fmt"
)

func (b *MBIMBackend) OpenLogicalChannel(ctx context.Context, aid string) (int, error) {
	aidBytes, err := hex.DecodeString(aid)
	if err != nil {
		return 0, fmt.Errorf("mbim: invalid AID hex %q: %w", aid, err)
	}
	ch, err := b.source.OpenChannel(ctx, aidBytes)
	if err != nil {
		return 0, err
	}
	return int(ch), nil
}

func (b *MBIMBackend) CloseLogicalChannel(ctx context.Context, channelID int) error {
	return b.source.CloseChannel(ctx, uint32(channelID))
}

func (b *MBIMBackend) TransmitAPDU(ctx context.Context, channelID int, command string) (string, error) {
	cmd, err := hex.DecodeString(command)
	if err != nil {
		return "", fmt.Errorf("mbim: invalid APDU hex: %w", err)
	}
	resp, err := b.source.TransmitAPDU(ctx, uint32(channelID), cmd)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(resp), nil
}

// ResolveSIMAuthAID 把短字首(USIM/ISIM ADF AID 前 7 位元組)解析成完整 AID:這張卡
// 在 MBIM 下不接受用短 AID 開邏輯通道(UICC_OPEN_CHANNEL status=0x87430002
// SelectFailed),必須先拿到完整 AID。
// 現在使用底層的 QMI over MBIM 隧道技術，直接取得真實 USIM/ISIM 列表。
func (b *MBIMBackend) ResolveSIMAuthAID(ctx context.Context, app string, fallbackAID string) (string, string, error) {
	aid, source, err := b.source.ResolveLogicalChannelAID(app, fallbackAID)
	if err != nil {
		return fallbackAID, "fallback_on_error", fmt.Errorf("%w: mbim qmi tunnel resolve %s AID: %v", ErrSIMAuthAIDNotReady, app, err)
	}
	return aid, source, nil
}
