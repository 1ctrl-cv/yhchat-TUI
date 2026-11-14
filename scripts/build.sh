#!/usr/bin/env sh
# yhchat-tui 全量构建脚本（Linux / macOS / Git Bash 入口）。
#
# 实际构建逻辑在 tools/build（Go 实现，跨平台），因此 Windows 与 Linux 产物
# 完全一致。本脚本只做参数转发。
#
# 用法：
#   ./scripts/build.sh                          # 全量构建（默认输出到 dist/）
#   ./scripts/build.sh -version 1.0.0           # 指定版本号
#   ./scripts/build.sh -skip-tests              # 跳过测试
#   ./scripts/build.sh -targets windows/amd64,linux/amd64
#   ./scripts/build.sh -help
#
# 一条命令完成：代码检查（gofmt/go vet）→ 测试 → Windows/macOS/Linux 全架构交叉编译，
# 并为每个目标注入版本/commit/构建时间。产物只有各平台可执行文件，不做打包。
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

if ! command -v go >/dev/null 2>&1; then
    printf '构建失败: 未找到 go 命令，请先安装 Go 1.24+ 并加入 PATH\n' >&2
    exit 1
fi

exec go run ./tools/build "$@"


