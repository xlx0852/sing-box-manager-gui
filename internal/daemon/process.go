package daemon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/xiaobei/singbox-manager/internal/logger"
)

// ProcessManager 进程管理器
type ProcessManager struct {
	singboxPath string
	configPath  string
	dataDir     string // 数据目录，用于设置 sing-box 的工作目录
	pidFile     string // PID 文件路径，用于持久化进程状态
	cmd         *exec.Cmd
	mu          sync.RWMutex
	running     bool
	pid         int // 保存 PID（支持恢复的进程，即使 cmd 为空）
	logs        []string
	maxLogs     int
}

// NewProcessManager 创建进程管理器
func NewProcessManager(singboxPath, configPath, dataDir string) *ProcessManager {
	pm := &ProcessManager{
		singboxPath: singboxPath,
		configPath:  configPath,
		dataDir:     dataDir,
		pidFile:     filepath.Join(dataDir, "singbox.pid"),
		maxLogs:     1000,
		logs:        make([]string, 0),
	}

	// 启动时尝试恢复已有的 sing-box 进程
	pm.recoverProcess()

	return pm
}

// recoverProcess 尝试恢复已有的 sing-box 进程（双重检测）
func (pm *ProcessManager) recoverProcess() {
	var pid int

	// 第一步：尝试从 PID 文件恢复
	pid = pm.recoverFromPidFile()

	// 第二步：如果 PID 文件无效，扫描系统进程
	if pid <= 0 {
		pid = pm.findSingboxProcess()
	}

	if pid <= 0 {
		return // 没有找到 sing-box 进程
	}

	// 恢复状态
	pm.mu.Lock()
	pm.running = true
	pm.pid = pid
	pm.mu.Unlock()

	// 更新 PID 文件（确保一致性）
	os.WriteFile(pm.pidFile, []byte(strconv.Itoa(pid)), 0644)

	logger.Printf("已恢复 sing-box 进程跟踪, PID: %d", pid)

	// 启动异步监控进程退出
	go pm.monitorProcess(pid)
}

// recoverFromPidFile 从 PID 文件恢复
func (pm *ProcessManager) recoverFromPidFile() int {
	data, err := os.ReadFile(pm.pidFile)
	if err != nil {
		return 0 // PID 文件不存在
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		os.Remove(pm.pidFile)
		return 0
	}

	// 验证进程是否存在且是 sing-box
	if !pm.isValidSingboxProcess(pid) {
		os.Remove(pm.pidFile)
		return 0
	}

	logger.Printf("从 PID 文件恢复 sing-box 进程, PID: %d", pid)
	return pid
}

// findSingboxProcess 扫描系统进程查找 sing-box
func (pm *ProcessManager) findSingboxProcess() int {
	procs, err := process.Processes()
	if err != nil {
		return 0
	}

	for _, proc := range procs {
		if !pm.isSingboxProcess(proc) {
			continue
		}

		// 进一步验证命令行参数，确保是使用我们的配置文件
		cmdline, err := proc.Cmdline()
		if err == nil && strings.Contains(cmdline, pm.configPath) {
			logger.Printf("通过进程扫描找到 sing-box (配置匹配), PID: %d", proc.Pid)
			return int(proc.Pid)
		}

		// 如果没有匹配的配置文件，也接受（可能是相同的 sing-box）
		logger.Printf("通过进程扫描找到 sing-box (未验证配置), PID: %d", proc.Pid)
		return int(proc.Pid)
	}

	return 0
}

// isSingboxProcess 检查进程是否是 sing-box
func (pm *ProcessManager) isSingboxProcess(proc *process.Process) bool {
	// 方法1：检查进程名称
	name, _ := proc.Name()
	if name == "sing-box" {
		return true
	}

	// 方法2：检查可执行文件路径（macOS 上进程名可能被截断）
	exe, _ := proc.Exe()
	if strings.HasSuffix(exe, "/sing-box") || strings.HasSuffix(exe, "\\sing-box") {
		return true
	}

	return false
}

// isValidSingboxProcess 验证 PID 是否是有效的 sing-box 进程
func (pm *ProcessManager) isValidSingboxProcess(pid int) bool {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}

	return pm.isSingboxProcess(proc)
}

// monitorProcess 监控已恢复的进程（当没有 cmd 对象时使用）
func (pm *ProcessManager) monitorProcess(pid int) {
	for {
		time.Sleep(2 * time.Second)

		// 检查进程是否仍在运行
		if !pm.isValidSingboxProcess(pid) {
			pm.mu.Lock()
			pm.running = false
			pm.pid = 0
			pm.mu.Unlock()
			os.Remove(pm.pidFile)
			logger.Printf("sing-box 进程已退出, PID: %d", pid)
			return
		}
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
	pm.pid = pm.cmd.Process.Pid

	// 写入 PID 文件
	if err := os.WriteFile(pm.pidFile, []byte(strconv.Itoa(pm.pid)), 0644); err != nil {
		logger.Printf("写入 PID 文件失败: %v", err)
	}

	logger.Printf("sing-box 已启动, PID: %d", pm.pid)

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
		pm.pid = 0
		pm.mu.Unlock()
		os.Remove(pm.pidFile)
		logger.Printf("sing-box 进程已退出")
	}()

	return nil
}

// Stop 停止 sing-box
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	var pid int

	// 情况1：有 cmd 对象（正常启动的进程）
	if pm.cmd != nil && pm.cmd.Process != nil {
		pid = pm.cmd.Process.Pid
		// 发送 SIGTERM 信号
		if err := pm.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// 如果 SIGTERM 失败，尝试 SIGKILL
			if err := pm.cmd.Process.Kill(); err != nil {
				return fmt.Errorf("停止 sing-box 失败: %w", err)
			}
		}
	} else if pm.pid > 0 {
		// 情况2：没有 cmd 对象（恢复的进程），通过 PID 发送信号
		pid = pm.pid
		proc, err := os.FindProcess(pid)
		if err == nil {
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				proc.Kill()
			}
		}
	}

	pm.running = false
	pm.pid = 0
	os.Remove(pm.pidFile)
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

	// 优先返回保存的 PID（支持恢复的进程）
	if pm.pid > 0 {
		return pm.pid
	}

	// 备用：从 cmd 获取
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
