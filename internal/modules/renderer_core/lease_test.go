package renderercore

import (
	"testing"
	"time"
)

func TestLeaseAcquireRenewRelease(t *testing.T) {
	manager := &LeaseManager{}

	lease, err := manager.Acquire("tester", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if lease.Token == "" {
		t.Fatalf("expected token")
	}

	_, err = manager.Renew(lease.ID, lease.Token, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	if err := manager.Release(lease.ID, lease.Token); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestLeaseExpiry(t *testing.T) {
	manager := &LeaseManager{}
	lease, err := manager.Acquire("tester", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(3 * time.Millisecond)
	if err := manager.Require(lease.ID, lease.Token); err == nil {
		t.Fatalf("expected expired lease")
	}
}
