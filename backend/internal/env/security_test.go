package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityGuard_CheckWritePath_DeniedExact(t *testing.T) {
	guard := NewSecurityGuard()
	home, _ := os.UserHomeDir()

	deniedPaths := []string{
		filepath.Join(home, ".ssh/authorized_keys"),
		filepath.Join(home, ".ssh/config"),
		filepath.Join(home, ".aws/credentials"),
		filepath.Join(home, ".kube/config"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".netrc"),
		filepath.Join(home, ".gitconfig"),
		"/etc/sudoers",
		"/etc/passwd",
		"/etc/shadow",
	}

	for _, path := range deniedPaths {
		err := guard.CheckWritePath(path)
		if err == nil {
			t.Errorf("expected write to %q to be denied", path)
		}
	}
}

func TestSecurityGuard_CheckWritePath_DeniedPrefix(t *testing.T) {
	guard := NewSecurityGuard()
	home, _ := os.UserHomeDir()

	deniedPaths := []string{
		filepath.Join(home, ".ssh/random_file"),
		filepath.Join(home, ".aws/something"),
		filepath.Join(home, ".kube/my-config"),
		filepath.Join(home, ".gnupg/pubring.gpg"),
		filepath.Join(home, ".config/gcloud/credentials.json"),
		"/etc/sudoers.d/custom",
		"/etc/systemd/malicious.service",
	}

	for _, path := range deniedPaths {
		err := guard.CheckWritePath(path)
		if err == nil {
			t.Errorf("expected write to %q to be denied (prefix match)", path)
		}
	}
}

func TestSecurityGuard_CheckWritePath_Allowed(t *testing.T) {
	guard := NewSecurityGuard()

	allowedPaths := []string{
		"/tmp/test.txt",
		"/home/user/project/main.go",
		"/sandbox/workspace/app.py",
		"/var/log/app.log",
		"/home/user/.local/share/app/data.json",
	}

	for _, path := range allowedPaths {
		err := guard.CheckWritePath(path)
		if err != nil {
			t.Errorf("expected write to %q to be allowed, got: %v", path, err)
		}
	}
}

func TestSecurityGuard_CheckWritePath_RelativeExpands(t *testing.T) {
	guard := NewSecurityGuard()

	// Relative paths starting with .ssh should be denied after $HOME expansion
	err := guard.CheckWritePath(".ssh/authorized_keys")
	if err == nil {
		t.Error("expected relative path .ssh/authorized_keys to be denied after expansion")
	}
}

func TestSecurityGuard_CheckCommand_RedirectToSSHKey(t *testing.T) {
	guard := NewSecurityGuard()

	blocked := []string{
		"echo 'foo' > ~/.ssh/authorized_keys",
		"cat key.pub >> ~/.ssh/authorized_keys",
		"echo 'hack' > ~/.ssh/config",
		"echo data > ~/.aws/credentials",
	}

	for _, cmd := range blocked {
		err := guard.CheckCommand(cmd)
		if err == nil {
			t.Errorf("expected command %q to be blocked", cmd)
		}
	}
}

func TestSecurityGuard_CheckCommand_Allowed(t *testing.T) {
	guard := NewSecurityGuard()

	allowed := []string{
		"echo hello > /tmp/test.txt",
		"cat file.txt",
		"go build ./...",
		"git commit -m 'fix'",
		"pip install flask > /dev/null",
	}

	for _, cmd := range allowed {
		err := guard.CheckCommand(cmd)
		if err != nil {
			t.Errorf("expected command %q to be allowed, got: %v", cmd, err)
		}
	}
}

func TestSecurityGuard_DefaultDenyLists_Expanded(t *testing.T) {
	guard := NewSecurityGuard()
	home, _ := os.UserHomeDir()

	// Verify no $HOME left in the deny lists
	for k := range guard.denyExact {
		if strings.Contains(k, "$HOME") {
			t.Errorf("denyExact has unexpanded $HOME: %s", k)
		}
	}
	for _, p := range guard.denyPrefix {
		if strings.Contains(p, "$HOME") {
			t.Errorf("denyPrefix has unexpanded $HOME: %s", p)
		}
	}

	// Verify home dir is set
	if guard.homeDir == "" {
		t.Error("homeDir should be set")
	}
	if guard.homeDir != home {
		t.Errorf("homeDir = %q, want %q", guard.homeDir, home)
	}
}
