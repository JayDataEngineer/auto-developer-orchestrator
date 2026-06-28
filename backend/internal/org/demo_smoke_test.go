package org_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/org"
)

// TestLoadDemoOrgSmoke loads the shipped orgs/_demo template as a sanity
// check that the canonical example parses end-to-end through the real
// Loader. If this fails, list_orgs / dispatch_task on a fresh install
// are also broken.
func TestLoadDemoOrgSmoke(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/org/<this> → backend/
	backendRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	projectRoot := filepath.Dir(backendRoot)

	loader := org.NewLoader(projectRoot)
	got, err := loader.LoadByName("_demo")
	if err != nil {
		t.Fatalf("LoadByName(_demo): %v", err)
	}
	if got.Name != "_demo" {
		t.Errorf("Name = %q, want _demo", got.Name)
	}
	if got.Description == "" {
		t.Error("Description is empty")
	}
	if len(got.CTO.Prompt) == 0 {
		t.Error("CTO prompt body is empty")
	}
	if !contains(got.CTO.Tools, "delegate_to") {
		t.Errorf("CTO tools missing delegate_to: %v", got.CTO.Tools)
	}
	r, ok := got.Roles["researcher"]
	if !ok {
		t.Fatal("researcher role missing")
	}
	if len(r.Prompt) == 0 {
		t.Error("researcher prompt body is empty")
	}
	if contains(r.Tools, "delegate_to") {
		t.Errorf("researcher must NOT have delegate_to (recursion guard): %v", r.Tools)
	}

	// list_orgs path: LoadAll must also see _demo.
	all, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var sawDemo bool
	for _, o := range all {
		if o.Name == "_demo" { sawDemo = true; break }
	}
	if !sawDemo {
		t.Errorf("LoadAll did not return _demo: %v", all)
	}
}

func contains(s []string, want string) bool {
	for _, x := range s { if x == want { return true } }
	return false
}
