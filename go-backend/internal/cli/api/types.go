package api

// Health response
type HealthResponse struct {
	Status string `json:"status"`
}

// Project responses
type ProjectListResponse struct {
	Projects []string `json:"projects"`
}

type ProjectAddRequest struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	RepoURL string `json:"repoUrl,omitempty"`
}

type ProjectAddResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Status response
type StatusResponse struct {
	Branch     string `json:"branch"`
	GitState   string `json:"gitState"`
	Modified   int    `json:"modified"`
	Staged     int    `json:"staged"`
	IsAutoMode bool   `json:"isAutoMode"`
	LastCommit string `json:"lastCommit"`
}

// Conversation / History
type ConversationSummary struct {
	Project      string `json:"project"`
	AgentID      string `json:"agentId"`
	LastMessage  string `json:"lastMessage"`
	LastAt       string `json:"lastAt"`
	MessageCount int    `json:"messageCount"`
	Title        string `json:"title"`
}

type HistoryResponse struct {
	Conversations []ConversationSummary `json:"conversations"`
}

// Prompt request
type PromptRequest struct {
	Message       string `json:"message"`
	Project       string `json:"project"`
	AgentID       string `json:"agentId,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	AutoBranch    bool   `json:"autoBranch,omitempty"`
}

// Approval request body (HTTP wire format with routing fields)
type ApprovalRequestBody struct {
	Project   string `json:"project"`
	AgentID   string `json:"agentId"`
	RequestID string `json:"requestId"`
	Action    string `json:"action"`    // approve, deny, answer
	Message   string `json:"message,omitempty"`
}

// Scheduler
type SchedulerJob struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	Project          string   `json:"project"`
	AgentID          string   `json:"agentId,omitempty"`
	Message          string   `json:"message"`
	ScheduleType     string   `json:"scheduleType"`
	CronExpr         string   `json:"cronExpr,omitempty"`
	EverySeconds     int      `json:"everySeconds,omitempty"`
	AtTime           string   `json:"atTime,omitempty"`
	Enabled          bool     `json:"enabled"`
	Status           string   `json:"status"`
	LastRunAt        string   `json:"lastRunAt,omitempty"`
	LastRunStatus    string   `json:"lastRunStatus,omitempty"`
	ConsecutiveErrors int     `json:"consecutiveErrors"`
	WebhookToken     string   `json:"webhookToken,omitempty"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
	Blocks           []string `json:"blocks,omitempty"`
	BlockedBy        []string `json:"blockedBy,omitempty"`
}

type SchedulerListResponse struct {
	Jobs []SchedulerJob `json:"jobs"`
}

type CreateJobRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Project       string `json:"project"`
	AgentID       string `json:"agentId,omitempty"`
	Message       string `json:"message"`
	ScheduleType  string `json:"scheduleType"`
	CronExpr      string `json:"cronExpr,omitempty"`
	EveryInterval string `json:"everyInterval,omitempty"`
	EverySeconds  int    `json:"everySeconds,omitempty"`
	AtTime        string `json:"atTime,omitempty"`
	AutoBranch    bool   `json:"autoBranch,omitempty"`
	AutoMerge     bool   `json:"autoMerge,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	Model         string `json:"model,omitempty"`
	Org           string `json:"org,omitempty"`
	Enabled       bool   `json:"enabled,omitempty"`
	Webhook       bool   `json:"webhook,omitempty"`
}

type SchedulerRun struct {
	ID        string `json:"id"`
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

type SchedulerRunsResponse struct {
	Runs []SchedulerRun `json:"runs"`
}

// Sandbox
type SandboxInfo struct {
	ID          string `json:"id"`
	ProjectPath string `json:"projectPath,omitempty"`
	Status      string `json:"status"`
	Image       string `json:"image,omitempty"`
}

type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// Artifact
type Artifact struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt"`
}

type ArtifactListResponse struct {
	Artifacts []Artifact `json:"artifacts"`
}

// Generic success response
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
