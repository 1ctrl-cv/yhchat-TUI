# Dot-source this file before running go commands in the sandboxed DSH session:
#   . .\scripts\env.ps1
# It points the Go toolchain caches and GOPROXY at workspace-local, writable
# locations (the sandbox denies writes to the default %USERPROFILE%\go paths)
# and adds the Go installation to PATH.

$Root = Split-Path -Parent $PSScriptRoot
$env:Path = "C:\Program Files\Go\bin;$env:Path"
$env:GOPATH = Join-Path $Root ".gopath"
$env:GOCACHE = Join-Path $Root ".gocache"
$env:GOPROXY = "https://goproxy.cn,direct"
$env:GOFLAGS = ""
