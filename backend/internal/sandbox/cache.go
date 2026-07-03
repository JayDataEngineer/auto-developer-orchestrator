package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// CacheVolumeEnabled toggles the per-project cache volume on/off.
// Default on (env "off" disables). Operators may disable for debugging
// stale-cache issues without code changes.
const cacheVolumeDisabledEnv = "PUX_CACHE_VOLUME"
const cacheVolumeDisabledValue = "off"

// CacheMountTarget is the in-container path the cache volume is mounted at.
// Covers pip (~/.cache/pip), huggingface (~/.cache/huggingface), and any
// other tool that respects XDG_CACHE_HOME or defaults to ~/.cache. npm's
// ~/.npm is NOT covered here — modern npm uses ~/.cache via --cache flag.
const CacheMountTarget = "/root/.cache"

// cacheVolumeName derives a deterministic Docker volume name from the
// absolute project path. Same project → same volume → same cached wheels
// and model weights across sessions. Different projects → different
// volumes → no cross-project bleed.
//
// Uses sha256 truncated to 16 hex chars (64 bits) to keep the name short
// while making collisions astronomically unlikely (2^32 project paths
// before 50% collision probability).
func cacheVolumeName(projectPath string) string {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}
	h := sha256.Sum256([]byte(abs))
	return "pux-cache-" + hex.EncodeToString(h[:])[:16]
}

// cacheVolumeEnabled checks the env opt-out. Default true.
func cacheVolumeEnabled() bool {
	return os.Getenv(cacheVolumeDisabledEnv) != cacheVolumeDisabledValue
}

// ensureCacheVolume creates the named volume if it doesn't already exist.
// Idempotent — VolumeInspect returns no error when the volume exists.
// Returns the volume name on success. Errors are logged + returned so the
// caller can decide whether to proceed without cache (yes) or fail loud (no).
//
// The volume is created WITHOUT labels/tags since Docker volume labels are
// advisory and add nothing here. Lifecycle (GC of stale volumes) is a
// separate concern; operators prune with `docker volume ls | grep pux-cache-`.
func ensureCacheVolume(ctx context.Context, dockerClient client.VolumeAPIClient, projectPath string, logger *zap.Logger) (string, error) {
	if !cacheVolumeEnabled() {
		return "", nil
	}
	name := cacheVolumeName(projectPath)
	if _, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); err == nil {
		return name, nil
	}
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   name,
		Driver: "local",
	}); err != nil {
		return "", fmt.Errorf("create cache volume %s: %w", name, err)
	}
	if logger != nil {
		logger.Info("Created per-project cache volume",
			zap.String("volume", name),
			zap.String("mount", CacheMountTarget))
	}
	return name, nil
}
