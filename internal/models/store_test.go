package models

import (
	"testing"
	"time"
)

func TestDeploymentStoreLifecycleAndListOrder(t *testing.T) {
	store := NewDeploymentStore()

	d1 := store.Create("alpha", "/tmp/alpha")
	time.Sleep(2 * time.Millisecond)
	d2 := store.Create("alpha", "/tmp/alpha-v2")

	store.SetContainerDetails(d1.ID, "cid-1", 40001)
	store.UpdateStatus(d1.ID, StatusHealthy, "")
	store.SetActiveApp("alpha", d1.ID)

	active, ok := store.GetActiveApp("alpha")
	if !ok || active.ID != d1.ID || active.HostPort != 40001 {
		t.Fatalf("unexpected active app: ok=%v dep=%+v", ok, active)
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(list))
	}
	if list[0].ID != d2.ID || list[1].ID != d1.ID {
		t.Fatalf("expected newest-first order [%s, %s], got [%s, %s]", d2.ID, d1.ID, list[0].ID, list[1].ID)
	}
}
