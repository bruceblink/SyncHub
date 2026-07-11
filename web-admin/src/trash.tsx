import { useEffect, useState } from 'react'
import { File, Folder, RefreshCw, RotateCcw, Trash2, X } from 'lucide-react'

type TrashItem = { id: string; name: string; path: string; node_type: 'file' | 'directory'; size: number; deleted_at: string }
type Request = <T>(path: string, options?: { method?: string; body?: unknown }) => Promise<T>

function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function formatSize(size: number) { if (!size) return '--'; const units = ['B', 'KB', 'MB', 'GB']; const p = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 3); return `${(size / 1024 ** p).toFixed(p ? 1 : 0)} ${units[p]}` }

export function Trash({ request, onError }: { request: Request; onError: (message: string) => void }) {
  const [items, setItems] = useState<TrashItem[]>([])
  const [loading, setLoading] = useState(true)
  const [restoring, setRestoring] = useState<string | null>(null)
  const [purging, setPurging] = useState<TrashItem | null>(null)
  const [busy, setBusy] = useState(false)
  const [retentionDays, setRetentionDays] = useState(0)
  async function load() { setLoading(true); try { const data = await request<{ items: TrashItem[]; retention_days: number }>('/trash?page_size=100'); setItems(data.items); setRetentionDays(data.retention_days) } catch (error) { onError(error instanceof Error ? error.message : '读取回收站失败') } finally { setLoading(false) } }
  useEffect(() => { void load() }, [])
  async function restore(item: TrashItem) { setRestoring(item.id); try { await request(`/trash/${item.id}/restore`, { method: 'POST', body: {} }); await load() } catch (error) { onError(error instanceof Error ? error.message : '恢复失败') } finally { setRestoring(null) } }
  async function purge() { if (!purging) return; setBusy(true); try { await request(`/trash/${purging.id}`, { method: 'DELETE' }); setPurging(null); await load() } catch (error) { onError(error instanceof Error ? error.message : '彻底删除失败') } finally { setBusy(false) } }
  return <section className="trash-page"><header className="toolbar"><div><p className="eyebrow">云端回收站</p><h1>已删除的文件</h1><p className="retention-note">{retentionDays > 0 ? `项目将在删除 ${retentionDays} 天后自动清理` : '自动清理已关闭'}</p></div><button className="icon-button" title="刷新回收站" onClick={() => void load()}><RefreshCw size={19} /></button></header><div className="file-area"><div className="list-head"><span>名称</span><span>大小</span><span>删除时间</span><span /></div>{loading ? <div className="empty">正在读取回收站...</div> : !items.length ? <div className="empty"><Trash2 size={36} /><strong>回收站为空</strong><span>删除的云端文件会显示在这里，可在保留期内恢复。</span></div> : items.map((item) => <div className="file-row" key={item.id}><div className="file-name"><span className={item.node_type === 'directory' ? 'folder-icon' : 'file-icon'}>{item.node_type === 'directory' ? <Folder size={21} fill="currentColor" /> : <File size={21} />}</span><span>{item.path}</span></div><span>{item.node_type === 'file' ? formatSize(item.size) : '--'}</span><span>{formatDate(item.deleted_at)}</span><div className="trash-actions"><button className="restore-button" disabled={restoring === item.id} onClick={() => void restore(item)}><RotateCcw size={15} />恢复</button><button className="icon-button compact danger-icon" title="彻底删除" onClick={() => setPurging(item)}><Trash2 size={16} /></button></div></div>)}</div>{purging && <div className="dialog-backdrop" role="presentation"><div className="dialog" role="dialog" aria-modal="true" aria-labelledby="purge-title"><button className="dialog-close" title="关闭" onClick={() => setPurging(null)}><X size={18} /></button><h2 id="purge-title">彻底删除？</h2><p>“{purging.name}”{purging.node_type === 'directory' ? '及其中的所有内容' : ''}将无法再恢复。</p><div className="dialog-actions"><button onClick={() => setPurging(null)} disabled={busy}>取消</button><button className="danger-button" onClick={() => void purge()} disabled={busy}>{busy ? '正在删除...' : '彻底删除'}</button></div></div></div>}</section>
}
