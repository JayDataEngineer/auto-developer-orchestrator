package handlers

import (
	"testing"
	"time"
)

func TestAgentRegistryStartStop(t *testing.T) {
	r := NewAgentRegistry()

	r.Start("proj1", "agent1")
	if !r.IsRunning("proj1", "agent1") {
		t.Error("expected agent to be running")
	}
	if r.IsRunning("proj1", "agent2") {
		t.Error("expected agent2 to not be running")
	}

	r.Stop("proj1", "agent1")
	if r.IsRunning("proj1", "agent1") {
		t.Error("expected agent to be stopped")
	}
}

func TestAgentRegistryGetAllRunning(t *testing.T) {
	r := NewAgentRegistry()

	running := r.GetAllRunning()
	if len(running) != 0 {
		t.Errorf("expected 0 running, got %d", len(running))
	}

	r.Start("proj1", "agent1")
	r.Start("proj2", "agent2")

	running = r.GetAllRunning()
	if len(running) != 2 {
		t.Errorf("expected 2 running, got %d", len(running))
	}

	// Verify entries have correct data
	found := map[string]bool{}
	for _, e := range running {
		found[e.Project+":"+e.AgentID] = true
		if e.Status != "running" {
			t.Errorf("expected status 'running', got %q", e.Status)
		}
		if e.StartedAt.IsZero() {
			t.Error("expected non-zero StartedAt")
		}
	}
	if !found["proj1:agent1"] || !found["proj2:agent2"] {
		t.Errorf("expected both agents in result, got %v", found)
	}
}

func TestAgentRegistryBump(t *testing.T) {
	r := NewAgentRegistry()
	r.Start("proj1", "agent1")

	time.Sleep(10 * time.Millisecond)
	r.Bump("proj1", "agent1")

	running := r.GetAllRunning()
	if len(running) != 1 {
		t.Fatalf("expected 1 running, got %d", len(running))
	}
	if running[0].LastEventAt.Before(running[0].StartedAt) {
		t.Error("expected LastEventAt to be after StartedAt")
	}

	// Bump on non-existent agent should not panic
	r.Bump("nope", "nope")
}

func TestAgentRegistryConcurrent(t *testing.T) {
	r := NewAgentRegistry()
	done := make(chan struct{})

	// Start multiple goroutines that access the registry concurrently
	for i := 0; i < 10; i++ {
		go func(n int) {
			proj := "proj"
			agent := string(rune('a' + n))
			r.Start(proj, agent)
			r.Bump(proj, agent)
			r.IsRunning(proj, agent)
			r.GetAllRunning()
			r.Stop(proj, agent)
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	running := r.GetAllRunning()
	if len(running) != 0 {
		t.Errorf("expected 0 running after all stops, got %d", len(running))
	}
}
