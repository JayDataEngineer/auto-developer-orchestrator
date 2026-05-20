package core

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestTaskManager_StartAndWait(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	task, err := mgr.Start(context.Background(), "echo hello", "", true, "")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if !task.IsBackgrounded {
		t.Fatal("expected task to be backgrounded")
	}

	// Wait for completion with generous timeout
	result, err := mgr.Wait(task.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if result.Status != TaskCompleted {
		t.Errorf("expected status completed, got %s", result.Status)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	output := task.GetOutput()
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestTaskManager_FailedTask(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	task, err := mgr.Start(context.Background(), "exit 42", "", true, "")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	result, err := mgr.Wait(task.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if result.Status != TaskFailed {
		t.Errorf("expected status failed, got %s", result.Status)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestTaskManager_Background(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	// Start a long-running foreground task
	task, err := mgr.Start(context.Background(), "sleep 30 && echo done", "", false, "")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Immediately background it
	err = mgr.Background(task.ID)
	if err != nil {
		t.Fatalf("Background failed: %v", err)
	}

	if !task.IsBackgrounded {
		t.Fatal("expected task to be backgrounded")
	}

	// Double-background should fail
	err = mgr.Background(task.ID)
	if err == nil {
		t.Fatal("expected error on double background")
	}

	// Clean up: kill the sleep
	_ = mgr.Background(task.ID) // already backgrounded, no-op
}

func TestTaskManager_Status(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	// Status of non-existent task
	_, err := mgr.Status("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}

	task, err := mgr.Start(context.Background(), "echo hello", "", true, "")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	status, err := mgr.Status(task.ID)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.Status != TaskRunning {
		t.Errorf("expected running status, got %s", status.Status)
	}

	// Wait for completion
	_, _ = mgr.Wait(task.ID, 5*time.Second)

	status, err = mgr.Status(task.ID)
	if err != nil {
		t.Fatalf("Status after completion failed: %v", err)
	}
	if status.Status != TaskCompleted {
		t.Errorf("expected completed status, got %s", status.Status)
	}
}

func TestTaskManager_List(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	task1, _ := mgr.Start(context.Background(), "echo one", "", true, "")
	task2, _ := mgr.Start(context.Background(), "echo two", "", true, "")

	list := mgr.List()
	if len(list) < 2 {
		t.Fatalf("expected at least 2 tasks, got %d", len(list))
	}

	// Verify both IDs are present
	ids := map[string]bool{}
	for _, s := range list {
		ids[s.ID] = true
	}
	if !ids[task1.ID] || !ids[task2.ID] {
		t.Errorf("expected both task IDs in list, got %v", ids)
	}

	// Wait for both to complete
	_, _ = mgr.Wait(task1.ID, 5*time.Second)
	_, _ = mgr.Wait(task2.ID, 5*time.Second)
}

func TestTaskManager_GetOutput(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	task, _ := mgr.Start(context.Background(), "echo 'hello world'", "", true, "")
	_, _ = mgr.Wait(task.ID, 5*time.Second)

	output, err := mgr.GetOutput(task.ID)
	if err != nil {
		t.Fatalf("GetOutput failed: %v", err)
	}
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestTaskManager_ForegroundSelect(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	// Start a fast foreground task — should complete via Done channel
	task, _ := mgr.Start(context.Background(), "echo fast", "", false, "")

	select {
	case <-task.Done:
		// Good — completed normally
	case <-task.BackgroundReq:
		t.Fatal("expected task to complete, not background")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task")
	}
}

func TestTaskManager_SSEEvents(t *testing.T) {
	dir, _ := os.MkdirTemp("", "pux-task-test-")
	defer os.RemoveAll(dir)

	mgr := NewTaskManager(dir)

	// Set up subscriber to capture events
	events := make(chan AgentEvent, 256)
	mgr.SetSubscriber(events)

	task, _ := mgr.Start(context.Background(), "echo test", "", true, "")
	_, _ = mgr.Wait(task.ID, 5*time.Second)

	// Drain events and check types
	var started, completed bool
	timeout := time.After(2 * time.Second)
	for !started || !completed {
		select {
		case evt := <-events:
			switch evt.Type {
			case EventTypeTaskStarted:
				started = true
				if evt.Data.TaskID != task.ID {
					t.Errorf("expected task ID %s, got %s", task.ID, evt.Data.TaskID)
				}
			case EventTypeTaskCompleted:
				completed = true
				if evt.Data.TaskID != task.ID {
					t.Errorf("expected task ID %s, got %s", task.ID, evt.Data.TaskID)
				}
			}
		case <-timeout:
			t.Fatalf("timeout waiting for events (started=%v, completed=%v)", started, completed)
		}
	}
}
