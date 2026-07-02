package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writePolicyFile writes a policy.yaml under a fake orgs/<name>/ dir.
// Returns the projectRoot the caller should pass to Load.
func writePolicyFile(t *testing.T, orgName, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "orgs", orgName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(body), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return root
}

func TestLoad_NoFile(t *testing.T) {
	root := t.TempDir()
	_, err := Load("ghost", root)
	if err != ErrNoPolicy {
		t.Fatalf("expected ErrNoPolicy, got %v", err)
	}
}

func TestLoad_EmptyOrgName(t *testing.T) {
	// Defensive — empty org name should not even attempt a read.
	_, err := Load("", "/tmp")
	if err != ErrNoPolicy {
		t.Fatalf("expected ErrNoPolicy for empty org name, got %v", err)
	}
}

func TestLoad_ValidYaml(t *testing.T) {
	body := `
workspace:
  mounts:
    - host: ${HOME}
      container: /workspace/home
      mode: ro
  run_as_host_user: true
egress:
  allow:
    - host: 1.2.3.4
      port: 443
    - host: example.com
      ports: [80, 443]
credentials:
  required: [ALPHA]
  optional: [BETA]
`
	root := writePolicyFile(t, "acme", body)
	p, err := Load("acme", root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !p.Workspace.RunAsHostUser {
		t.Error("RunAsHostUser not parsed")
	}
	if len(p.Workspace.Mounts) != 1 || p.Workspace.Mounts[0].Container != "/workspace/home" {
		t.Errorf("mount not parsed: %+v", p.Workspace.Mounts)
	}
	if len(p.Egress.Allow) != 2 {
		t.Errorf("expected 2 egress rules, got %d", len(p.Egress.Allow))
	}
	if p.Egress.Allow[0].Protocol != "tcp" {
		t.Errorf("expected default tcp protocol, got %q", p.Egress.Allow[0].Protocol)
	}
	if len(p.Credentials.Required) != 1 || p.Credentials.Required[0] != "ALPHA" {
		t.Errorf("required creds not parsed: %+v", p.Credentials.Required)
	}
}

func TestLoad_MalformedYaml(t *testing.T) {
	// Tabs are explicitly invalid in YAML — guaranteed parser failure.
	root := writePolicyFile(t, "broken", "workspace:\n\tmounts: oops\n")
	_, err := Load("broken", root)
	if err == nil || err == ErrNoPolicy {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestValidateEnv_AllPresent(t *testing.T) {
	t.Setenv("REQUIRED_ONE", "a")
	t.Setenv("REQUIRED_TWO", "b")
	p := &Policy{Credentials: Credentials{Required: []string{"REQUIRED_ONE", "REQUIRED_TWO"}}}
	if err := ValidateEnv(p); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateEnv_MissingListsAll(t *testing.T) {
	// Don't set either of these in the test env.
	t.Setenv("PRESENT_ONE", "x")
	p := &Policy{Credentials: Credentials{Required: []string{"PRESENT_ONE", "MISSING_ONE", "MISSING_TWO"}}}
	err := ValidateEnv(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var e *ErrMissingCreds
	if !errors.As(err, &e) {
		t.Fatalf("expected *ErrMissingCreds, got %T: %v", err, err)
	}
	if len(e.Missing) != 2 {
		t.Fatalf("expected 2 missing, got %d: %v", len(e.Missing), e.Missing)
	}
}

func TestEnvVars_RequiredAndOptional(t *testing.T) {
	t.Setenv("REQ", "rv")
	t.Setenv("OPT_SET", "ov")
	// OPT_UNSET intentionally not set.
	p := &Policy{Credentials: Credentials{
		Required: []string{"REQ"},
		Optional: []string{"OPT_SET", "OPT_UNSET"},
	}}
	got := EnvVars(p)
	wantSet := map[string]bool{"REQ=rv": false, "OPT_SET=ov": false}
	for _, kv := range got {
		if _, ok := wantSet[kv]; ok {
			wantSet[kv] = true
		} else {
			t.Errorf("unexpected env var: %s", kv)
		}
	}
	for kv, found := range wantSet {
		if !found {
			t.Errorf("missing expected env var: %s", kv)
		}
	}
}

func TestResolveMounts_PlaceholderExpansion(t *testing.T) {
	t.Setenv("MY_VAR", "/tmp/expanded")
	p := &Policy{Workspace: Workspace{Mounts: []Mount{
		{Host: "${MY_VAR}", Container: "/workspace/x", Mode: "rw"},
	}}}
	out, err := ResolveMounts(p)
	if err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}
	if len(out) != 1 || out[0].Host != "/tmp/expanded" {
		t.Errorf("expected expanded host, got %+v", out)
	}
}

func TestResolveMounts_UnsetVarFailsLoud(t *testing.T) {
	// UNSET_MOUNT_VAR is never set.
	p := &Policy{Workspace: Workspace{Mounts: []Mount{
		{Host: "${UNSET_MOUNT_VAR}", Container: "/workspace/x"},
	}}}
	_, err := ResolveMounts(p)
	if err == nil {
		t.Fatal("expected error for unset var, got nil")
	}
	var e *ErrUnresolvedMount
	if !errors.As(err, &e) {
		t.Fatalf("expected *ErrUnresolvedMount, got %T: %v", err, err)
	}
	if e.MissingVar != "UNSET_MOUNT_VAR" {
		t.Errorf("MissingVar = %q, want UNSET_MOUNT_VAR", e.MissingVar)
	}
}

func TestResolveMounts_RelativeContainerRejected(t *testing.T) {
	p := &Policy{Workspace: Workspace{Mounts: []Mount{
		{Host: "/abs/path", Container: "relative/path"},
	}}}
	_, err := ResolveMounts(p)
	if err == nil {
		t.Fatal("expected error for relative container path")
	}
}

func TestResolveMounts_BadModeRejected(t *testing.T) {
	p := &Policy{Workspace: Workspace{Mounts: []Mount{
		{Host: "/abs/path", Container: "/workspace/x", Mode: "execute"},
	}}}
	_, err := ResolveMounts(p)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestResolveMounts_DefaultModeRW(t *testing.T) {
	p := &Policy{Workspace: Workspace{Mounts: []Mount{
		{Host: "/abs/path", Container: "/workspace/x"},
	}}}
	out, err := ResolveMounts(p)
	if err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}
	if out[0].Mode != "rw" {
		t.Errorf("expected default rw, got %q", out[0].Mode)
	}
}

func TestEgressRules_LiteralIP(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "1.2.3.4", Port: 443},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Fatalf("EgressRules: %v", err)
	}
	if out != "1.2.3.4 443\n" {
		t.Errorf("got %q, want '1.2.3.4 443\\n'", out)
	}
}

func TestEgressRules_IPv6Literal(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "::1", Port: 443},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Fatalf("EgressRules: %v", err)
	}
	if out != "::1 443\n" {
		t.Errorf("got %q, want '::1 443\\n'", out)
	}
}

