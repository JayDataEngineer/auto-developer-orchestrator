package sandbox

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestNewPortAllocator(t *testing.T) {
	p := NewPortAllocator()
	if p.nextDisplayNum != 1 {
		t.Errorf("expected display 1, got %d", p.nextDisplayNum)
	}
	if p.nextVNCPort != 5901 {
		t.Errorf("expected VNC 5901, got %d", p.nextVNCPort)
	}
	if p.nextCDPPort != 9223 {
		t.Errorf("expected CDP 9223, got %d", p.nextCDPPort)
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
	if c1 != 9223 || c2 != 9224 {
		t.Errorf("CDP ports: got %d and %d, want 9223 and 9224", c1, c2)
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

// TestSandboxVolumeBindString locks in the bind-string rendering for
// org-declared sandbox volumes. The manager appends whatever BindString()
// returns to the Docker container's Binds list — a malformed string here
// fails container creation, so the rendering rules need a regression test.
//
// Rules under test:
//   - type=volume with docker_name → "<docker_name>:<container>"
//   - type=volume without docker_name → falls back to name
//   - type=bind → "<host>:<container>"
//   - missing container → empty string (skip signal)
//   - missing source (no name/docker_name for volume, no host for bind) → empty
//   - unknown type → empty (defensive — schema validator should catch upstream)
func TestSandboxVolumeBindString(t *testing.T) {
	cases := []struct {
		name string
		v    SandboxVolume
		want string
	}{
		{
			name: "volume with docker_name override",
			v:    SandboxVolume{Type: "volume", Name: "workspace", DockerName: "test_org_workspace", Container: "/workspace"},
			want: "test_org_workspace:/workspace",
		},
		{
			name: "volume without docker_name falls back to name",
			v:    SandboxVolume{Type: "volume", Name: "cache", Container: "/cache"},
			want: "cache:/cache",
		},
		{
			name: "bind mount",
			v:    SandboxVolume{Type: "bind", Host: "/tmp/host", Container: "/mnt/host"},
			want: "/tmp/host:/mnt/host",
		},
		{
			name: "empty type defaults to volume semantics",
			v:    SandboxVolume{Name: "implicit", Container: "/data"},
			want: "implicit:/data",
		},
		{
			name: "missing container returns empty",
			v:    SandboxVolume{Type: "volume", Name: "workspace", DockerName: "ws"},
			want: "",
		},
		{
			name: "volume missing both name and docker_name returns empty",
			v:    SandboxVolume{Type: "volume", Container: "/data"},
			want: "",
		},
		{
			name: "bind missing host returns empty",
			v:    SandboxVolume{Type: "bind", Container: "/data"},
			want: "",
		},
		{
			name: "unknown type returns empty",
			v:    SandboxVolume{Type: "tmpfs", Name: "x", Container: "/y"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.v.BindString(); got != tc.want {
				t.Errorf("BindString() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAdoptContainerAsSandbox locks down the contract for adopting an
// externally-created Docker container (e.g. one started by an org's
// `docker compose up` via bootstrap.sh) into Pux's in-memory sandbox map.
//
// This is the linchpin of container reuse: without it, Pux silently spins
// up a sibling container when the user already has one running for the
// same project. The discovery path lives in discoverByProjectLabel (which
// needs a real Docker client); the adoption logic is pure and lives here.
//
// Rules under test:
//   - matching project-path label → adopted with full metadata recovery
//   - mismatched project-path label → nil (don't adopt wrong container)
//   - missing labels entirely → nil
//   - sandbox-id label missing → fall back to container name, then ID hash
//   - policy label missing → default to "developer"
//   - container with no names AND short ID → use the ID as-is (no panic)
func TestAdoptContainerAsSandbox(t *testing.T) {
	const wantPath = "/home/user/projects/invest"
	cases := []struct {
		name      string
		c         container.Summary
		project   string
		wantAdopt bool
		wantID    string
		wantPol   string
	}{
		{
			name: "compose container with full labels",
			c: container.Summary{
				ID:     "abc123def456",
				Names:  []string{"/invest-sandbox-1"},
				Image:  "invest-sandbox:latest",
				Labels: map[string]string{
					"openshell.org-name":     "invest",
					"openshell.project-path": wantPath,
					"openshell.sandbox-id":   "invest-compose",
					"openshell.policy":       "developer",
				},
			},
			project:   wantPath,
			wantAdopt: true,
			wantID:    "invest-compose",
			wantPol:   "developer",
		},
		{
			name: "mismatched project path is rejected",
			c: container.Summary{
				Labels: map[string]string{
					"openshell.project-path": "/some/other/path",
					"openshell.sandbox-id":   "other",
				},
			},
			project:   wantPath,
			wantAdopt: false,
		},
		{
			name: "empty labels map is rejected",
			c: container.Summary{
				Labels: map[string]string{},
			},
			project:   wantPath,
			wantAdopt: false,
		},
		{
			name: "nil labels map is rejected",
			c: container.Summary{
				Labels: nil,
			},
			project:   wantPath,
			wantAdopt: false,
		},
		{
			name: "missing sandbox-id falls back to container name",
			c: container.Summary{
				ID:    "xyz789",
				Names: []string{"/my-container"},
				Labels: map[string]string{
					"openshell.project-path": wantPath,
				},
			},
			project:   wantPath,
			wantAdopt: true,
			wantID:    "my-container",
			wantPol:   "developer", // default
		},
		{
			name: "no names and no sandbox-id falls back to short ID",
			c: container.Summary{
				ID:    "abcdef1234567890",
				Names: nil,
				Labels: map[string]string{
					"openshell.project-path": wantPath,
				},
			},
			project:   wantPath,
			wantAdopt: true,
			wantID:    "abcdef123456", // first 12 chars
			wantPol:   "developer",
		},
		{
			name: "explicit policy label is preserved",
			c: container.Summary{
				ID:    "policytest",
				Names: []string{"/policy-test"},
				Labels: map[string]string{
					"openshell.project-path": wantPath,
					"openshell.policy":       "researcher",
				},
			},
			project:   wantPath,
			wantAdopt: true,
			wantID:    "policy-test",
			wantPol:   "researcher",
		},
		{
			name: "name with multiple slashes uses first as-is",
			c: container.Summary{
				ID:    "multi",
				Names: []string{"/foo", "/bar"},
				Labels: map[string]string{
					"openshell.project-path": wantPath,
				},
			},
			project:   wantPath,
			wantAdopt: true,
			wantID:    "foo", // first name, leading slash stripped
			wantPol:   "developer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := adoptContainerAsSandbox(tc.c, tc.project)
			if !tc.wantAdopt {
				if sb != nil {
					t.Errorf("expected nil adoption, got %+v", sb)
				}
				return
			}
			if sb == nil {
				t.Fatalf("expected adoption, got nil")
			}
			if sb.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", sb.ID, tc.wantID)
			}
			if sb.Policy != tc.wantPol {
				t.Errorf("Policy = %q, want %q", sb.Policy, tc.wantPol)
			}
			if sb.ProjectPath != tc.project {
				t.Errorf("ProjectPath = %q, want %q", sb.ProjectPath, tc.project)
			}
			if sb.ContainerID != tc.c.ID {
				t.Errorf("ContainerID = %q, want %q", sb.ContainerID, tc.c.ID)
			}
			if sb.Status != StatusRunning {
				t.Errorf("Status = %q, want %q", sb.Status, StatusRunning)
			}
		})
	}
}

// TestAdoptContainerAsSandbox_NameLeadingSlashStripped is a focused
// regression: Docker returns container names with a leading "/" (e.g.
// "/invest-sandbox-1"). The adopted sandbox ID must strip it, otherwise
// downstream lookups by ID (file paths, exec commands) treat it as a
// relative path and fail in confusing ways.
func TestAdoptContainerAsSandbox_NameLeadingSlashStripped(t *testing.T) {
	c := container.Summary{
		ID:    "abc",
		Names: []string{"/invest-sandbox-1"},
		Labels: map[string]string{
			"openshell.project-path": "/p",
		},
	}
	sb := adoptContainerAsSandbox(c, "/p")
	if sb == nil {
		t.Fatal("expected non-nil adoption")
	}
	if sb.ID != "invest-sandbox-1" {
		t.Errorf("ID = %q, want %q (leading slash must be stripped)",
			sb.ID, "invest-sandbox-1")
	}
}
