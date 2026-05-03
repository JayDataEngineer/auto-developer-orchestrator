package handlers

// This file previously contained adapter types (bashExecutorAdapter, fileOpsAdapter,
// gitOpsAdapter, approvalManagerAdapter) that have been extracted to the shared
// internal/adapters/ package. The handler wiring now uses adapters.BashExecutor,
// adapters.FileOps, adapters.GitExecutor, and adapters.ApprovalHandler directly.
//
// See: go-backend/internal/adapters/adapters.go
