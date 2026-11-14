// Command build is the cross-platform builder for yhchat-tui.
//
// It is the implementation behind scripts/build.ps1 and scripts/build.sh:
//
//  1. gofmt check + go vet            (disable with -skip-checks)
//  2. go test ./... -count=1          (disable with -skip-tests)
//  3. cross-compile Windows/Linux targets into the output directory
//  4. inject version, git commit and build time via -ldflags
//
// The output is one executable per GOOS/GOARCH target; no archives or extra
// metadata files are produced.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const appName = "yhchat-tui"

// options holds the command line configuration.
type options struct {
	version    string
	outDir     string
	targets    string
	skipChecks bool
	skipTests  bool
	noClean    bool
}

// target is one GOOS/GOARCH pair to build.
type target struct {
	OS    string
	Arch  string
	GOARM string
}

// artifact is a built executable.
type artifact struct {
	OS   string
	Arch string
	Name string
	Path string
	Size int64
}

// buildMeta holds information collected from git and the toolchain.
type buildMeta struct {
	Version   string
	Commit    string
	Dirty     bool
	BuildTime string
	GoVersion string
	Module    string
}

func main() {
	var opts options
	fs := flag.NewFlagSet(appName+" build", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() { fmt.Fprint(os.Stdout, usageText()) }
	fs.StringVar(&opts.version, "version", "", "版本号（默认取 git tag，否则 0.1.0-dev+commit）")
	fs.StringVar(&opts.outDir, "out", "dist", "输出目录（默认 <仓库>/dist）")
	fs.StringVar(&opts.targets, "targets", "", "目标列表，逗号或空格分隔（默认全部常用 Windows/Linux 架构）")
	fs.BoolVar(&opts.skipChecks, "skip-checks", false, "跳过 gofmt / go vet")
	fs.BoolVar(&opts.skipTests, "skip-tests", false, "跳过 go test")
	fs.BoolVar(&opts.noClean, "no-clean", false, "构建前不清空输出目录")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "未知参数: %s\n\n", strings.Join(fs.Args(), " "))
		fs.Usage()
		os.Exit(2)
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "\n构建失败: %v\n", err)
		os.Exit(1)
	}
}

// usageText returns the -help output.
func usageText() string {
	return `yhchat-tui 全量构建（Windows / Linux）

用法: go run ./tools/build [选项]
（更常用的入口: pwsh -File scripts\build.ps1 或 ./scripts/build.sh，二者都会转发到这里）

选项:
  -version <版本>   版本号（默认取 git tag，否则 0.1.0-dev+<commit>）
  -out <目录>       输出目录（默认 <仓库>/dist）
  -targets <列表>   目标列表，逗号或空格分隔，默认:
                    windows/386 windows/amd64 windows/arm64
                    linux/386 linux/amd64 linux/arm linux/arm64
                    linux/loong64 linux/riscv64
  -skip-checks      跳过 gofmt / go vet
  -skip-tests       跳过 go test
  -no-clean         构建前不清空输出目录
  -h, -help         显示本帮助

产物为每个目标一个可执行文件（不生成压缩包/校验和等额外文件）。

示例:
  go run ./tools/build
  go run ./tools/build -version 1.0.0 -skip-tests
  go run ./tools/build -targets windows/amd64,linux/amd64
`
}

// run executes the whole build.
func run(opts options) error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("进入仓库目录失败: %w", err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return errors.New("未找到 go 命令：请先安装 Go 1.24+ 并加入 PATH")
	}

	step("收集构建信息")
	meta, err := collectMeta(opts.version, goBin)
	if err != nil {
		return err
	}
	ok("version : %s", meta.Version)
	ok("commit  : %s%s", meta.Commit, dirtySuffix(meta.Dirty))
	ok("built   : %s", meta.BuildTime)
	ok("go      : %s", meta.GoVersion)
	ok("module  : %s", meta.Module)

	targets, err := parseTargets(opts.targets)
	if err != nil {
		return err
	}

	outDir, err := filepath.Abs(opts.outDir)
	if err != nil {
		return fmt.Errorf("解析输出目录失败: %w", err)
	}
	ok("output  : %s", outDir)
	var targetNames []string
	for _, t := range targets {
		targetNames = append(targetNames, t.OS+"/"+t.Arch)
	}
	ok("targets : %s", strings.Join(targetNames, ", "))

	// -----------------------------------------------------------------
	if opts.skipChecks {
		step("跳过代码检查（-skip-checks）")
	} else {
		step("代码检查（gofmt / go vet）")
		if err := checkFormatting(goBin); err != nil {
			return err
		}
		ok("gofmt: 通过")
		if err := runCommand("", goBin, "vet", "./..."); err != nil {
			return fmt.Errorf("go vet 失败: %w", err)
		}
		ok("go vet: 通过")
	}

	if opts.skipTests {
		step("跳过测试（-skip-tests）")
	} else {
		step("运行测试（go test ./...）")
		if err := runCommand("", goBin, "test", "./...", "-count=1"); err != nil {
			return fmt.Errorf("测试失败: %w", err)
		}
		ok("测试: 通过")
	}

	// -----------------------------------------------------------------
	if err := prepareOutDir(outDir, opts.noClean); err != nil {
		return err
	}

	step("交叉编译")
	artifacts, err := buildTargets(goBin, meta, targets, outDir)
	if err != nil {
		return err
	}

	// Smoke test the host-matching binary: -version must contain the injected
	// version, proving the ldflags were applied.
	if hostArtifact := findArtifact(artifacts, runtime.GOOS, runtime.GOARCH); hostArtifact != nil {
		step("冒烟测试（-version）")
		output, runErr := exec.Command(hostArtifact.Path, "-version").CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("执行 %s -version 失败: %w", hostArtifact.Name, runErr)
		}
		text := string(output)
		if !strings.Contains(text, meta.Version) {
			return fmt.Errorf("-version 输出未包含版本号 %s:\n%s", meta.Version, text)
		}
		ok("%s", strings.TrimSpace(text))
	}

	printSummary(artifacts, outDir)
	return nil
}

