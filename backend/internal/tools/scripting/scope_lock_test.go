package scripting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// responseFailed returns true if the response map indicates scripts.py
// rejected the call — either via an explicit `error` JSON field (scripts.py
// handled it gracefully) or via a non-zero exit_code (uncaught exception +
// traceback). Both paths are valid rejections.
//
// exit_code can arrive as int (from exec.ExitError.ExitCode() in the
// not-JSON branch) or float64 (from json.Unmarshal into map[string]any).
func responseFailed(m map[string]any) bool {
	if errMsg, _ := m["error"].(string); errMsg != "" {
		return true
	}
	switch v := m["exit_code"].(type) {
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

// TestSystemBScopeLockRejectsPathTraversal proves that scripts.py::_script_path
// rejects malicious script names before any file is created on disk.
//
// The scope lock is the regex `[A-Za-z_][A-Za-z0-9_]*` enforced in
// sandbox/scripts/scripts.py. It rejects path traversal (`../`),
// absolute paths (`/etc/passwd`), and nested paths (`foo/bar`).
//
// This is the PRIMARY enforcement of the two-tier Python separation:
// System B (agent-authored scratch) is restricted to SCRIPTS_DIR and
// cannot mutate System A (git-tracked backbone at /sandbox/<name>.py).
//
// The chmod 0444 on System A files is defense-in-depth — it fails open
// when the container runs as root (Docker default). The scope lock is
// what actually holds.
func TestSystemBScopeLockRejectsPathTraversal(t *testing.T) {
	cases := []struct {
		name        string
		scriptName  string
		description string
	}{
		{"parent_dir", "../../evil", "escape via parent dir"},
		{"single_parent", "..", "bare parent"},
		{"abs_path", "/etc/passwd", "absolute path"},
		{"nested_path", "foo/bar", "nested path"},
		{"leading_slash", "/sandbox/telegram_session", "target System A directly"},
		{"dot_segments", "x/../../../etc/shadow", "compound traversal"},
		{"dotfile", ".hidden", "leading dot"},
		{"shell_metachar", "evil; rm -rf /", "shell metacharacters"},
		{"null_byte", "evil\x00.txt", "null byte"},
		{"backslash", `evil\..\bar`, "windows-style traversal"},
		{"space_prefix", " evil", "leading space"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scriptsDir := withTempScriptsDir(t)

			// Make_script should reject this name BEFORE any file is touched.
			res, _ := MakeScriptTool{}.Execute(context.Background(), map[string]any{
				"name":        tc.scriptName,
				"description": tc.description,
				"code":        "print('pwned')",
			})
			m, ok := res.(map[string]any)
			if !ok {
				t.Fatalf("make_script returned non-map: %T", res)
			}
			if !responseFailed(m) {
				t.Errorf("make_script accepted malicious name %q without error: %v",
					tc.scriptName, m)
			}

			// The strongest guarantee: no file was created on disk at any
			// path derivable from the malicious name. Walk the temp dir
			// and assert nothing matching "evil" or "passwd" or "shadow"
			// showed up.
			visited := []string{}
			_ = filepath.Walk(scriptsDir, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				visited = append(visited, p)
				return nil
			})
			for _, p := range visited {
				if strings.Contains(p, "evil") || strings.Contains(p, "passwd") || strings.Contains(p, "shadow") {
					t.Errorf("malicious file leaked to disk after scope-lock rejection: %s", p)
				}
			}

			// Belt-and-suspenders: also confirm no file was created at the
			// literal target path (resolve ../etc/passwd relative to SCRIPTS_DIR).
			target := filepath.Join(scriptsDir, tc.scriptName+".py")
			if _, err := os.Stat(target); err == nil {
				t.Errorf("malicious file exists at resolved target path: %s", target)
			}
		})
	}
}

