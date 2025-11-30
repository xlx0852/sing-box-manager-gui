import { useEffect, useState, useRef } from 'react';
import { Card, CardBody, CardHeader, Button, Tabs, Tab, Switch } from '@nextui-org/react';
import { RefreshCw, Trash2, Terminal, Server, Pause, Play } from 'lucide-react';
import { monitorApi } from '../api';

type LogType = 'sbm' | 'singbox';

export default function Logs() {
  const [activeTab, setActiveTab] = useState<LogType>('singbox');
  const [sbmLogs, setSbmLogs] = useState<string[]>([]);
  const [singboxLogs, setSingboxLogs] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchLogs = async (type: LogType) => {
    try {
      setLoading(true);
      if (type === 'sbm') {
        const res = await monitorApi.appLogs(500);
        setSbmLogs(res.data.data || []);
      } else {
        const res = await monitorApi.singboxLogs(500);
        setSingboxLogs(res.data.data || []);
      }
    } catch (error) {
      console.error('获取日志失败:', error);
    } finally {
      setLoading(false);
    }
  };

  const fetchCurrentLogs = () => {
    fetchLogs(activeTab);
  };

  // 滚动到底部
  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  };

  // 初始加载
  useEffect(() => {
    fetchLogs('sbm');
    fetchLogs('singbox');
  }, []);

  // 自动刷新
  useEffect(() => {
    if (autoRefresh) {
      intervalRef.current = setInterval(() => {
        fetchLogs(activeTab);
      }, 5000);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [autoRefresh, activeTab]);

  // 日志更新后滚动到底部
  useEffect(() => {
    scrollToBottom();
  }, [sbmLogs, singboxLogs, activeTab]);

  const handleClear = () => {
    if (activeTab === 'sbm') {
      setSbmLogs([]);
    } else {
      setSingboxLogs([]);
    }
  };

  const currentLogs = activeTab === 'sbm' ? sbmLogs : singboxLogs;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-800 dark:text-white">日志</h1>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            {autoRefresh ? (
              <Pause className="w-4 h-4 text-gray-500" />
            ) : (
              <Play className="w-4 h-4 text-gray-500" />
            )}
            <span className="text-sm text-gray-500">自动刷新</span>
            <Switch
              size="sm"
              isSelected={autoRefresh}
              onValueChange={setAutoRefresh}
            />
          </div>
          <Button
            size="sm"
            color="primary"
            variant="flat"
            startContent={<RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />}
            onPress={fetchCurrentLogs}
            isDisabled={loading}
          >
            刷新
          </Button>
          <Button
            size="sm"
            color="danger"
            variant="flat"
            startContent={<Trash2 className="w-4 h-4" />}
            onPress={handleClear}
          >
            清空
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader className="pb-0">
          <Tabs
            selectedKey={activeTab}
            onSelectionChange={(key) => setActiveTab(key as LogType)}
            aria-label="日志类型"
          >
            <Tab
              key="singbox"
              title={
                <div className="flex items-center gap-2">
                  <Terminal className="w-4 h-4" />
                  <span>sing-box 日志</span>
                </div>
              }
            />
            <Tab
              key="sbm"
              title={
                <div className="flex items-center gap-2">
                  <Server className="w-4 h-4" />
                  <span>应用日志</span>
                </div>
              }
            />
          </Tabs>
        </CardHeader>
        <CardBody>
          <div
            ref={logContainerRef}
            className="bg-gray-900 text-gray-100 rounded-lg p-4 h-[600px] overflow-auto font-mono text-sm"
          >
            {currentLogs.length === 0 ? (
              <div className="text-gray-500 text-center py-8">
                暂无日志
              </div>
            ) : (
              currentLogs.map((line, index) => (
                <div
                  key={index}
                  className={`whitespace-pre-wrap break-all py-0.5 ${
                    line.includes('error') || line.includes('ERROR') || line.includes('fatal') || line.includes('FATAL')
                      ? 'text-red-400'
                      : line.includes('warn') || line.includes('WARN')
                      ? 'text-yellow-400'
                      : line.includes('info') || line.includes('INFO')
                      ? 'text-blue-400'
                      : ''
                  }`}
                >
                  {line}
                </div>
              ))
            )}
          </div>
          <div className="mt-2 text-sm text-gray-500 flex justify-between">
            <span>
              共 {currentLogs.length} 行
            </span>
            <span>
              {autoRefresh ? '每 5 秒自动刷新' : '自动刷新已暂停'}
            </span>
          </div>
        </CardBody>
      </Card>
    </div>
  );
}
