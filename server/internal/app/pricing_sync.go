package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	litellmPricingURL     = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	pricingCacheTTL       = 1 * time.Hour
	pricingRequestTimeout = 30 * time.Second
)

// LiteLLMPricing represents a single model's pricing from the LiteLLM dataset.
type LiteLLMPricing struct {
	InputCostPerToken          float64 `json:"input_cost_per_token"`
	OutputCostPerToken         float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost    float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
}

// pricingCache holds the fetched LiteLLM pricing data in memory.
type pricingCache struct {
	mu       sync.RWMutex
	data     map[string]LiteLLMPricing
	fetched  time.Time
	loading  bool
}

var globalPricingCache = &pricingCache{}

// fetchLiteLLMPricing fetches the pricing dataset from LiteLLM's GitHub.
// Results are cached in memory for pricingCacheTTL.
func (pc *pricingCache) fetchLiteLLMPricing() (map[string]LiteLLMPricing, error) {
	pc.mu.RLock()
	if pc.data != nil && time.Since(pc.fetched) < pricingCacheTTL {
		data := pc.data
		pc.mu.RUnlock()
		return data, nil
	}
	isLoading := pc.loading
	pc.mu.RUnlock()

	if isLoading {
		return nil, fmt.Errorf("pricing fetch already in progress")
	}

	pc.mu.Lock()
	// Double-check after acquiring write lock
	if pc.data != nil && time.Since(pc.fetched) < pricingCacheTTL {
		data := pc.data
		pc.mu.Unlock()
		return data, nil
	}
	pc.loading = true
	pc.mu.Unlock()

	data, err := doFetchLiteLLMPricing()

	pc.mu.Lock()
	pc.loading = false
	if err == nil {
		pc.data = data
		pc.fetched = time.Now()
	}
	pc.mu.Unlock()

	return data, err
}

func doFetchLiteLLMPricing() (map[string]LiteLLMPricing, error) {
	client := &http.Client{Timeout: pricingRequestTimeout}
	resp, err := client.Get(litellmPricingURL)
	if err != nil {
		return nil, fmt.Errorf("fetch litellm pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("litellm pricing HTTP %d", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode litellm pricing: %w", err)
	}

	result := make(map[string]LiteLLMPricing, len(raw))
	for key, val := range raw {
		var p LiteLLMPricing
		if err := json.Unmarshal(val, &p); err != nil {
			continue // skip malformed entries
		}
		// Only include entries that have at least input or output pricing
		if p.InputCostPerToken > 0 || p.OutputCostPerToken > 0 {
			result[strings.ToLower(key)] = p
		}
	}
	return result, nil
}

// normalizeLiteLLMKey extracts the model name portion from a LiteLLM key.
// e.g. "anthropic/claude-sonnet-4-5" -> "claude-sonnet-4-5"
//
//	"bedrock/anthropic.claude-3-5-haiku-20241022-v1:0" -> "anthropic.claude-3-5-haiku-20241022-v1:0"
func normalizeLiteLLMKey(key string) string {
	lower := strings.ToLower(key)
	// Skip known provider prefixes that aren't part of the model name
	providerPrefixes := []string{
		"openai/", "anthropic/", "google/", "bedrock/", "vertex_ai/",
		"azure/", "azure_ai/", "openrouter/", "together/", "together_ai/",
		"fireworks_ai/", "groq/", "deepseek/", "meta-llama/", "mistralai/",
		"minimax/", "x-ai/", "xai/", "qwen/", "cohere/", "perplexity/",
	}
	for _, prefix := range providerPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimPrefix(lower, prefix)
		}
	}
	return lower
}

// matchPricing finds the best matching pricing for a model name.
// Tries exact match first, then provider-prefix-stripped match.
func matchPricing(modelName string, dataset map[string]LiteLLMPricing) (LiteLLMPricing, bool) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return LiteLLMPricing{}, false
	}

	// 1. Exact match
	if p, ok := dataset[lower]; ok {
		return p, true
	}

	// 2. Try matching after stripping common provider prefixes from model name
	for _, prefix := range []string{"openai/", "anthropic/", "google/"} {
		if strings.HasPrefix(lower, prefix) {
			stripped := strings.TrimPrefix(lower, prefix)
			if p, ok := dataset[stripped]; ok {
				return p, true
			}
		}
	}

	// 3. Try matching against normalized LiteLLM keys
	// (strip provider prefix from LiteLLM keys and compare)
	for key, p := range dataset {
		normalized := normalizeLiteLLMKey(key)
		if normalized == lower {
			return p, true
		}
	}

	return LiteLLMPricing{}, false
}

// PricingSyncResult holds the result of a pricing sync operation.
type PricingSyncResult struct {
	Synced  int      `json:"synced"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

// syncModelPricing fetches LiteLLM pricing and updates matching ModelConfig records.
func (a *App) syncModelPricing() (*PricingSyncResult, error) {
	dataset, err := globalPricingCache.fetchLiteLLMPricing()
	if err != nil {
		return nil, err
	}

	var models []ModelConfig
	if err := a.db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("load models: %w", err)
	}

	result := &PricingSyncResult{}
	for _, model := range models {
		pricing, ok := matchPricing(model.Name, dataset)
		if !ok {
			result.Skipped++
			continue
		}

		updates := map[string]any{}
		if pricing.InputCostPerToken > 0 {
			inputPrice := pricing.InputCostPerToken * 1_000_000 // convert per-token to per-million
			updates["input_price"] = inputPrice
			updates["billing_input"] = finalBillingPrice(inputPrice, model.InputMultiple)
		}
		if pricing.OutputCostPerToken > 0 {
			outputPrice := pricing.OutputCostPerToken * 1_000_000
			updates["output_price"] = outputPrice
			updates["billing_output"] = finalBillingPrice(outputPrice, model.OutputMultiple)
		}
		if pricing.CacheReadInputTokenCost > 0 {
			cacheReadPrice := pricing.CacheReadInputTokenCost * 1_000_000
			updates["cache_read_price"] = cacheReadPrice
			updates["billing_cache_read"] = finalBillingPrice(cacheReadPrice, model.CacheReadMultiple)
		}
		if pricing.CacheCreationInputTokenCost > 0 {
			cacheWritePrice := pricing.CacheCreationInputTokenCost * 1_000_000
			updates["cache_write_price"] = cacheWritePrice
			updates["billing_cache_write"] = finalBillingPrice(cacheWritePrice, model.CacheWriteMultiple)
		}

		if len(updates) > 0 {
			if err := a.db.Model(&ModelConfig{}).Where("id = ?", model.ID).Updates(updates).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("model %s: %v", model.Name, err))
				continue
			}
			result.Synced++
		} else {
			result.Skipped++
		}
	}

	return result, nil
}

// pricingStatus returns the current pricing cache status.
func (pc *pricingCache) status() gin.H {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return gin.H{
		"cached":     pc.data != nil,
		"modelCount": len(pc.data),
		"lastSync":   pc.fetched.Format(time.RFC3339),
		"ttlSeconds": int(pricingCacheTTL.Seconds()),
	}
}
