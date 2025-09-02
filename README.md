# buildkite-ls

A comprehensive Language Server Protocol (LSP) implementation for Buildkite pipeline files, providing rich IDE features for productive pipeline development.

## 🎯 Features

### Core LSP Features
- ✅ **Hover Documentation** - Rich contextual help with Markdown formatting for all pipeline properties and plugins
- ✅ **Document Symbols** - Hierarchical pipeline navigation with step detection for quick navigation
- ✅ **Smart Autocompletion** - Context-aware suggestions for properties, plugins, and step types with snippets
- ✅ **Enhanced Diagnostics** - Multi-level validation with precise error locations and actionable messages
- ✅ **Signature Help** - Contextual parameter hints for step types and plugin configurations  
- ✅ **Go-to-Definition** - Navigate to step definitions from `depends_on` references
- ✅ **Code Actions** - Quick fixes for common issues (add missing labels, fix empty commands, etc.)
- ✅ **Semantic Highlighting** - Rich syntax highlighting for step types, properties, and plugin names

### Validation & Schema Support
- ✅ **YAML Validation** - Real-time YAML syntax validation
- ✅ **Schema Validation** - Official Buildkite pipeline schema validation
- ✅ **Plugin Validation** - Dynamic validation of 200+ plugin configurations from the Buildkite Plugin Directory
- ✅ **Smart File Detection** - Automatically activates for `.buildkite/` files and common pipeline patterns

## 🚀 Installation

### Option 1: Mason (Recommended for Neovim)