// TestSystemBScopeLockRejectsEditAndRun proves the scope lock also holds for
// --edit, --run, --show, --rm. The validator runs in _script_path(), which
// every operation calls first.
func TestSystemBScopeLockRejectsEditAndRun(t *testing.T) {
	withTempScriptsDir(t)

	malicious := "../../escape"

	for _, op := range []struct {
		tool string
		exec func() map[string]any
	}{
		{"run", func() map[string]any {
			r, _ := RunScriptTool{}.Execute(context.Background(), map[string]any{"name": malicious})
			m, _ := r.(map[string]any)
			return m
		}},
		{"edit", func() map[string]any {
			r, _ := EditScriptTool{}.Execute(context.Background(), map[string]any{
				"name": malicious,
				"code": "print('edited')",
			})
			m, _ := r.(map[string]any)
			return m
		}},
		{"show", func() map[string]any {
			r, _ := ShowScriptTool{}.Execute(context.Background(), map[string]any{"name": malicious})
			m, _ := r.(map[string]any)
			return m
		}},
		{"rm", func() map[string]any {
			r, _ := RemoveScriptTool{}.Execute(context.Background(), map[string]any{"name": malicious})
			m, _ := r.(map[string]any)
			return m
		}},
	} {
		t.Run(op.tool, func(t *testing.T) {
			m := op.exec()
			if m == nil {
				t.Fatalf("%s returned nil map", op.tool)
			}
			if !responseFailed(m) {
				t.Errorf("%s on malicious name %q returned success-shaped response: %v",
					op.tool, malicious, m)
			}
		})
	}
}

// TestSystemBScopeLockAllowsLegitNames proves the scope lock doesn't reject
// legitimate Python identifier names. This is the positive control for the
// negative tests above — without it, a regex that always returns False would
// pass the rejection tests vacuously.
func TestSystemBScopeLockAllowsLegitNames(t *testing.T) {
	cases := []string{
		"greet",
		"calculate_sum",
		"_private",
		"myHelper2",
		"fetchUserTweets123",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			withTempScriptsDir(t)
			res, _ := MakeScriptTool{}.Execute(context.Background(), map[string]any{
				"name":        name,
				"description": "test script",
				"code":        "print('hello')",
			})
			m, ok := res.(map[string]any)
			if !ok {
				t.Fatalf("make_script returned non-map: %T", res)
			}
			if errMsg, _ := m["error"].(string); errMsg != "" {
				t.Errorf("legitimate name %q rejected by scope lock: %s", name, errMsg)
			}
			if _, ok := m["created"]; !ok {
				t.Errorf("legitimate name %q was not created: %v", name, m)
			}
		})
	}
}

// TestSystemBHintsSectionRoundTrip proves that hints authored via make_script
// survive a read_script round-trip. This is the System B half of the
// scripts-as-skills substrate (PR3 §B3 of the zero-drift plan).
//
// If hints were silently dropped, the AvailableScriptsBlock prompt injection
// would show scripts without their usage guidance — the agent would have to
// re-read code to figure out how to call them, defeating the point of the
// hints field.
func TestSystemBHintsSectionRoundTrip(t *testing.T) {
	withTempScriptsDir(t)

	hints := "Use when greeting a user by name.\nReturns greeting string.\nPitfall: name is required, no default."
	res, _ := MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "greet_user",
		"description": "Greet a user by name.",
		"code":        "import sys; print(f'hello {sys.argv[1]}')",
		"hints":       hints,
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("make_script returned non-map: %T", res)
	}
	if _, ok := m["created"]; !ok {
		t.Fatalf("make_script did not create script: %v", m)
	}

	// read_script should return the hints verbatim.
	readRes, _ := ReadScriptTool{}.Execute(context.Background(), map[string]any{"name": "greet_user"})
	readMap, ok := readRes.(map[string]any)
	if !ok {
		t.Fatalf("read_script returned non-map: %T", readRes)
	}
	gotHints, _ := readMap["hints"].(string)
	if gotHints != hints {
		t.Errorf("hints did not round-trip.\nwant: %q\ngot:  %q", hints, gotHints)
	}
}

