package plugins

import (
	"strings"
	"testing"
	"time"
)

func TestGetPopularPlugins(t *testing.T) {
	plugins := GetPopularPlugins()

	if len(plugins) == 0 {
		t.Fatal("Expected popular plugins, got none")
	}

	// Check that we have some expected plugins
	pluginNames := make(map[string]bool)
	for _, plugin := range plugins {
		pluginNames[plugin.Name] = true

		// Validate structure
		if plugin.Name == "" {
			t.Error("Plugin name should not be empty")
		}
		if plugin.Version == "" {
			t.Error("Plugin version should not be empty")
		}
		if plugin.Description == "" {
			t.Error("Plugin description should not be empty")
		}

		// Version should start with 'v'
		if !strings.HasPrefix(plugin.Version, "v") {
			t.Errorf("Plugin %s version %s should start with 'v'", plugin.Name, plugin.Version)
		}
	}

	// Check for some expected popular plugins
	expectedPlugins := []string{"docker", "docker-compose", "cache"}
	for _, expected := range expectedPlugins {
		if !pluginNames[expected] {
			t.Errorf("Expected popular plugin '%s' not found", expected)
		}
	}
}

func TestCachedPluginSchema_IsExpired(t *testing.T) {
	now := time.Now()

	// Not expired
	notExpired := &CachedPluginSchema{
		CachedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}

	if notExpired.IsExpired() {
		t.Error("Schema should not be expired")
	}

	// Expired
	expired := &CachedPluginSchema{
		CachedAt:  now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}

	if !expired.IsExpired() {
		t.Error("Schema should be expired")
	}
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("Registry should not be nil")
	}

	if registry.plugins == nil {
		t.Error("Registry plugins map should be initialized")
	}

	if registry.cacheTTL != 24*time.Hour {
		t.Errorf("Expected default cache TTL of 24h, got %v", registry.cacheTTL)
	}

	if registry.maxRetries != 3 {
		t.Errorf("Expected default max retries of 3, got %d", registry.maxRetries)
	}
}

func TestNewRegistryWithTTL(t *testing.T) {
	customTTL := 2 * time.Hour
	registry := NewRegistryWithTTL(customTTL)

	if registry == nil {
		t.Fatal("Registry should not be nil")
	}

	if registry.cacheTTL != customTTL {
		t.Errorf("Expected custom cache TTL of %v, got %v", customTTL, registry.cacheTTL)
	}
}

func TestRegistry_ClearExpiredCache(t *testing.T) {
	registry := NewRegistry()
	now := time.Now()

	// Add some cached schemas - one expired, one not
	registry.plugins["expired"] = &CachedPluginSchema{
		Schema:    &PluginSchema{Name: "expired"},
		CachedAt:  now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}

	registry.plugins["valid"] = &CachedPluginSchema{
		Schema:    &PluginSchema{Name: "valid"},
		CachedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}

	// Should have 2 entries
	if len(registry.plugins) != 2 {
		t.Errorf("Expected 2 cached entries, got %d", len(registry.plugins))
	}

	// Clear expired cache
	registry.ClearExpiredCache()

	// Should have 1 entry left
	if len(registry.plugins) != 1 {
		t.Errorf("Expected 1 cached entry after clearing expired, got %d", len(registry.plugins))
	}

	// Should be the valid one
	if _, exists := registry.plugins["valid"]; !exists {
		t.Error("Valid cache entry should still exist")
	}

	if _, exists := registry.plugins["expired"]; exists {
		t.Error("Expired cache entry should be removed")
	}
}

