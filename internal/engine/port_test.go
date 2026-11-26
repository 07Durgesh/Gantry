package engine

import "testing"

func TestPortManagerAllocateReleaseAndExhaustion(t *testing.T) {
	pm := NewPortManager(49100, 49101)

	p1, err := pm.Allocate()
	if err != nil {
		t.Fatalf("first Allocate failed: %v", err)
	}
	p2, err := pm.Allocate()
	if err != nil {
		t.Fatalf("second Allocate failed: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("expected distinct ports, got %d twice", p1)
	}

	if _, err := pm.Allocate(); err == nil {
		t.Fatal("expected port exhaustion error when range is full")
	}

	pm.Release(p1)
	p3, err := pm.Allocate()
	if err != nil {
		t.Fatalf("Allocate after Release failed: %v", err)
	}
	if p3 != p1 {
		t.Fatalf("expected released port %d to be reused, got %d", p1, p3)
	}
}
