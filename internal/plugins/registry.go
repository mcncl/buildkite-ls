package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// PopularPlugin represents a commonly used plugin with its latest version
type PopularPlugin struct {
	Name        string
	Version     string
	Description string
}

// GetPopularPlugins returns a list of the most commonly used Buildkite plugins
func GetPopularPlugins() []PopularPlugin {
	return []PopularPlugin{
		{"docker", "v5.13.0", "Run build steps in Docker containers"},
		{"docker-compose", "v5.10.0", "Run build steps with Docker Compose"},
		{"cache", "v1.7.0", "Cache files between builds"},
		{"artifacts", "v1.9.4", "Upload and download build artifacts"},
		{"test-collector", "v1.11.0", "Collect and analyze test results"},
		{"junit-annotate", "v2.7.0", "Annotate builds with JUnit test results"},
		{"shellcheck", "v1.4.0", "Run ShellCheck on shell scripts"},
		{"ecr", "v2.10.0", "Build and push Docker images to AWS ECR"},
		{"monorepo-diff", "v1.5.1", "Skip builds for unchanged parts of monorepos"},
		{"plugin-linter", "v3.3.0", "Lint Buildkite plugins"},
		{"docker-login", "v3.0.0", "Log in to Docker registries"},
	}
}

type PluginSchema struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Author        string         `yaml:"author"`
	Requirements  []string       `yaml:"requirements"`
	Configuration map[string]any `yaml:"configuration"`
	SchemaData    []byte
}

// CachedPluginSchema wraps a plugin schema with cache metadata
type CachedPluginSchema struct {
	Schema    *PluginSchema
	CachedAt  time.Time
	ExpiresAt time.Time
}

// IsExpired checks if the cached schema has expired
func (c *CachedPluginSchema) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

type Registry struct {
	mu         sync.RWMutex
	plugins    map[string]*CachedPluginSchema // Cache with expiration
	cacheTTL   time.Duration                  // How long to cache schemas
	maxRetries int                            // Maximum retry attempts for failed requests
}

func NewRegistry() *Registry {
	return &Registry{
		plugins:    make(map[string]*CachedPluginSchema),
		cacheTTL:   24 * time.Hour,
		maxRetries: 3,
	}
}

// NewRegistryWithTTL creates a registry with custom cache TTL
func NewRegistryWithTTL(ttl time.Duration) *Registry {
	return &Registry{
		plugins:    make(map[string]*CachedPluginSchema),
		cacheTTL:   ttl,
		maxRetries: 3,
	}
}

func (r *Registry) GetPluginSchema(pluginName string) (*PluginSchema, error) {
	r.mu.RLock()
	if cached, exists := r.plugins[pluginName]; exists {
		// Check if cache is still valid
		if !cached.IsExpired() {
			r.mu.RUnlock()
			return cached.Schema, nil
		}
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, exists := r.plugins[pluginName]; exists && !cached.IsExpired() {
		return cached.Schema, nil
	}

	// Cache is expired or doesn't exist, fetch new schema
	schema, err := r.fetchPluginSchema(pluginName)
	if err != nil {
		return nil, err
	}

	// Cache the schema with expiration
	now := time.Now()
	r.plugins[pluginName] = &CachedPluginSchema{
		Schema:    schema,
		CachedAt:  now,
		ExpiresAt: now.Add(r.cacheTTL),
	}

	return schema, nil
}

func (r *Registry) fetchPluginSchema(pluginName string) (*PluginSchema, error) {
	// Parse the plugin reference to get org/name/version
	parsed := ParsePluginReference(pluginName)
	if parsed == nil {
		return nil, fmt.Errorf("invalid plugin reference: %s", pluginName)
	}

	// Get all possible URLs to try
	urls := parsed.GetAllSchemaURLs()

	var lastErr error
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}

		schemaBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var schema PluginSchema
		if err := yaml.Unmarshal(schemaBytes, &schema); err != nil {
			lastErr = err
			continue
		}

		// Store schema data if configuration exists
		if schema.Configuration != nil {
			configJSON, err := json.Marshal(schema.Configuration)
			if err != nil {
				lastErr = err
				continue
			}
			schema.SchemaData = configJSON
		}

		return &schema, nil
	}

	return nil, fmt.Errorf("failed to fetch plugin schema for %s (org: %s, name: %s, version: %s): %v",
		pluginName, parsed.Org, parsed.Name, parsed.Version, lastErr)
}

// ClearExpiredCache removes expired entries from the cache
func (r *Registry) ClearExpiredCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, cached := range r.plugins {
		if cached.IsExpired() {
			delete(r.plugins, key)
		}
	}
}

// GetCacheStats returns cache statistics
func (r *Registry) GetCacheStats() (total, expired int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total = len(r.plugins)
	for _, cached := range r.plugins {
		if cached.IsExpired() {
			expired++
		}
	}

	return total, expired
}

