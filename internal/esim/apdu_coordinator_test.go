package esim

import (
	"context"
	"sync"
	"testing"

	"github.com/WongLoki/DJ4Hub/internal/apduarbiter"
)

func TestAPDUCoordinatorChanMuIsStablePerChannel(t *testing.T) {
	c := newAPDUCoordinator("TEST")
	a := c.getOrCreateChanMu(3)
	b := c.getOrCreateChanMu(3)
	if a != b {
		t.Fatal("getOrCreateChanMu(3) 應回傳同一把鎖")
	}
	if c.getOrCreateChanMu(4) == a {
		t.Fatal("不同 channel 應是不同鎖")
	}
}

func TestAPDUCoordinatorSessionRegistry(t *testing.T) {
	c := newAPDUCoordinator("TEST")
	if c.hasSession(2) {
		t.Fatal("未繫結時 hasSession 應為 false")
	}
	c.bindSession(2, "esim")
	if !c.hasSession(2) {
		t.Fatal("繫結後 hasSession 應為 true")
	}
	if _, ok := c.takeSession(2); !ok {
		t.Fatal("takeSession 應取出已繫結工作階段")
	}
	if c.hasSession(2) {
		t.Fatal("takeSession 後工作階段應被移除")
	}
}

func TestAPDUCoordinatorAcquireLeaseNilArbiterReturnsNil(t *testing.T) {
	c := newAPDUCoordinator("MBIM")
	lease, err := c.acquireLease(context.Background(), 0, "owner", apduarbiter.APDUClassEUICCWrite, 0, apduarbiter.TransportScopeExclusive)
	if err != nil {
		t.Fatalf("nil arbiter 不應回報錯誤: %v", err)
	}
	if lease != nil {
		t.Fatal("nil arbiter 應回傳 nil 租約(退化為僅互斥)")
	}
}

func TestAPDUCoordinatorAcquireLeaseUsesArbiterAndMode(t *testing.T) {
	c := newAPDUCoordinator("MBIM")
	arb := apduarbiter.New("test-dev", apduarbiter.Options{MaxSessions: 3, MaxQMITransports: 3})
	c.setArbiter(arb)
	lease, err := c.acquireLease(context.Background(), 0, "esim_session_open", apduarbiter.APDUClassEUICCWrite, 0, apduarbiter.TransportScopeExclusive)
	if err != nil {
		t.Fatalf("acquireLease 失敗: %v", err)
	}
	if lease == nil {
		t.Fatal("有 arbiter 時應回傳非 nil 租約")
	}
	lease.Release()
}

var _ = sync.Mutex{}