// TestSystemBScopeLockScriptsDirEnvOverride proves that PUX_SCRIPTS_DIR env
// actually controls where scripts.py writes. This is the testability escape
// hatch — without it, every test would pollute the project's real scripts dir.
//
// Corollary: scripts.py NEVER writes outside $PUX_SCRIPTS_DIR, regardless of
// the name passed in (TestSystemBScopeLockRejectsPathTraversal above covers
// the "name tries to escape" angle).
func TestSystemBScopeLockScriptsDirEnvOverride(t *testing.T) {
	scriptsDir := withTempScriptsDir(t)

	res, _ := MakeScriptTool{}.Execute(context.Background(), map[string]any{
		"name":        "scoped_script",
		"description": "should land in scriptsDir",
		"code":        "print('ok')",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("make_script returned non-map: %T", res)
	}
	created, _ := m["created"].(string)
	if created == "" {
		t.Fatalf("make_script did not report a created path: %v", m)
	}

	// Resolve both paths and confirm the script landed inside scriptsDir.
	createdAbs, err := filepath.Abs(created)
	if err != nil {
		t.Fatalf("abs created: %v", err)
	}
	scriptsDirAbs, err := filepath.Abs(scriptsDir)
	if err != nil {
		t.Fatalf("abs scriptsDir: %v", err)
	}
	if !strings.HasPrefix(createdAbs, scriptsDirAbs+string(filepath.Separator)) {
		t.Errorf("script created OUTSIDE PUX_SCRIPTS_DIR:\n  created:    %s\n  scriptsDir: %s",
			createdAbs, scriptsDirAbs)
	}
}

// TestSystemBScopeLockRejectsSymlinkEscape proves the symlink-escape guard
// in scripts.py::_script_path actually fires.
//
// The bypass: agent uses bash to `ln -s /sandbox/twitter_session.py
// /sandbox/workspace/scripts/hi.py` then calls `edit_script(name="hi")`.
// The regex name check passes (hi.py is a valid name), so without the
// symlink guard, write_text would follow the symlink and overwrite a
// System A backbone file. The chmod 0444 layer is bypassed because the
// container runs as root today.
//
// Fix: scripts.py refuses to operate on any path where is_symlink() is True
// OR whose realpath escapes SCRIPTS_DIR. This test creates the malicious
// symlink the way the agent would (os.Symlink) and asserts every operation
// (make/edit/run/show/rm) fails cleanly.
func TestSystemBScopeLockRejectsSymlinkEscape(t *testing.T) {
	scriptsDir := withTempScriptsDir(t)

	// Place a "System A backbone" outside SCRIPTS_DIR to stand in for
	// /sandbox/twitter_session.py. The symlink in SCRIPTS_DIR will point here.
	target := filepath.Join(filepath.Dir(scriptsDir), "fake_backbone.py")
	// Stub a fake backbone file with content the agent should NOT be able to
	// overwrite. Read at end of test to prove it stayed intact.
	originalContent := "# fake backbone — must remain unchanged\nprint('original')\n"
	if err := os.WriteFile(target, []byte(originalContent), 0644); err != nil {
		t.Fatalf("seed fake backbone: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(target) })

	// The malicious symlink: appears as a System B script, actually points
	// at the fake backbone.
	linkPath := filepath.Join(scriptsDir, "escape.py")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("create malicious symlink: %v", err)
	}

	// Every operation must fail. We're testing that scripts.py refuses to
	// touch the symlink, not that the operation itself fails for any other
	// reason.
	for _, op := range []struct {
		tool string
		exec func() map[string]any
	}{
		{"make", func() map[string]any {
			r, _ := MakeScriptTool{}.Execute(context.Background(), map[string]any{
				"name": "escape", "code": "print('pwned')",
				"description": "should fail",
			})
			m, _ := r.(map[string]any)
			return m
		}},
		{"edit", func() map[string]any {
			r, _ := EditScriptTool{}.Execute(context.Background(), map[string]any{
				"name": "escape", "code": "print('pwned')",
			})
			m, _ := r.(map[string]any)
			return m
		}},
		{"run", func() map[string]any {
			r, _ := RunScriptTool{}.Execute(context.Background(), map[string]any{
				"name": "escape", "timeout_seconds": 5,
			})
			m, _ := r.(map[string]any)
			return m
		}},
		{"show", func() map[string]any {
			r, _ := ShowScriptTool{}.Execute(context.Background(), map[string]any{"name": "escape"})
			m, _ := r.(map[string]any)
			return m
		}},
		{"rm", func() map[string]any {
			r, _ := RemoveScriptTool{}.Execute(context.Background(), map[string]any{"name": "escape"})
			m, _ := r.(map[string]any)
			return m
		}},
	} {
		t.Run(op.tool, func(t *testing.T) {
			m := op.exec()
			if m == nil {
				t.Fatalf("%s returned nil map", op.tool)
			}
			if !responseFailed(m) {
				t.Errorf("%s on symlink target returned success-shaped response: %v\n"+
					"The symlink-escape guard in scripts.py::_script_path must refuse "+
					"to operate on symlinks so an agent can't mutate System A backbone "+
					"files via a bash-created symlink.",
					op.tool, m)
			}
		})
	}

	// And the fake backbone file MUST be unchanged — proving none of the
	// operations slipped through and wrote to the symlink target.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read fake backbone after ops: %v", err)
	}
	if string(got) != originalContent {
		t.Errorf("fake backbone was MUTATED via symlink escape:\n  before: %q\n  after:  %q\n"+
			"One of the scripting operations wrote through the symlink — "+
			"the scope-lock symlink guard is broken.",
			originalContent, string(got))
	}
}
