package sandbox

import (
	"strings"
	"testing"
)

func TestCacheVolumeName_Deterministic(t *testing.T) {
	a := cacheVolumeName("/home/ubuntu/my-project")
	b := cacheVolumeName("/home/ubuntu/my-project")
	if a != b {
		t.Fatalf("same path produced different volume names: %q vs %q", a, b)
	}
}

func TestCacheVolumeName_DistinctForDistinctPaths(t *testing.T) {
	cases := []struct {
		a, b string
	}{
		{"/home/ubuntu/project-a", "/home/ubuntu/project-b"},
		{"/home/ubuntu/a", "/home/ubuntu/b"},
		{"/path/one", "/path/two"},
	}
	for _, c := range cases {
		va := cacheVolumeName(c.a)
		vb := cacheVolumeName(c.b)
		if va == vb {
			t.Fatalf("distinct paths %q vs %q produced same volume %q", c.a, c.b, va)
		}
	}
}

func TestCacheVolumeName_PrefixAndLength(t *testing.T) {
	name := cacheVolumeName("/any/path")
	if !strings.HasPrefix(name, "pux-cache-") {
		t.Fatalf("missing pux-cache- prefix: %q", name)
	}
	// 16 hex chars after prefix
	suffix := strings.TrimPrefix(name, "pux-cache-")
	if len(suffix) != 16 {
		t.Fatalf("expected 16 hex chars after prefix, got %d in %q", len(suffix), name)
	}
	for _, r := range suffix {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("non-hex char %q in %q", r, name)
		}
	}
}

func TestCacheVolumeName_RelativePathResolved(t *testing.T) {
	// Same logical project, two relative paths that resolve to the same
	// absolute path, should produce the same volume name.
	a := cacheVolumeName(".")
	b := cacheVolumeName("././././.")
	if a != b {
		t.Fatalf("equivalent relative paths produced different names: %q vs %q", a, b)
	}
}

func TestCacheVolumeEnabled_DefaultTrue(t *testing.T) {
	t.Setenv(cacheVolumeDisabledEnv, "")
	if !cacheVolumeEnabled() {
		t.Fatal("expected cache volume enabled by default")
	}
}

func TestCacheVolumeEnabled_OptOut(t *testing.T) {
	t.Setenv(cacheVolumeDisabledEnv, "off")
	if cacheVolumeEnabled() {
		t.Fatal("expected cache volume disabled when PUX_CACHE_VOLUME=off")
	}
}

func TestCacheVolumeEnabled_OtherValuesStillEnabled(t *testing.T) {
	// Only the literal "off" opts out — no fuzzy matching, no surprise.
	for _, v := range []string{"false", "0", "no", "disabled"} {
		t.Setenv(cacheVolumeDisabledEnv, v)
		if !cacheVolumeEnabled() {
			t.Fatalf("PUX_CACHE_VOLUME=%q should still enable cache", v)
		}
	}
}
