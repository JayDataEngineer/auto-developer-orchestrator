package testutil

import "go.uber.org/zap"

// NopLogger returns a zap.Logger that discards all output. Standardizes the
// zap.NewNop() usage across tests so handler setup is uniform.
func NopLogger() *zap.Logger {
	return zap.NewNop()
}