Install using [Mason.nvim](https://github.com/williamboman/mason.nvim):

```lua
-- Install via Mason
:MasonInstall buildkite-ls

-- Or add to your Mason setup
require('mason').setup()
require('mason-lspconfig').setup({
  ensure_installed = { 'buildkite_ls' }
})
```

### Option 2: Pre-built Binaries

Download from [GitHub Releases](https://github.com/mcncl/buildkite-ls/releases):

```bash
# Linux/macOS
curl -L https://github.com/mcncl/buildkite-ls/releases/latest/download/buildkite-ls_linux_amd64.tar.gz | tar xz
sudo mv buildkite-ls /usr/local/bin/

# Or use the install script
curl -fsSL https://raw.githubusercontent.com/mcncl/buildkite-ls/main/install.sh | bash
```

### Option 3: From Source

```bash
git clone https://github.com/mcncl/buildkite-ls.git
cd buildkite-ls
go build -o buildkite-ls ./main.go

# Install to PATH (optional)
sudo mv buildkite-ls /usr/local/bin/
```

## ⚙️ Editor Configuration

### Neovim Setup

#### Option 1: Mason + nvim-lspconfig (Recommended)

First install via Mason, then configure:

```lua
-- ~/.config/nvim/lua/buildkite-ls.lua or in your init.lua

-- Install and setup Mason
require('mason').setup()
require('mason-lspconfig').setup({
  ensure_installed = { 'buildkite_ls' }
})

local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

-- Add buildkite-ls to lspconfig
if not configs.buildkite_ls then
  configs.buildkite_ls = {
    default_config = {
      cmd = { 'buildkite-ls' },
      filetypes = { 'yaml' },
      root_dir = lspconfig.util.root_pattern('.buildkite', 'pipeline.yml', 'pipeline.yaml', '.git'),
      settings = {},
      single_file_support = true,
    },
  }
end

-- Setup the language server
lspconfig.buildkite_ls.setup {
  on_attach = function(client, bufnr)
    -- Enable semantic tokens if supported
    if client.server_capabilities.semanticTokensProvider then
      vim.lsp.semantic_tokens.start(bufnr, client.id)
    end
  end,
}
```

#### Option 2: Manual nvim-lspconfig (if not using Mason)

```lua
local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

-- Add buildkite-ls to lspconfig
if not configs.buildkite_ls then
  configs.buildkite_ls = {
    default_config = {
      cmd = { 'buildkite-ls' },
      filetypes = { 'yaml' },
      root_dir = lspconfig.util.root_pattern('.buildkite', 'pipeline.yml', 'pipeline.yaml', '.git'),
      settings = {},
      single_file_support = true,
    },
  }
end

-- Setup the language server
lspconfig.buildkite_ls.setup {
  on_attach = function(client, bufnr)
    -- Enable semantic tokens if supported
    if client.server_capabilities.semanticTokensProvider then
      vim.lsp.semantic_tokens.start(bufnr, client.id)
    end
  end,
}
```

#### Option 3: LazyVim

```lua
-- ~/.config/nvim/lua/plugins/buildkite.lua
return {
  {
    "neovim/nvim-lspconfig",
    opts = {
      servers = {
        buildkite_ls = {
          cmd = { "buildkite-ls" },
          filetypes = { "yaml" },
          root_dir = require("lspconfig.util").root_pattern(".buildkite", "pipeline.yml", "pipeline.yaml", ".git"),
          single_file_support = true,
        },
      },
    },
  },
}
```

### Semantic Highlighting Configuration

#### For Catppuccin Theme Users

Enable semantic tokens in your Catppuccin configuration:

```lua
require("catppuccin").setup({
    integrations = {
        native_lsp = {
            enabled = true,
            virtual_text = {
                errors = { "italic" },
                hints = { "italic" },
                warnings = { "italic" },
                information = { "italic" },
            },
            underlines = {
                errors = { "underline" },
                hints = { "underline" },
                warnings = { "underline" },
                information = { "underline" },
            },
        },
        semantic_tokens = true,
    }
})
```

#### Custom Semantic Highlighting Colors 🎨

If you want to customize semantic token colors for Buildkite files:

```lua
-- Define custom semantic token highlights
vim.api.nvim_create_autocmd("ColorScheme", {
  pattern = "*",
  callback = function()
    -- Buildkite-specific semantic token colors
    vim.api.nvim_set_hl(0, "@lsp.type.keyword", { fg = "#f38ba8", bold = true }) -- Step types (command, wait, block)
    vim.api.nvim_set_hl(0, "@lsp.type.string", { fg = "#a6e3a1" })              -- String values
    vim.api.nvim_set_hl(0, "@lsp.type.property", { fg = "#89b4fa" })            -- YAML properties
    vim.api.nvim_set_hl(0, "@lsp.type.variable", { fg = "#f9e2af" })            -- Environment variables
    vim.api.nvim_set_hl(0, "@lsp.type.function", { fg = "#cba6f7" })            -- Plugin names
    vim.api.nvim_set_hl(0, "@lsp.type.namespace", { fg = "#fab387" })           -- Step keys and labels
    vim.api.nvim_set_hl(0, "@lsp.type.operator", { fg = "#89dceb" })            -- YAML operators (:, -)
    vim.api.nvim_set_hl(0, "@lsp.type.comment", { fg = "#6c7086", italic = true }) -- Comments
  end,
})
```

#### Semantic Token Types Reference

The language server provides these semantic token types for rich highlighting:

| Token Type | Used For | Example |
|------------|----------|---------|
| `keyword` | Step types and boolean values | `command:`, `wait:`, `true`, `false` |
| `string` | Labels, commands, and string values | `"Build App"`, `"make test"` |
| `property` | YAML property keys | `timeout_in_minutes:`, `branches:` |
| `variable` | Environment variables | `env:`, env values |
| `function` | Plugin names with versions | `docker#v5.13.0`, `cache#v2.4.10` |
| `namespace` | Step identifiers | `key:`, `label:` values |
| `operator` | YAML structural elements | `:`, `-`, `\|` |
| `comment` | YAML comments | `# This is a comment` |

### VS Code Setup

Create a VS Code extension or add to your `settings.json`:

```json
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/buildkite/pipeline-schema/main/schema.json": [
      "pipeline.yml",
      "pipeline.yaml", 
      ".buildkite/pipeline.yml",
      ".buildkite/pipeline.yaml"
    ]
  }
}
```

*Note: Full VS Code extension coming soon.*

## 🎯 What You Get

### Rich Code Intelligence

**Hover Documentation**: Get detailed help for any pipeline property:
```yaml
steps:
  - label: "Build"           # Hover shows: Human-readable name for the step
    command: "make build"    # Hover shows: Shell command(s) to execute
    timeout_in_minutes: 30   # Hover shows: Maximum time the step can run
```

**Smart Autocompletion**: Context-aware suggestions:
- Top-level properties (`steps`, `env`, `agents`)
- Step properties (`label`, `command`, `plugins`, `depends_on`)
- Plugin names with versions (`docker#v5.13.0`, `cache#v2.4.10`)
- Step types (`command`, `wait`, `block`, `input`, `trigger`)

**Document Symbols**: Navigate your pipeline structure:
- Pipeline sections (`env`, `agents`, `steps`)
- Individual steps with their labels
- Step types (Command Step, Wait Step, Block Step, etc.)

**Go-to-Definition**: Jump from step references to definitions:
```yaml
steps:
  - label: "Build"
    key: "build-step"
  
  - label: "Test"
    depends_on: "build-step"  # Ctrl+click to jump to build step
```

**Code Actions**: Quick fixes for common issues:
- Add missing `label` to steps
- Add missing `key` to steps  
- Fix empty `command` values
- Convert single commands to command arrays
- Add missing step types

**Enhanced Diagnostics**: Precise error reporting:
- Schema validation errors with exact locations
- Plugin configuration validation
- Step dependency validation
- Multi-level severity (Error, Warning, Info)

## 📋 Examples

### Basic Pipeline
```yaml
steps:
  - label: "Build"
    command: "make build"
    agents:
      queue: "default"
    
  - label: "Test"
    command: "make test"
    depends_on: "build"
    artifact_paths: "coverage/*"
```

### Pipeline with Docker and Caching
```yaml
env:
  NODE_ENV: production

steps:
  - label: ":rocket: Build"
    command: "npm run build"
    key: "build"
    plugins:
      - docker#v5.13.0:
          image: "node:18"
          volumes:
            - ".:/app"
          workdir: "/app"
      - cache#v2.4.10:
          key: "npm-{{ checksum 'package-lock.json' }}"
          paths:
            - "node_modules"
    
  - wait: "Ready to deploy?"
  
  - block: "Deploy to production?"
    prompt: "Deploy the build to production?"
    
  - label: ":truck: Deploy"
    command: "./deploy.sh"
    depends_on: "build"
```

### Multi-Step Pipeline with All Features
```yaml
env:
  NODE_ENV: production
  APP_VERSION: "1.0.0"

agents:
  queue: "default"
  os: "linux"

steps:
  - label: ":hammer: Build"
    key: "build"
    command: 
      - "npm ci"
      - "npm run build" 
      - "npm run test"
    artifact_paths: "dist/**/*"
    plugins:
      - docker#v5.13.0:
          image: "node:18-alpine"
          volumes: [".:/app"]
          workdir: "/app"

  - wait: "Build complete"

  - input: "Release version"
    key: "release-input"
    fields:
      - text: "version"
        required: true
        default: "1.0.0"

  - label: ":package: Package"
    command: "docker build -t myapp:${BUILDKITE_BUILD_NUMBER} ."
    depends_on: 
      - "build"
      - "release-input"

  - block: ":rocket: Deploy to production?"
    prompt: "This will deploy to production. Are you sure?"

  - trigger: "deploy-pipeline"
    async: false
    build:
      message: "Deploy ${BUILDKITE_MESSAGE}"
      commit: "${BUILDKITE_COMMIT}"
      branch: "${BUILDKITE_BRANCH}"
```

## 🔧 Advanced Configuration

### Custom Plugin Schemas

The language server automatically fetches schemas for popular plugins. For custom plugins:

1. Plugin schemas are cached locally after first fetch
2. Internet access required for initial plugin validation
3. Supports 200+ plugins from the Buildkite Plugin Directory

### File Detection

The language server activates for:
- Any `.yml` or `.yaml` file inside a `.buildkite/` directory
- Files named: `pipeline.yml`, `pipeline.yaml`, `buildkite.yml`, `buildkite.yaml`
- Can be configured to activate on specific file patterns

### Debugging

Enable debug logging:
```bash
# Debug logs are written to:
tail -f /tmp/buildkite-ls-debug.log
```

## 🐛 Troubleshooting

### LSP Not Starting
```bash
# Check if buildkite-ls is in PATH
which buildkite-ls

# Check LSP logs in Neovim
:LspLog
:LspInfo

# Restart LSP client
:LspRestart
```

### Semantic Highlighting Not Working
1. Ensure your theme supports LSP semantic tokens
2. Check that semantic tokens are enabled in your LSP config
3. Use `:lua print(vim.inspect(vim.lsp.get_active_clients()))` to verify semantic token support

### Plugin Validation Issues
- Requires internet access for initial plugin schema fetching
- Schemas are cached locally after first download
- Check network connectivity if plugin validation fails

### Common Issues
- **Schema fetching fails**: Requires internet access for Buildkite schema
- **Plugin validation slow**: First run fetches schemas, subsequent runs use cache
- **Memory usage**: Large pipelines with many plugins may use more memory
- **File not detected**: Ensure file matches detection patterns

## 🏗️ Architecture

```
buildkite-ls/
├── main.go                 # LSP server entry point
├── internal/
│   ├── lsp/               # LSP protocol implementation
│   │   ├── server.go      # Main LSP server
│   │   ├── completion.go  # Autocompletion
│   │   ├── hover.go       # Hover documentation  
│   │   ├── diagnostics.go # Error reporting
│   │   └── semantic_tokens.go # Syntax highlighting
│   ├── schema/            # Schema validation
│   ├── plugins/           # Plugin registry
│   ├── parser/            # YAML parsing
│   └── context/           # Position analysis
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Add tests for new functionality
4. Ensure all tests pass (`go test ./...`)
5. Commit your changes (`git commit -am 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Submit a pull request

### Development Setup
```bash
git clone https://github.com/mcncl/buildkite-ls.git
cd buildkite-ls
go mod download
go test ./...
```

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built with [go.lsp.dev/protocol](https://go.lsp.dev/protocol) for LSP implementation
- Schema validation powered by [gojsonschema](https://github.com/xeipuuv/gojsonschema)
- Plugin schemas from the [Buildkite Plugin Directory](https://buildkite.com/plugins)
- YAML parsing with [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3)

---

**Happy Building!** 🚀