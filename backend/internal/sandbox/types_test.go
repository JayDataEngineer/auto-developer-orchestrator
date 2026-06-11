package sandbox

import (
	"testing"
)

func TestNewPortAllocator(t *testing.T) {
	p := NewPortAllocator()
	if p.nextDisplayNum != 1 {
		t.Errorf("expected display 1, got %d", p.nextDisplayNum)
	}
	if p.nextVNCPort != 5901 {
		t.Errorf("expected VNC 5901, got %d", p.nextVNCPort)
	}
	if p.nextCDPPort != 9222 {
		t.Errorf("expected CDP 9222, got %d", p.nextCDPPort)
	}
	if p.nextNoVNCPort != 6081 {
		t.Errorf("expected noVNC 6081, got %d", p.nextNoVNCPort)
	}
}

func TestAllocatePorts_Increments(t *testing.T) {
	p := NewPortAllocator()
	d1, v1, c1, n1 := p.AllocatePorts()
	d2, v2, c2, n2 := p.AllocatePorts()

	if d1 != 1 || d2 != 2 {
		t.Errorf("display numbers: got %d and %d, want 1 and 2", d1, d2)
	}
	if v1 != 5901 || v2 != 5902 {
		t.Errorf("VNC ports: got %d and %d, want 5901 and 5902", v1, v2)
	}
	if c1 != 9222 || c2 != 9223 {
		t.Errorf("CDP ports: got %d and %d, want 9222 and 9223", c1, c2)
	}
	if n1 != 6081 || n2 != 6082 {
		t.Errorf("noVNC ports: got %d and %d, want 6081 and 6082", n1, n2)
	}
}

func TestReleasePorts_Reuses(t *testing.T) {
	p := NewPortAllocator()
	d1, v1, c1, n1 := p.AllocatePorts()
	p.AllocatePorts() // allocate second

	p.ReleasePorts(d1, v1, c1, n1)

	// Next allocation should reuse the released ports
	d, v, c, n := p.AllocatePorts()
	if d != d1 {
		t.Errorf("expected display %d, got %d", d1, d)
	}
	if v != v1 {
		t.Errorf("expected VNC %d, got %d", v1, v)
	}
	if c != c1 {
		t.Errorf("expected CDP %d, got %d", c1, c)
	}
	if n != n1 {
		t.Errorf("expected noVNC %d, got %d", n1, n)
	}
}

func TestReleasePorts_FirstReleased(t *testing.T) {
	p := NewPortAllocator()
	d1, _, _, _ := p.AllocatePorts()
	d2, v2, c2, n2 := p.AllocatePorts()
	d3, _, _, _ := p.AllocatePorts()

	// Release middle allocation
	p.ReleasePorts(d2, v2, c2, n2)

	// Re-allocate - should get d2's ports
	d, v, c, n := p.AllocatePorts()
	if d != d2 {
		t.Errorf("expected display %d, got %d", d2, d)
	}
	if v != v2 {
		t.Errorf("expected VNC %d, got %d", v2, v)
	}
	if c != c2 {
		t.Errorf("expected CDP %d, got %d", c2, c)
	}
	if n != n2 {
		t.Errorf("expected noVNC %d, got %d", n2, n)
	}

	// Verify d1 and d3 are still in use
	if p.usedDisplays[d1] != true {
		t.Error("d1 should still be in use")
	}
	if p.usedDisplays[d3] != true {
		t.Error("d3 should still be in use")
	}
}

func TestAllocatePorts_SkipUsed(t *testing.T) {
	p := NewPortAllocator()
	d1, v1, c1, n1 := p.AllocatePorts()

	// Manually mark the next ports as used
	p.usedDisplays[2] = true

	// Should skip to display 3
	d2, _, _, _ := p.AllocatePorts()
	if d2 != 3 {
		t.Errorf("expected display 3 (skip 2), got %d", d2)
	}

	// Clean up to avoid interference
	p.ReleasePorts(d1, v1, c1, n1)
}

func TestAllocatePorts_AllUsed(t *testing.T) {
	p := NewPortAllocator()

	// Simulate all VNC ports in range 5901-5920 being used
	for port := 5901; port <= 5920; port++ {
		p.usedVNC[port] = true
	}
	p.nextVNCPort = 5901

	// Allocate should return whatever port is next (even if beyond preferred range)
	_, v, _, _ := p.AllocatePorts()
	// Just verify we don't get zero, infinite loop, or crash
	if v == 0 {
		t.Errorf("VNC port should be non-zero, got %d", v)
	}
}