// ---------------------------------------------------------------------------
// repository / metadata

// findRepoRoot walks up from the current directory looking for go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("未找到 go.mod：请在仓库内运行构建脚本")
		}
		dir = parent
	}
}

// collectMeta gathers versioning and toolchain information.
func collectMeta(requestedVersion, goBin string) (*buildMeta, error) {
	meta := &buildMeta{
		Commit:    "unknown",
		BuildTime: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		meta.Commit = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil && len(out) > 0 {
		meta.Dirty = true
	}
	if out, err := exec.Command(goBin, "version").Output(); err == nil {
		meta.GoVersion = strings.TrimSpace(string(out))
	}
	if module, err := modulePath(); err == nil {
		meta.Module = module
	} else {
		return nil, err
	}

	requestedVersion = strings.TrimSpace(requestedVersion)
	version := requestedVersion
	if version == "" {
		if out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output(); err == nil {
			candidate := strings.TrimSpace(string(out))
			// Only a real tag is used as the version; a bare commit hash means
			// the repository has no tags yet.
			if candidate != "" && isVersionTag(candidate) {
				version = strings.TrimPrefix(candidate, "v")
			}
		}
	}
	if version == "" {
		version = "0.1.0-dev"
		if meta.Commit != "unknown" {
			version = "0.1.0-dev+" + meta.Commit
		}
	}
	// Only auto-derived versions get the dirty marker; an explicitly requested
	// version is used verbatim.
	if requestedVersion == "" && meta.Dirty && !strings.Contains(version, "dirty") {
		version += "-dirty"
	}
	if strings.ContainsAny(version, " \t\r\n\"'\\") {
		return nil, fmt.Errorf("版本号不能包含空白或引号: %q", version)
	}
	meta.Version = version
	return meta, nil
}

// isVersionTag reports whether a git describe output starts like a version.
func isVersionTag(s string) bool {
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// modulePath returns the module path declared in go.mod.
func modulePath() (string, error) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("读取 go.mod 失败: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", errors.New("go.mod 中未找到 module 声明")
}

// checkFormatting fails when any tracked Go source is not gofmt-clean.
func checkFormatting(goBin string) error {
	gofmt := filepath.Join(filepath.Dir(goBin), "gofmt")
	if runtime.GOOS == "windows" {
		gofmt += ".exe"
	}
	if _, err := os.Stat(gofmt); err != nil {
		if found, lookErr := exec.LookPath("gofmt"); lookErr == nil {
			gofmt = found
		} else {
			return errors.New("未找到 gofmt（通常与 go 一起安装）")
		}
	}
	out, err := exec.Command(gofmt, "-l", "main.go", "internal", "tools").Output()
	if err != nil {
		return fmt.Errorf("执行 gofmt 失败: %w", err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("以下文件未格式化，请先执行 gofmt -w:\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// targets

// defaultTargets are all common Windows/Linux architectures.
func defaultTargets() []target {
	return []target{
		{OS: "windows", Arch: "386"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
		{OS: "linux", Arch: "386"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm", GOARM: "7"},
		{OS: "linux", Arch: "arm64"},
		{OS: "linux", Arch: "loong64"},
		{OS: "linux", Arch: "riscv64"},
	}
}

// parseTargets parses the -targets flag ("os/arch" separated by comma/space).
func parseTargets(spec string) ([]target, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return defaultTargets(), nil
	}
	fields := strings.Fields(strings.ReplaceAll(spec, ",", " "))
	targets := make([]target, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		parts := strings.Split(field, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("目标格式错误: %q（应为 os/arch，例如 windows/amd64）", field)
		}
		t := target{
			OS:   strings.ToLower(parts[0]),
			Arch: strings.ToLower(parts[1]),
		}
		if t.OS != "windows" && t.OS != "linux" {
			return nil, fmt.Errorf("不支持的目标系统: %s（只支持 windows 与 linux）", t.OS)
		}
		if t.Arch == "arm" {
			t.GOARM = "7"
		}
		key := t.OS + "/" + t.Arch
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return nil, errors.New("目标列表为空")
	}
	return targets, nil
}

// ---------------------------------------------------------------------------
// build

// prepareOutDir creates the output directory and optionally empties it.
func prepareOutDir(outDir string, noClean bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	if noClean {
		return nil
	}
	step("清空输出目录")
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return fmt.Errorf("读取输出目录失败: %w", err)
	}
	for _, entry := range entries {
		if removeErr := os.RemoveAll(filepath.Join(outDir, entry.Name())); removeErr != nil {
			return fmt.Errorf("清空输出目录失败: %w", removeErr)
		}
	}
	return nil
}

// buildTargets cross-compiles every target and returns the artifacts.
func buildTargets(goBin string, meta *buildMeta, targets []target, outDir string) ([]artifact, error) {
	modFlag := "-mod=mod"
	if _, err := os.Stat(filepath.Join("vendor", "modules.txt")); err == nil {
		modFlag = "-mod=vendor"
	}

	ldflags := strings.Join([]string{
		"-s -w",
		fmt.Sprintf("-X %s/internal/tui.Version=%s", meta.Module, meta.Version),
		fmt.Sprintf("-X %s/internal/tui.Commit=%s", meta.Module, meta.Commit),
		fmt.Sprintf("-X %s/internal/tui.BuildTime=%s", meta.Module, meta.BuildTime),
	}, " ")

	artifacts := make([]artifact, 0, len(targets))
	for _, t := range targets {
		name := fmt.Sprintf("%s-%s-%s", appName, t.OS, t.Arch)
		if t.OS == "windows" {
			name += ".exe"
		}
		outPath := filepath.Join(outDir, name)

		env := append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS="+t.OS,
			"GOARCH="+t.Arch,
		)
		if t.GOARM != "" {
			env = append(env, "GOARM="+t.GOARM)
		}
		fmt.Printf("    -> %-8s %-6s %s\n", t.OS, t.Arch, name)

		cmd := exec.Command(goBin, "build",
			"-trimpath", "-buildvcs=false", modFlag,
			"-ldflags", ldflags,
			"-o", outPath, ".",
		)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("构建失败 %s/%s: %w", t.OS, t.Arch, err)
		}
		info, err := os.Stat(outPath)
		if err != nil {
			return nil, fmt.Errorf("构建产物缺失 %s: %w", outPath, err)
		}
		if info.Size() == 0 {
			return nil, fmt.Errorf("构建产物为空: %s", outPath)
		}
		artifacts = append(artifacts, artifact{
			OS:   t.OS,
			Arch: t.Arch,
			Name: name,
			Path: outPath,
			Size: info.Size(),
		})
	}
	return artifacts, nil
}

// findArtifact returns the artifact for an OS/arch pair, or nil.
func findArtifact(artifacts []artifact, osName, arch string) *artifact {
	for i := range artifacts {
		if artifacts[i].OS == osName && artifacts[i].Arch == arch {
			return &artifacts[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers

// step prints a centered section header.
func step(text string) {
	fmt.Printf("\n==> %s\n", text)
}

// ok prints an indented info line.
func ok(format string, args ...any) {
	fmt.Printf("    "+format+"\n", args...)
}

// runCommand runs a command with inherited stdio.
func runCommand(dir, bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// dirtySuffix returns " (dirty)" when the worktree has uncommitted changes.
func dirtySuffix(dirty bool) string {
	if dirty {
		return " (dirty)"
	}
	return ""
}

// printSummary prints the final table of compiled binaries.
func printSummary(artifacts []artifact, outDir string) {
	step("构建完成")
	fmt.Printf("    %-14s %-38s %s\n", "目标", "文件", "大小")
	var total int64
	for _, a := range artifacts {
		fmt.Printf("    %-14s %-38s %.2f MB\n",
			a.OS+"/"+a.Arch, a.Name, float64(a.Size)/(1024*1024))
		total += a.Size
	}
	fmt.Printf("\n共 %d 个可执行文件，合计 %.2f MB\n输出目录: %s\n",
		len(artifacts), float64(total)/(1024*1024), outDir)
}
