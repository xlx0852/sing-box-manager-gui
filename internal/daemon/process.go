package daemon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/xiaobei/singbox-manager/internal/logger"
)

// ProcessManager 进程管理器
type ProcessManager struct {
	singboxPath string
	configPath  string
	dataDir     string // 数据目录，用于设置 sing-box 的工作目录
	cmd         *exec.Cmd
	mu          sync.RWMutex
	running     bool
	logs        []string
	maxLogs     int
}

// NewProcessManager 创建进程管理器
func NewProcessManager(singboxPath, configPath, dataDir string) *ProcessManager {
	return &ProcessManager{
		singboxPath: singboxPath,
		configPath:  configPath,
		dataDir:     dataDir,
		maxLogs:     1000,
		logs:        make([]string, 0),
	}
}

// Start 启动 sing-box
func (pm *ProcessManager) Start() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return fmt.Errorf("sing-box 已经在运行")
	}

	// 检查 sing-box 是否存在
	if _, err := os.Stat(pm.singboxPath); os.IsNotExist(err) {
		return fmt.Errorf("sing-box 不存在: %s", pm.singboxPath)
	}

	// 检查配置文件是否存在
	if _, err := os.Stat(pm.configPath); os.IsNotExist(err) {
		return fmt.Errorf("配置文件不存在: %s", pm.configPath)
	}

	pm.cmd = exec.Command(pm.singboxPath, "run", "-c", pm.configPath)
	pm.cmd.Dir = pm.dataDir // 设置工作目录，确保相对路径（如 external_ui）正确解析

	// 捕获输出
	stdout, err := pm.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取标准输出失败: %w", err)
	}

	stderr, err := pm.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("获取标准错误失败: %w", err)
	}

	if err := pm.cmd.Start(); err != nil {
		return fmt.Errorf("启动 sing-box 失败: %w", err)
	}

	pm.running = true
	logger.Printf("sing-box 已启动, PID: %d", pm.cmd.Process.Pid)

	// 获取 sing-box 日志记录器
	var singboxLogger *logger.Logger
	if logManager := logger.GetLogManager(); logManager != nil {
		singboxLogger = logManager.SingboxLogger()
	}

	// 异步读取日志
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			pm.addLog(line)
			// 同时写入日志文件
			if singboxLogger != nil {
				singboxLogger.WriteRaw(line)
			}
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			pm.addLog(line)
			// 同时写入日志文件
			if singboxLogger != nil {
				singboxLogger.WriteRaw(line)
			}
		}
	}()

	// 监控进程退出
	go func() {
		pm.cmd.Wait()
		pm.mu.Lock()
		pm.running = false
		pm.mu.Unlock()
	}()

	return nil
}

// Stop 停止 sing-box
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running || pm.cmd == nil || pm.cmd.Process == nil {
		return nil
	}

	pid := pm.cmd.Process.Pid

	// 发送 SIGTERM 信号
	if err := pm.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// 如果 SIGTERM 失败，尝试 SIGKILL
		if err := pm.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("停止 sing-box 失败: %w", err)
		}
	}

	pm.running = false
	logger.Printf("sing-box 已停止, PID: %d", pid)
	return nil
}

// Restart 重启 sing-box
func (pm *ProcessManager) Restart() error {
	if err := pm.Stop(); err != nil {
		return err
	}
	return pm.Start()
}

// Reload 热重载配置
func (pm *ProcessManager) Reload() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.running || pm.cmd == nil || pm.cmd.Process == nil {
		return fmt.Errorf("sing-box 未运行")
	}

	// sing-box 支持 SIGHUP 热重载
	if err := pm.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("重载配置失败: %w", err)
	}

	return nil
}

// IsRunning 检查是否运行中
func (pm *ProcessManager) IsRunning() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.running
}

// GetPID 获取进程 ID
func (pm *ProcessManager) GetPID() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.cmd != nil && pm.cmd.Process != nil {
		return pm.cmd.Process.Pid
	}
	return 0
}

// GetLogs 获取日志
func (pm *ProcessManager) GetLogs() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	logs := make([]string, len(pm.logs))
	copy(logs, pm.logs)
	return logs
}

// ClearLogs 清除日志
func (pm *ProcessManager) ClearLogs() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.logs = make([]string, 0)
}

// addLog 添加日志
func (pm *ProcessManager) addLog(line string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logs = append(pm.logs, line)

	// 限制日志数量
	if len(pm.logs) > pm.maxLogs {
		pm.logs = pm.logs[len(pm.logs)-pm.maxLogs:]
	}
}

// SetPaths 设置路径
func (pm *ProcessManager) SetPaths(singboxPath, configPath string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.singboxPath = singboxPath
	pm.configPath = configPath
}

// SetConfigPath 只设置配置文件路径
func (pm *ProcessManager) SetConfigPath(configPath string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.configPath = configPath
}

// Check 检查配置文件
func (pm *ProcessManager) Check() error {
	cmd := exec.Command(pm.singboxPath, "check", "-c", pm.configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("配置检查失败: %s", string(output))
	}
	return nil
}

// Version 获取 sing-box 版本
func (pm *ProcessManager) Version() (string, error) {
	cmd := exec.Command(pm.singboxPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取版本失败: %w", err)
	}
	return string(output), nil
}
