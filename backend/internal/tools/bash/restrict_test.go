package bash

import (
	"strings"
	"testing"
)

func TestValidate_BlockedCommands(t *testing.T) {
	v := NewDefaultValidator()

	blocked := []struct {
		cmd      string
		category string
	}{
		{"rm -rf /", "destruction"},
		{"rm -rf /var/log", "destruction"},
		{"rm -fr /", "destruction"},
		{"rm -Rf /etc", "destruction"},
		{"rm -rf /usr/local", "destruction"},
		{"rm -rf /bin", "destruction"},
		{"mkfs.ext4 /dev/sda", "destruction"},
		{"mkfs -t ext4 /dev/nvme0n1", "destruction"},
		{"dd if=/dev/zero of=/dev/sda bs=1M", "destruction"},
		{"sudo rm -rf /", "privilege"}, // hits sudo rm rule first
		{"sudo su", "privilege"},
		{"sudo su -", "privilege"},
		{"sudo bash", "privilege"},
		{"sudo sh", "privilege"},
		{"sudo chmod 777 /etc/passwd", "privilege"},
		{"chmod -R 777 /var/www", "privilege"},
		{"passwd", "privilege"},
		{"passwd root", "privilege"},
		{"nmap -sV 192.168.1.1", "network_attack"},
		{"nmap -sS target.local", "network_attack"},
		{"hydra -l admin -P wordlist.txt target", "network_attack"},
		{"aircrack-ng capture.cap", "network_attack"},
		{"netcat -e /bin/bash -l -p 4444", "network_attack"},
		{"nc -e /bin/sh -l -p 8080", "network_attack"},
		{"shred /dev/sda", "destruction"},
		{"echo hi && rm -rf /", "destruction"},
		{"echo hi; rm -rf /", "destruction"},
		{"echo hi && mkfs.ext4 /dev/sda", "destruction"},
		{":(){ :|:& };:", "destruction"},
	}

	for _, tc := range blocked {
		err := v.Validate(tc.cmd)
		if err == nil {
			t.Errorf("expected %q to be blocked (%s), but it was allowed", tc.cmd, tc.category)
			continue
		}
		blockedErr, ok := err.(*CommandBlockedError)
		if !ok {
			t.Errorf("expected *CommandBlockedError for %q, got %T: %v", tc.cmd, err, err)
			continue
		}
		if blockedErr.Category != tc.category {
			t.Errorf("expected category %q for %q, got %q", tc.category, tc.cmd, blockedErr.Category)
		}
	}
}

func TestValidate_AllowedCommands(t *testing.T) {
	v := NewDefaultValidator()

	allowed := []string{
		"rm -rf ./node_modules",
		"rm -rf build/",
		"rm -f /tmp/test.log",
		"rm -rf /tmp/scratch",
		"rm -rf /home/user/project/dist",
		"pip install requests",
		"git push origin main",
		"git commit -m 'fix bug'",
		"npm install express",
		"curl https://example.com",
		"wget https://example.com/file.tar.gz",
		"chmod 644 config.yaml",
		"chmod +x script.sh",
		"chown user:group file.txt",
		"docker build -t myapp .",
		"docker run -d nginx",
		"python3 script.py",
		"go test ./...",
		"echo 'hello world'",
		"cat /etc/hosts",
		"ls -la /var/log",
		"find . -name '*.go'",
		"grep -r 'pattern' src/",
		"sed -i 's/old/new/g' file.txt",
		"awk '{print $1}' data.txt",
		"tar -czf archive.tar.gz src/",
		"ps aux",
		"top -bn1",
		"df -h",
		"du -sh .",
		"make build",
		"cargo build --release",
		"bun install",
		"uv pip install flask",
		// Strings that contain blocked keywords but aren't in command position
		"echo 'use nmap to scan'",
		"echo 'hydra is a tool'",
		"echo 'passwd file location'",
		"echo 'mkfs command'",
		"grep 'passwd' /etc/shadow",
		"cat /etc/passwd",
	}

	for _, cmd := range allowed {
		err := v.Validate(cmd)
		if err != nil {
			t.Errorf("expected %q to be allowed, but got: %v", cmd, err)
		}
	}
}

func TestValidate_SudoOverrides(t *testing.T) {
	v := NewDefaultValidator()

	// These sudo commands should be allowed via override patterns
	allowed := []string{
		"sudo apt-get install nginx",
		"sudo apt update",
		"sudo apt install build-essential",
		"sudo yum install gcc",
		"sudo dnf update",
		"sudo systemctl restart nginx",
		"sudo service apache2 restart",
		"sudo docker ps",
		"sudo docker-compose up -d",
		"sudo pip install requests",
		"sudo npm install -g typescript",
		"sudo reboot",
	}

	for _, cmd := range allowed {
		err := v.Validate(cmd)
		if err != nil {
			t.Errorf("expected %q to be allowed via override, but got: %v", cmd, err)
		}
	}

	// These sudo commands should still be blocked
	blocked := []string{
		"sudo rm -rf /",
		"sudo rm -rf /var",
		"sudo su",
		"sudo bash",
		"sudo chmod 777 /etc/hosts",
	}

	for _, cmd := range blocked {
		err := v.Validate(cmd)
		if err == nil {
			t.Errorf("expected %q to be blocked even with overrides", cmd)
		}
	}
}

func TestValidate_PipeContext(t *testing.T) {
	v := NewDefaultValidator()

	// Dangerous commands in pipe chains should still be caught
	blocked := []string{
		"echo hi && rm -rf /",
		"echo hi; rm -rf /",
		"true || mkfs.ext4 /dev/sda",
		"cat file | shred /dev/sda",
	}

	for _, cmd := range blocked {
		err := v.Validate(cmd)
		if err == nil {
			t.Errorf("expected %q to be blocked in pipe context", cmd)
		}
	}
}

func TestValidate_CaseInsensitive(t *testing.T) {
	v := NewDefaultValidator()

	blocked := []string{
		"RM -RF /",
		"Sudo SU",
		"SUDO BASH",
		"NMAP -sV target",
		"MKFS /dev/sda",
	}

	for _, cmd := range blocked {
		err := v.Validate(cmd)
		if err == nil {
			t.Errorf("expected %q to be blocked (case insensitive)", cmd)
		}
	}
}

func TestValidate_LongCommand(t *testing.T) {
	v := NewDefaultValidator()

	// Very long command — error message should be truncated
	longCmd := "rm -rf /" + strings.Repeat(" extra_arg", 100)
	err := v.Validate(longCmd)
	if err == nil {
		t.Fatal("expected long rm -rf to be blocked")
	}
	errMsg := err.Error()
	if len(errMsg) > 500 {
		t.Errorf("error message too long (%d chars): %s", len(errMsg), errMsg[:200])
	}
}

func TestCommandBlockedError_Format(t *testing.T) {
	err := &CommandBlockedError{
		Command:  "rm -rf /",
		Category: "destruction",
		Message:  "recursive force-delete targeting root is not allowed",
	}
	msg := err.Error()
	if !strings.Contains(msg, "destruction") {
		t.Errorf("error should contain category, got: %s", msg)
	}
	if !strings.Contains(msg, "recursive") {
		t.Errorf("error should contain message, got: %s", msg)
	}
	if !strings.Contains(msg, "rm -rf /") {
		t.Errorf("error should contain command, got: %s", msg)
	}
}
