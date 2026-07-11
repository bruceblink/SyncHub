import { useEffect, useState } from 'react'
import { History, Pin, RotateCcw, X } from 'lucide-react'
import { formatDate } from './api'

type FileVersion = {
  id: string
  version: number
  size: number
  pinned_at: string | null
  created_at: string
}

type APIRequest = <T>(path: string, options?: { method?: string; body?: unknown }) => Promise<T>

function formatSize(size: number) {
  if (!size) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const power = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1)
  return `${(size / 1024 ** power).toFixed(power ? 1 : 0)} ${units[power]}`
}

export function VersionHistory({ fileID, fileName, request, onClose, onUpdated, onError }: {
  fileID: string
  fileName: string
  request: APIRequest
  onClose: () => void
  onUpdated: () => Promise<void>
  onError: (message: string) => void
}) {
  const [versions, setVersions] = useState<FileVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [busyVersion, setBusyVersion] = useState<number | null>(null)

  async function load() {
    setLoading(true)
    try {
      const data = await request<{ items: FileVersion[] }>(`/files/${fileID}/versions?limit=100`)
      setVersions(data.items)
    } catch (error) {
      onError(error instanceof Error ? error.message : '读取版本历史失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [fileID])

  async function restore(version: FileVersion) {
    setBusyVersion(version.version)
    try {
      await request(`/files/${fileID}/versions/${version.version}/restore`, { method: 'POST', body: {} })
      await onUpdated()
      await load()
    } catch (error) {
      onError(error instanceof Error ? error.message : '恢复版本失败')
    } finally {
      setBusyVersion(null)
    }
  }

  async function togglePin(version: FileVersion) {
    setBusyVersion(version.version)
    try {
      await request(`/files/${fileID}/versions/${version.version}/pin`, { method: version.pinned_at ? 'DELETE' : 'POST' })
      await load()
    } catch (error) {
      onError(error instanceof Error ? error.message : '更新版本保留状态失败')
    } finally {
      setBusyVersion(null)
    }
  }

  return <div className="version-backdrop" role="presentation">
    <aside className="version-panel" aria-label={`${fileName} 的版本历史`}>
      <header><div><p className="eyebrow">版本历史</p><h2>{fileName}</h2></div><button className="icon-button" title="关闭版本历史" onClick={onClose}><X size={19} /></button></header>
      {loading ? <div className="empty compact-empty">正在读取版本...</div> : versions.length === 0 ? <div className="empty compact-empty"><History size={30} /><strong>尚无历史版本</strong></div> : <div className="versions-list">{versions.map((version) => <div className="version-row" key={version.id}><div><strong>版本 {version.version}</strong><small>{formatDate(version.created_at)} · {formatSize(version.size)}</small></div><div className="version-actions"><button className={version.pinned_at ? 'pinned' : ''} title={version.pinned_at ? '取消固定此版本' : '固定此版本'} disabled={busyVersion === version.version} onClick={() => void togglePin(version)}><Pin size={16} /></button><button className="restore-button" disabled={busyVersion === version.version} onClick={() => void restore(version)}><RotateCcw size={15} />恢复</button></div></div>)}</div>}
    </aside>
  </div>
}
