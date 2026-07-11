import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Laptop, MonitorCog, RefreshCw, Trash2, XCircle } from 'lucide-react'

type Device = {
  id: string
  name: string
  platform: string
  last_seen_at: string | null
  last_sync_at: string | null
  last_sync_status: 'success' | 'error' | null
  last_sync_error: string | null
  last_applied_change_id: number
}

type Conflict = {
  id: string
  path: string
  local_version: number | null
  remote_version: number | null
  created_at: string
}

type APIRequest = <T>(path: string, options?: { method?: string; body?: unknown }) => Promise<T>

function formatDate(value: string | null) {
  return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '尚未上报'
}

export function SyncStatus({ request, onError }: { request: APIRequest; onError: (message: string) => void }) {
  const [devices, setDevices] = useState<Device[]>([])
  const [conflicts, setConflicts] = useState<Conflict[]>([])
  const [loading, setLoading] = useState(true)
  const [revokingID, setRevokingID] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    try {
      const [deviceData, conflictData] = await Promise.all([
        request<{ items: Device[] }>('/devices?limit=100'),
        request<{ items: Conflict[] }>('/sync/conflicts?resolution=pending&limit=100'),
      ])
      setDevices(deviceData.items)
      setConflicts(conflictData.items)
    } catch (error) {
      onError(error instanceof Error ? error.message : '读取同步状态失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [])

  async function resolve(id: string, resolution: 'keep_local' | 'keep_remote' | 'keep_both') {
    try {
      await request(`/sync/conflicts/${id}`, { method: 'PATCH', body: { resolution } })
      await load()
    } catch (error) {
      onError(error instanceof Error ? error.message : '处理冲突失败')
    }
  }

  async function revoke(device: Device) {
    if (!window.confirm(`撤销“${device.name}”后，该设备需要重新登录并注册才能继续同步。`)) return
    setRevokingID(device.id)
    try {
      await request(`/devices/${device.id}`, { method: 'DELETE' })
      await load()
    } catch (error) {
      onError(error instanceof Error ? error.message : '撤销设备失败')
    } finally {
      setRevokingID(null)
    }
  }

  return <section className="status-page">
    <header className="toolbar">
      <div><p className="eyebrow">同步状态</p><h1>设备与冲突</h1></div>
      <button className="icon-button" title="刷新状态" onClick={() => void load()}><RefreshCw size={19} /></button>
    </header>
    <div className="status-grid">
      <section className="status-section">
        <div className="section-title"><Laptop size={19} /><h2>已连接设备</h2><span>{devices.length}</span></div>
        {loading ? <div className="empty compact-empty">正在读取设备...</div> : devices.length === 0 ? <div className="empty compact-empty"><MonitorCog size={30} /><strong>尚未注册设备</strong><span>桌面客户端开始同步后会显示在这里。</span></div> : <div>{devices.map((device) => <div className="device-row" key={device.id}><span className="device-icon"><Laptop size={19} /></span><div><strong>{device.name}</strong><small>{device.platform || '未知平台'}</small></div><div className="device-meta"><small>最近在线</small><span>{formatDate(device.last_seen_at)}</span><small>同步游标 {device.last_applied_change_id}</small></div><div className={`device-sync ${device.last_sync_status || 'unknown'}`}>{device.last_sync_status === 'success' ? <CheckCircle2 size={17} /> : device.last_sync_status === 'error' ? <XCircle size={17} /> : <MonitorCog size={17} />}<div><strong>{device.last_sync_status === 'success' ? '同步成功' : device.last_sync_status === 'error' ? '同步失败' : '尚未同步'}</strong><small>{formatDate(device.last_sync_at)}</small>{device.last_sync_error && <span title={device.last_sync_error}>{device.last_sync_error}</span>}</div></div><button className="icon-button device-revoke" title={`撤销 ${device.name}`} disabled={revokingID === device.id} onClick={() => void revoke(device)}><Trash2 size={17} /></button></div>)}</div>}
      </section>
      <section className="status-section">
        <div className="section-title"><AlertTriangle size={19} /><h2>待处理冲突</h2><span className={conflicts.length ? 'warning-count' : ''}>{conflicts.length}</span></div>
        {loading ? <div className="empty compact-empty">正在读取冲突...</div> : conflicts.length === 0 ? <div className="empty compact-empty"><AlertTriangle size={30} /><strong>没有待处理冲突</strong><span>所有设备的变更目前可以安全同步。</span></div> : <div>{conflicts.map((conflict) => <div className="conflict-row" key={conflict.id}><div><strong>{conflict.path}</strong><small>本地版本 {conflict.local_version ?? '--'} · 云端版本 {conflict.remote_version ?? '--'} · {formatDate(conflict.created_at)}</small></div><div className="resolution-actions"><button onClick={() => void resolve(conflict.id, 'keep_local')}>保留本地</button><button onClick={() => void resolve(conflict.id, 'keep_remote')}>保留云端</button><button className="primary" onClick={() => void resolve(conflict.id, 'keep_both')}>两者保留</button></div></div>)}</div>}
      </section>
    </div>
  </section>
}
