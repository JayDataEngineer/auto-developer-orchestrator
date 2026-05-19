package internal

// Version is set at build time via -ldflags "-X internal.Version=X.Y.Z".
// Defaults to "dev" for builds without ldflags (e.g. go run, go test).
var Version = "dev"