// InvalidateCache removes a specific plugin from the cache
func (r *Registry) InvalidateCache(pluginName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, pluginName)
}

func (r *Registry) ValidatePluginConfig(pluginName string, config interface{}) error {
	schema, err := r.GetPluginSchema(pluginName)
	if err != nil {
		return fmt.Errorf("failed to get schema for plugin %s: %w", pluginName, err)
	}

	if schema.SchemaData == nil {
		// No schema defined, so no validation needed
		return nil
	}

	// Convert config to JSON for validation
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("plugin %s config serialization failed: %w", pluginName, err)
	}

	// Use the same validation library as main schema
	schemaLoader := gojsonschema.NewBytesLoader(schema.SchemaData)
	documentLoader := gojsonschema.NewBytesLoader(configJSON)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("plugin %s validation failed: %w", pluginName, err)
	}

	if !result.Valid() {
		// Return the first validation error
		if len(result.Errors()) > 0 {
			return fmt.Errorf("plugin %s configuration error: %s", pluginName, result.Errors()[0].Description())
		}
		return fmt.Errorf("plugin %s configuration is invalid", pluginName)
	}

	return nil
}

// ParsePluginFromStep extracts plugin information from a pipeline step
func ParsePluginFromStep(stepData map[string]interface{}) []PluginReference {
	var plugins []PluginReference

	if pluginsData, exists := stepData["plugins"]; exists {
		if pluginsList, ok := pluginsData.([]interface{}); ok {
			for _, pluginItem := range pluginsList {
				if pluginMap, ok := pluginItem.(map[string]interface{}); ok {
					for pluginName, config := range pluginMap {
						plugins = append(plugins, PluginReference{
							Name:   pluginName,
							Config: config,
						})
					}
				}
			}
		}
	}

	return plugins
}

type PluginReference struct {
	Name   string
	Config interface{}
}

// ParsedPluginRef represents a parsed plugin reference with org/name/version
type ParsedPluginRef struct {
	Org     string // GitHub organization (e.g., "buildkite-plugins", "mcncl")
	Name    string // Plugin name without suffix (e.g., "docker", "foo")
	Version string // Version tag (e.g., "v5.13.0", "latest")
	FullRef string // Original reference (e.g., "docker#v5.13.0", "mcncl/foo#v3.0.0")
}

// ParsePluginReference parses plugin references into components
// Examples:
//
//	"docker#v5.13.0" -> {Org: "buildkite-plugins", Name: "docker", Version: "v5.13.0"}
//	"mcncl/foo#v3.0.0" -> {Org: "mcncl", Name: "foo", Version: "v3.0.0"}
//	"company/internal#latest" -> {Org: "company", Name: "internal", Version: "latest"}
func ParsePluginReference(ref string) *ParsedPluginRef {
	if ref == "" {
		return nil
	}

	parsed := &ParsedPluginRef{FullRef: ref}

	// Split on # to separate plugin name from version
	parts := strings.SplitN(ref, "#", 2)
	pluginPart := parts[0]

	if len(parts) == 2 {
		parsed.Version = parts[1]
	} else {
		parsed.Version = "latest" // Default version
	}

	// Check if plugin contains org (has a slash)
	if strings.Contains(pluginPart, "/") {
		orgParts := strings.SplitN(pluginPart, "/", 2)
		parsed.Org = orgParts[0]
		parsed.Name = orgParts[1]
	} else {
		// Default to buildkite-plugins org for official plugins
		parsed.Org = "buildkite-plugins"
		parsed.Name = pluginPart
	}

	return parsed
}

// GetRepositoryURL returns the GitHub repository URL for this plugin
func (p *ParsedPluginRef) GetRepositoryURL() string {
	return fmt.Sprintf("https://github.com/%s/%s-buildkite-plugin", p.Org, p.Name)
}

// GetSchemaURL returns the plugin.yml URL for fetching schema
func (p *ParsedPluginRef) GetSchemaURL() string {
	// Try version-specific branch first, then fall back to main/master
	urls := []string{
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/%s/plugin.yml", p.Org, p.Name, p.Version),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/main/plugin.yml", p.Org, p.Name),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/master/plugin.yml", p.Org, p.Name),
	}
	return urls[0] // Return primary URL, fetchPluginSchema will try fallbacks
}

// GetAllSchemaURLs returns all possible URLs to try for fetching the schema
func (p *ParsedPluginRef) GetAllSchemaURLs() []string {
	return []string{
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/%s/plugin.yml", p.Org, p.Name, p.Version),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/main/plugin.yml", p.Org, p.Name),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s-buildkite-plugin/master/plugin.yml", p.Org, p.Name),
	}
}
