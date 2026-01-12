import { useState, useEffect, useRef, useCallback } from 'react';
import { useStore } from '../store';

// Clash API 连接数据类型
export interface ConnectionMetadata {
  network: string;
  type: string;
  sourceIP: string;
  destinationIP: string;
  sourcePort: string;
  destinationPort: string;
  host: string;
  dnsMode: string;
  processPath?: string;
}

export interface Connection {
  id: string;
  metadata: ConnectionMetadata;
  upload: number;
  download: number;
  start: string;
  chains: string[];
  rule: string;
  rulePayload: string;
}

export interface ConnectionsMessage {
  downloadTotal: number;
  uploadTotal: number;
  connections: Connection[];
}

interface UseClashConnectionsReturn {
  connections: Connection[];
  downloadTotal: number;
  uploadTotal: number;
  isConnected: boolean;
  error: string | null;
  reconnect: () => void;
}

export function useClashConnections(): UseClashConnectionsReturn {
  const settings = useStore(state => state.settings);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [downloadTotal, setDownloadTotal] = useState(0);
  const [uploadTotal, setUploadTotal] = useState(0);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!settings) return;

    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    
    // 构建 WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.hostname;
    let wsUrl = `${protocol}//${host}:${port}/connections`;
    
    if (secret) {
      wsUrl += `?token=${encodeURIComponent(secret)}`;
    }

    // 关闭现有连接
    if (wsRef.current) {
      wsRef.current.close();
    }

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setIsConnected(true);
        setError(null);
        console.log('Clash API WebSocket connected');
      };

      ws.onmessage = (event) => {
        try {
          const data: ConnectionsMessage = JSON.parse(event.data);
          setConnections(data.connections || []);
          setDownloadTotal(data.downloadTotal || 0);
          setUploadTotal(data.uploadTotal || 0);
        } catch (e) {
          console.error('Failed to parse connections data:', e);
        }
      };

      ws.onerror = (event) => {
        console.error('WebSocket error:', event);
        setError('连接 Clash API 失败');
      };

      ws.onclose = () => {
        setIsConnected(false);
        wsRef.current = null;
        
        // 5秒后尝试重连
        reconnectTimeoutRef.current = setTimeout(() => {
          connect();
        }, 5000);
      };
    } catch (e) {
      setError('无法建立 WebSocket 连接');
      console.error('WebSocket connection error:', e);
    }
  }, [settings]);

  const reconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
    }
    connect();
  }, [connect]);

  useEffect(() => {
    if (settings) {
      connect();
    }

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [settings, connect]);

  return {
    connections,
    downloadTotal,
    uploadTotal,
    isConnected,
    error,
    reconnect,
  };
}
