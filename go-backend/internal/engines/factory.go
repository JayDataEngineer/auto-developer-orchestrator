package engines

import (
	"encoding/json"
	"os"
	"path/filepath"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/services"
	"go.uber.org/zap"
)

// Engines holds all initialized LLM engines.
// Only the Active engine is used for agent inference; others are available for model switching.
type Engines struct {
	Active      *llamaeng.LLMClient
	LlamaServer *llamaeng.LLMClient // local GPU engine
	Gemini      *llamaeng.LLMClient // optional Google Gemini cloud
	Cluster     *llamaeng.LLMClient // optional Ray cluster LLM
	OpenRouter  *llamaeng.LLMClient // optional OpenRouter cloud
}

// NewEngines creates and connects all LLM engines based on available configuration.
// Engines that fail to connect are set to nil (graceful degradation).
func NewEngines(logger *zap.Logger) *Engines {
	e := &Engines{}

	e.LlamaServer = createLocalEngine(logger)
	e.Gemini = createGeminiEngine(logger)
	e.Cluster = createClusterEngine(logger)
	e.OpenRouter = createOpenRouterEngine(logger)

	// Select active engine: local → cluster → Gemini → OpenRouter
	e.Active = selectEngine(e.LlamaServer, e.Cluster, e.Gemini, e.OpenRouter)
	if e.Active == nil {
		logger.Warn("No LLM engine available — agent features disabled")
	}

	return e
}

// Close shuts down all engines.
func (e *Engines) Close() {
	for _, eng := range []*llamaeng.LLMClient{
		e.LlamaServer, e.Gemini, e.Cluster, e.OpenRouter,
	} {
		if eng != nil {
			eng.Close()
		}
	}
}

func createLocalEngine(logger *zap.Logger) *llamaeng.LLMClient {
	url := os.Getenv("LLAMA_SERVER_URL")
	if url == "" {
		url = "http://localhost:8001"
	}
	modelAlias := readActiveModelAlias()

	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:   url,
		ModelName: modelAlias,
		Logger:    logger,
	})
	if err := eng.LoadModel(); err != nil {
		logger.Warn("llama-server not reachable — agent features disabled, sandbox/API only",
			zap.Error(err), zap.String("url", url))
		return nil
	}
	if err := eng.WarmUp(); err != nil {
		logger.Warn("Warm-up request failed (first prompt may be slow)", zap.Error(err))
	}
	logger.Info("llama-server HTTP engine connected", zap.String("url", url))
	return eng
}

func createGeminiEngine(logger *zap.Logger) *llamaeng.LLMClient {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		return nil
	}
	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai",
		APIKey:    key,
		ModelName: "gemini-3-flash-preview",
		Logger:    logger,
	})
	if err := eng.LoadModel(); err != nil {
		logger.Warn("Gemini engine failed to initialize", zap.Error(err))
		return nil
	}
	logger.Info("Gemini cloud engine connected", zap.String("model", "gemini-3-flash-preview"))
	return eng
}

func createClusterEngine(logger *zap.Logger) *llamaeng.LLMClient {
	if os.Getenv("CLUSTER_LLM_DISABLE") == "true" {
		return nil
	}
	url := os.Getenv("CLUSTER_LLM_URL")
	if url == "" {
		url = services.HubBase()
	}
	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:          url,
		ModelName:        "qwen3.6-27b-q5_k_s",
		DisableStreaming: true,
		Logger:           logger,
	})
	if err := eng.LoadModel(); err != nil {
		logger.Warn("Ray cluster LLM not reachable", zap.Error(err), zap.String("url", url))
		return nil
	}
	logger.Info("Ray cluster LLM engine connected",
		zap.String("url", url), zap.String("model", "qwen3.6-27b"))
	return eng
}

func createOpenRouterEngine(logger *zap.Logger) *llamaeng.LLMClient {
	type providerSettings struct {
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
		Models  []struct {
			ID string `json:"id"`
		} `json:"models"`
	}

	data, err := os.ReadFile(os.Getenv("HOME") + "/.pi/agent/settings.json")
	if err != nil {
		return nil
	}

	var settings struct {
		Providers map[string]providerSettings `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}

	or, ok := settings.Providers["openrouter"]
	if !ok || or.APIKey == "" {
		return nil
	}

	modelID := "deepseek/deepseek-v4-flash"
	if len(or.Models) > 0 && or.Models[0].ID != "" {
		modelID = or.Models[0].ID
	}

	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:   or.BaseURL,
		APIKey:    or.APIKey,
		ModelName: modelID,
		Logger:    logger,
	})
	if err := eng.LoadModel(); err != nil {
		logger.Warn("OpenRouter engine failed to initialize", zap.Error(err))
		return nil
	}
	logger.Info("OpenRouter cloud engine connected", zap.String("model", modelID))
	return eng
}

// selectEngine returns the first non-nil engine from candidates.
func selectEngine(candidates ...*llamaeng.LLMClient) *llamaeng.LLMClient {
	for _, e := range candidates {
		if e != nil {
			return e
		}
	}
	return nil
}

// readActiveModelAlias determines the model alias to send in API requests.
// Priority: /tmp/orchestrator-model.json → config/models.json → "gemma-4-26b".
func readActiveModelAlias() string {
	if data, err := os.ReadFile("/tmp/orchestrator-model.json"); err == nil {
		var m struct {
			Alias string `json:"alias"`
		}
		if json.Unmarshal(data, &m) == nil && m.Alias != "" {
			return m.Alias
		}
	}

	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		cfgPath := filepath.Join(root, "config", "models.json")
		if data, err := os.ReadFile(cfgPath); err == nil {
			var cfg struct {
				Current string `json:"current"`
				Models  map[string]struct {
					Alias string `json:"alias"`
				} `json:"models"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Current != "" {
				if m, ok := cfg.Models[cfg.Current]; ok && m.Alias != "" {
					return m.Alias
				}
			}
		}
	}

	return "gemma-4-26b"
}
