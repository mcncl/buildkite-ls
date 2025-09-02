package plugins

import (
	"testing"
)

func TestParsePluginReference(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ParsedPluginRef
	}{
		{
			name:  "official_plugin_with_version",
			input: "docker#v5.13.0",
			expected: &ParsedPluginRef{
				Org:     "buildkite-plugins",
				Name:    "docker",
				Version: "v5.13.0",
				FullRef: "docker#v5.13.0",
			},
		},
		{
			name:  "official_plugin_without_version",
			input: "docker",
			expected: &ParsedPluginRef{
				Org:     "buildkite-plugins",
				Name:    "docker",
				Version: "latest",
				FullRef: "docker",
			},
		},
		{
			name:  "custom_org_with_version",
			input: "mcncl/foo#v3.0.0",
			expected: &ParsedPluginRef{
				Org:     "mcncl",
				Name:    "foo",
				Version: "v3.0.0",
				FullRef: "mcncl/foo#v3.0.0",
			},
		},
		{
			name:  "custom_org_without_version",
			input: "company/internal",
			expected: &ParsedPluginRef{
				Org:     "company",
				Name:    "internal",
				Version: "latest",
				FullRef: "company/internal",
			},
		},
		{
			name:  "complex_version_tag",
			input: "buildkite/test#v1.2.3-alpha",
			expected: &ParsedPluginRef{
				Org:     "buildkite",
				Name:    "test",
				Version: "v1.2.3-alpha",
				FullRef: "buildkite/test#v1.2.3-alpha",
			},
		},
		{
			name:     "empty_input",
			input:    "",
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ParsePluginReference(test.input)

			if test.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("Expected result, got nil")
			}

			if result.Org != test.expected.Org {
				t.Errorf("Org: expected %s, got %s", test.expected.Org, result.Org)
			}

			if result.Name != test.expected.Name {
				t.Errorf("Name: expected %s, got %s", test.expected.Name, result.Name)
			}

			if result.Version != test.expected.Version {
				t.Errorf("Version: expected %s, got %s", test.expected.Version, result.Version)
			}

			if result.FullRef != test.expected.FullRef {
				t.Errorf("FullRef: expected %s, got %s", test.expected.FullRef, result.FullRef)
			}
		})
	}
}

func TestParsedPluginRef_GetRepositoryURL(t *testing.T) {
	tests := []struct {
		name     string
		plugin   *ParsedPluginRef
		expected string
	}{
		{
			name: "official_plugin",
			plugin: &ParsedPluginRef{
				Org:  "buildkite-plugins",
				Name: "docker",
			},
			expected: "https://github.com/buildkite-plugins/docker-buildkite-plugin",
		},
		{
			name: "custom_org",
			plugin: &ParsedPluginRef{
				Org:  "mcncl",
				Name: "foo",
			},
			expected: "https://github.com/mcncl/foo-buildkite-plugin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.plugin.GetRepositoryURL()
			if result != test.expected {
				t.Errorf("Expected %s, got %s", test.expected, result)
			}
		})
	}
}

func TestParsedPluginRef_GetAllSchemaURLs(t *testing.T) {
	plugin := &ParsedPluginRef{
		Org:     "mcncl",
		Name:    "foo",
		Version: "v3.0.0",
	}

	urls := plugin.GetAllSchemaURLs()

	expected := []string{
		"https://raw.githubusercontent.com/mcncl/foo-buildkite-plugin/v3.0.0/plugin.yml",
		"https://raw.githubusercontent.com/mcncl/foo-buildkite-plugin/main/plugin.yml",
		"https://raw.githubusercontent.com/mcncl/foo-buildkite-plugin/master/plugin.yml",
	}

	if len(urls) != len(expected) {
		t.Fatalf("Expected %d URLs, got %d", len(expected), len(urls))
	}

	for i, expectedURL := range expected {
		if urls[i] != expectedURL {
			t.Errorf("URL %d: expected %s, got %s", i, expectedURL, urls[i])
		}
	}
}