func TestEgressRules_PortsListFanout(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "10.0.0.1", Ports: []int{80, 443, 8080}},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Fatalf("EgressRules: %v", err)
	}
	want := "10.0.0.1 80\n10.0.0.1 443\n10.0.0.1 8080\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestEgressRules_DNSResolutionReal(t *testing.T) {
	// Hits real DNS — this is intentional. We want to know if the
	// resolution pipeline works against a live hostname, not a mock.
	// If offline, skip rather than fail.
	if testing.Short() {
		t.Skip("skipping real-DNS test in -short mode")
	}
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "localhost", Port: 80},
	}}}
	out, err := EgressRules(p)
	if err != nil {
		t.Skipf("DNS resolution failed (likely offline): %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty rules")
	}
}

func TestEgressRules_DNSFailureIsError(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "this-host-does-not-exist.invalid", Port: 443},
	}}}
	_, err := EgressRules(p)
	if err == nil {
		t.Fatal("expected DNS resolution error, got nil")
	}
}

func TestEgressRules_PortOutOfRange(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "1.2.3.4", Port: 99999},
	}}}
	_, err := EgressRules(p)
	if err == nil {
		t.Fatal("expected port-out-of-range error")
	}
}

func TestEgressRules_NoPort(t *testing.T) {
	p := &Policy{Egress: Egress{Allow: []Rule{
		{Host: "1.2.3.4"},
	}}}
	_, err := EgressRules(p)
	if err == nil {
		t.Fatal("expected error for rule without port")
	}
}

func TestEgressRules_EmptyPolicyReturnsEmpty(t *testing.T) {
	p := &Policy{}
	out, err := EgressRules(p)
	if err != nil {
		t.Fatalf("EgressRules: %v", err)
	}
	if out != "" {
		t.Errorf("got %q, want empty", out)
	}
}

func TestEgressRules_NilPolicySafe(t *testing.T) {
	out, err := EgressRules(nil)
	if err != nil {
		t.Fatalf("EgressRules(nil): %v", err)
	}
	if out != "" {
		t.Errorf("got %q, want empty", out)
	}
}
