package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.SbmPath}}</string>
        <string>-data</string>
        <string>{{.DataDir}}</string>
        <string>-port</string>
        <string>{{.Port}}</string>
    </array>
    <key>RunAtLoad</key>
    <{{if .RunAtLoad}}true{{else}}false{{end}}/>
    <key>KeepAlive</key>
    <{{if .KeepAlive}}true{{else}}false{{end}}/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/sbm.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/sbm.error.log</string>
    <key>WorkingDirectory</key>
    <string>{{.WorkingDir}}</string>
</dict>
</plist>`

// LaunchdConfig launchd 配置
type LaunchdConfig struct {
	Label      string
	SbmPath    string // sbm 可执行文件路径
	DataDir    string // 数据目录
	Port       string // Web 端口
	LogPath    string
	WorkingDir string
	RunAtLoad  bool
	KeepAlive  bool
}

// LaunchdManager launchd 管理器
type LaunchdManager struct {
	label     string
	plistPath string
}

// NewLaunchdManager 创建 launchd 管理器
func NewLaunchdManager() (*LaunchdManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	label := "com.singbox.manager"
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", label+".plist")

	return &LaunchdManager{
		label:     label,
		plistPath: plistPath,
	}, nil
}

// Install 安装 launchd 服务
func (lm *LaunchdManager) Install(config LaunchdConfig) error {
	// 设置默认值
	if config.Label == "" {
		config.Label = lm.label
	}

	// 确保日志目录存在
	if err := os.MkdirAll(config.LogPath, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 确保 LaunchAgents 目录存在
	launchAgentsDir := filepath.Dir(lm.plistPath)
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}

	// 生成 plist 文件
	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return fmt.Errorf("解析模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("生成 plist 失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(lm.plistPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("写入 plist 失败: %w", err)
	}

	// 加载服务
	cmd := exec.Command("launchctl", "load", lm.plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("加载服务失败: %s", string(output))
	}

	return nil
}

// Uninstall 卸载 launchd 服务
func (lm *LaunchdManager) Uninstall() error {
	// 先停止服务
	lm.Stop()

	// 卸载服务
	cmd := exec.Command("launchctl", "unload", lm.plistPath)
	cmd.Run() // 忽略错误，可能服务未加载

	// 删除 plist 文件
	if err := os.Remove(lm.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 plist 失败: %w", err)
	}

	return nil
}

// Start 启动服务
func (lm *LaunchdManager) Start() error {
	cmd := exec.Command("launchctl", "start", lm.label)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("启动服务失败: %s", string(output))
	}
	return nil
}

// Stop 停止服务
func (lm *LaunchdManager) Stop() error {
	cmd := exec.Command("launchctl", "stop", lm.label)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("停止服务失败: %s", string(output))
	}
	return nil
}

// Restart 重启服务
func (lm *LaunchdManager) Restart() error {
	if err := lm.Stop(); err != nil {
		// 忽略停止错误
	}
	return lm.Start()
}

// IsInstalled 检查是否已安装
func (lm *LaunchdManager) IsInstalled() bool {
	_, err := os.Stat(lm.plistPath)
	return err == nil
}

// IsRunning 检查是否运行中
func (lm *LaunchdManager) IsRunning() bool {
	cmd := exec.Command("launchctl", "list", lm.label)
	err := cmd.Run()
	return err == nil
}

// GetPlistPath 获取 plist 文件路径
func (lm *LaunchdManager) GetPlistPath() string {
	return lm.plistPath
}

// GetLabel 获取服务标签
func (lm *LaunchdManager) GetLabel() string {
	return lm.label
}
