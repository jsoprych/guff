package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Router maps model names to providers and dispatches requests.
// Model names can be prefixed with provider ("openai/gpt-4o") or configured
// explicitly in the model routing table.
type Router struct {
	mu        sync.RWMutex
	providers map[string]Provider           // provider name -> Provider
	routes    map[string]routeEntry         // model alias -> route
	fallback  Provider                      // default provider (usually local)
}

type routeEntry struct {
	provider  Provider
	modelName string // the actual model name to send to the provider
}

// NewRouter creates a new provider router.
func NewRouter() *Router {
	return &Router{
		providers: make(map[string]Provider),
		routes:    make(map[string]routeEntry),
	}
}

// RegisterProvider adds a provider to the router.
func (r *Router) RegisterProvider(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// SetFallback sets the default provider for unrouted model names.
func (r *Router) SetFallback(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = p
}

// AddRoute maps a model alias to a specific provider and (optionally different) model name.
// Example: AddRoute("claude-sonnet", anthropicProvider, "claude-sonnet-4-5-20250929")
func (r *Router) AddRoute(alias string, p Provider, modelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if modelName == "" {
		modelName = alias
	}
	r.routes[alias] = routeEntry{provider: p, modelName: modelName}
}

// Resolve finds the provider and actual model name for a given model string.
// Resolution order:
//  1. Explicit route table ("claude-sonnet" -> anthropic/claude-sonnet-4-5-20250929)
//  2. Provider prefix ("openai/gpt-4o" -> openai provider, model "gpt-4o")
//  3. Fallback provider (local)
func (r *Router) Resolve(model string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. Check route table
	if route, ok := r.routes[model]; ok {
		return route.provider, route.modelName, nil
	}

	// 2. Check for provider/ prefix
	if idx := strings.Index(model, "/"); idx > 0 {
		providerName := model[:idx]
		modelName := model[idx+1:]
		if p, ok := r.providers[providerName]; ok {
			return p, modelName, nil
		}
		return nil, "", fmt.Errorf("%w: provider %q", ErrProviderNotFound, providerName)
	}

	// 3. Fallback
	if r.fallback != nil {
		return r.fallback, model, nil
	}

	return nil, "", fmt.Errorf("%w: no provider for model %q", ErrProviderNotFound, model)
}

// ChatCompletion routes a request to the appropriate provider.
func (r *Router) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	p, modelName, err := r.Resolve(req.Model)
	if err != nil {
		return nil, err
	}
	req.Model = modelName
	return p.ChatCompletion(ctx, req)
}

// ChatCompletionStream routes a streaming request to the appropriate provider.
func (r *Router) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	p, modelName, err := r.Resolve(req.Model)
	if err != nil {
		return nil, err
	}
	req.Model = modelName
	return p.ChatCompletionStream(ctx, req)
}

// Embed routes an embedding request to the appropriate provider.
// The resolved provider must implement the Embedder interface.
func (r *Router) Embed(ctx context.Context, req EmbeddingRequest) ([]EmbeddingResult, error) {
	p, modelName, err := r.Resolve(req.Model)
	if err != nil {
		return nil, err
	}
	embedder, ok := p.(Embedder)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support embeddings", p.Name())
	}
	req.Model = modelName
	return embedder.Embed(ctx, req)
}

// ListModels returns models from all registered providers.
func (r *Router) ListModels(ctx context.Context) ([]ModelInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []ModelInfo
	seen := make(map[string]bool)

	for _, p := range r.providers {
		models, err := p.ListModels(ctx)
		if err != nil {
			continue // skip providers that fail
		}
		for _, m := range models {
			if !seen[m.ID] {
				seen[m.ID] = true
				all = append(all, m)
			}
		}
	}

	// Also include route aliases
	for alias, route := range r.routes {
		if !seen[alias] {
			seen[alias] = true
			all = append(all, ModelInfo{
				ID:       alias,
				Provider: route.provider.Name(),
				OwnedBy:  route.provider.Name(),
			})
		}
	}

	return all, nil
}
