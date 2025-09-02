local Pkg = require "mason-core.package"
local github = require "mason-core.managers.github"

return Pkg.new {
    name = "buildkite-ls",
    desc = [[A comprehensive Language Server Protocol (LSP) implementation for Buildkite pipeline files, providing rich IDE features for productive pipeline development.]],
    homepage = "https://github.com/mcncl/buildkite-ls",
    languages = { Pkg.Lang.YAML },
    categories = { Pkg.Cat.LSP },
    ---@async
    ---@param ctx InstallContext
    install = function(ctx)
        github
            .unzip_release_file({
                repo = "mcncl/buildkite-ls",
                asset_file = function(version, target_os, target_arch)
                    local os_map = {
                        linux = "linux",
                        macos = "darwin",
                        win = "windows",
                    }
                    local arch_map = {
                        x64 = "amd64",
                        arm64 = "arm64",
                    }
                    return ("buildkite-ls_%s_%s_%s.tar.gz"):format(
                        version:gsub("^v", ""),
                        os_map[target_os] or target_os,
                        arch_map[target_arch] or target_arch
                    )
                end,
            })
            .with_receipt()

        ctx:link_bin("buildkite-ls", "buildkite-ls")
    end,
}