func TestRegistry_GetCacheStats(t *testing.T) {
	registry := NewRegistry()
	now := time.Now()

	// Empty registry
	total, expired := registry.GetCacheStats()
	if total != 0 || expired != 0 {
		t.Errorf("Empty registry stats should be 0,0 got %d,%d", total, expired)
	}

	// Add some entries
	registry.plugins["expired1"] = &CachedPluginSchema{
		ExpiresAt: now.Add(-time.Hour),
	}

	registry.plugins["expired2"] = &CachedPluginSchema{
		ExpiresAt: now.Add(-30 * time.Minute),
	}

	registry.plugins["valid"] = &CachedPluginSchema{
		ExpiresAt: now.Add(time.Hour),
	}

	total, expired = registry.GetCacheStats()
	if total != 3 {
		t.Errorf("Expected total 3, got %d", total)
	}
	if expired != 2 {
		t.Errorf("Expected expired 2, got %d", expired)
	}
}

func TestRegistry_InvalidateCache(t *testing.T) {
	registry := NewRegistry()

	// Add a cached entry
	registry.plugins["test-plugin"] = &CachedPluginSchema{
		Schema: &PluginSchema{Name: "test"},
	}

	// Verify it exists
	if _, exists := registry.plugins["test-plugin"]; !exists {
		t.Fatal("Cache entry should exist before invalidation")
	}

	// Invalidate cache
	registry.InvalidateCache("test-plugin")

	// Verify it's gone
	if _, exists := registry.plugins["test-plugin"]; exists {
		t.Error("Cache entry should be removed after invalidation")
	}
}

func TestParsePluginFromStep(t *testing.T) {
	tests := []struct {
		name     string
		stepData map[string]interface{}
		expected []PluginReference
	}{
		{
			name:     "no plugins",
			stepData: map[string]interface{}{"label": "test"},
			expected: []PluginReference{},
		},
		{
			name: "single plugin",
			stepData: map[string]interface{}{
				"plugins": []interface{}{
					map[string]interface{}{
						"docker#v5.13.0": map[string]interface{}{
							"image": "node:18",
						},
					},
				},
			},
			expected: []PluginReference{
				{
					Name: "docker#v5.13.0",
					Config: map[string]interface{}{
						"image": "node:18",
					},
				},
			},
		},
		{
			name: "multiple plugins",
			stepData: map[string]interface{}{
				"plugins": []interface{}{
					map[string]interface{}{
						"docker#v5.13.0": map[string]interface{}{
							"image": "node:18",
						},
					},
					map[string]interface{}{
						"cache#v1.7.0": map[string]interface{}{
							"key": "cache-key",
						},
					},
				},
			},
			expected: []PluginReference{
				{
					Name: "docker#v5.13.0",
					Config: map[string]interface{}{
						"image": "node:18",
					},
				},
				{
					Name: "cache#v1.7.0",
					Config: map[string]interface{}{
						"key": "cache-key",
					},
				},
			},
		},
		{
			name: "invalid plugins format",
			stepData: map[string]interface{}{
				"plugins": "invalid",
			},
			expected: []PluginReference{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ParsePluginFromStep(test.stepData)

			if len(result) != len(test.expected) {
				t.Errorf("Expected %d plugins, got %d", len(test.expected), len(result))
				return
			}

			for i, expected := range test.expected {
				if i >= len(result) {
					t.Errorf("Missing plugin at index %d", i)
					continue
				}

				if result[i].Name != expected.Name {
					t.Errorf("Plugin %d name: expected %s, got %s", i, expected.Name, result[i].Name)
				}

				// Compare configs as strings (simple comparison)
				expectedStr := interfaceToString(expected.Config)
				resultStr := interfaceToString(result[i].Config)
				if expectedStr != resultStr {
					t.Errorf("Plugin %d config: expected %s, got %s", i, expectedStr, resultStr)
				}
			}
		})
	}
}

// Helper function to convert interface{} to string for comparison
func interfaceToString(v interface{}) string {
	if v == nil {
		return "nil"
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(stringify(v)), " ", ""), "\n", "")
}

func stringify(v interface{}) []byte {
	switch val := v.(type) {
	case map[string]interface{}:
		result := "{"
		for k, v := range val {
			result += k + ":" + string(stringify(v)) + ","
		}
		result += "}"
		return []byte(result)
	case string:
		return []byte(val)
	default:
		return []byte("")
	}
}
