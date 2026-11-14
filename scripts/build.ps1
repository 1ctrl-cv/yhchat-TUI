#Requires -Version 5.1
<#
.SYNOPSIS
    yhchat-tui 全量构建脚本（Windows / PowerShell 入口）。

.DESCRIPTION
    实际构建逻辑在 tools/build（Go 实现，跨平台），因此 Windows 与 Linux 产物
    完全一致。本脚本只做参数转发。

    一条命令完成：代码检查（gofmt/go vet）→ 测试 → Windows/Linux 全架构交叉编译
    → 注入版本/commit/构建时间 → 生成 zip/tar.gz → SHA256SUMS.txt + BUILD-INFO.txt。

    默认输出目录为仓库下的 dist/。

.PARAMETER Version
    版本号（默认取 git tag，否则 0.1.0-dev+<commit>）。等价于 -version。

.PARAMETER Out
    输出目录（默认 <仓库>/dist）。等价于 -out。

.PARAMETER Targets
    目标列表，逗号分隔，例如 "windows/amd64,linux/amd64"。
    默认：windows/386、windows/amd64、windows/arm64、
    linux/386、linux/amd64、linux/arm、linux/arm64、linux/loong64、linux/riscv64。

.PARAMETER SkipChecks
    跳过 gofmt / go vet。

.PARAMETER SkipTests
    跳过 go test。

.PARAMETER NoArchive
    只生成可执行文件，不生成压缩包。

.PARAMETER NoClean
    构建前不清空输出目录。

.EXAMPLE
    pwsh -File scripts\build.ps1

.EXAMPLE
    pwsh -File scripts\build.ps1 -Version 1.0.0 -SkipTests

.EXAMPLE
    pwsh -File scripts\build.ps1 -Targets windows/amd64,linux/amd64 -NoArchive
#>
[CmdletBinding()]
param(
    [Alias("v")]
    [string]$Version = "",

    [Alias("o")]
    [string]$Out = "",

    [Alias("t")]
    [string]$Targets = "",

    [Alias("skip-checks")]
    [switch]$SkipChecks,

    [Alias("skip-tests")]
    [switch]$SkipTests,

    [Alias("no-archive")]
    [switch]$NoArchive,

    [Alias("no-clean")]
    [switch]$NoClean,

    [Alias("h")]
    [switch]$Help
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "构建失败: 未找到 go 命令，请先安装 Go 1.24+ 并加入 PATH" -ForegroundColor Red
    exit 1
}

$goArgs = @()
if ($Version) { $goArgs += @("-version", $Version) }
if ($Out) { $goArgs += @("-out", $Out) }
if ($Targets) { $goArgs += @("-targets", $Targets) }
if ($SkipChecks) { $goArgs += "-skip-checks" }
if ($SkipTests) { $goArgs += "-skip-tests" }
if ($NoArchive) { $goArgs += "-no-archive" }
if ($NoClean) { $goArgs += "-no-clean" }
if ($Help) { $goArgs += "-help" }

Push-Location $Root
try {
    & go run ./tools/build @goArgs
    exit $LASTEXITCODE
}
catch {
    Write-Host "构建失败: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
finally {
    Pop-Location
}

