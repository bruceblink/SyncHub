import { useEffect, useState } from 'react'
import { File, Folder, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'

type TrashItem = { id: string; name: string; path: string; node_type: 'file' | 'directory'; size: number; deleted_at: string }
type Request = <T>(path: string, options?: { method?: string; body?: unknown }) => Promise<T>

function formatDate(value: string) { return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function formatSize(size: number) { if (!size) return '--'; const units = ['B', 'KB', 'MB', 'GB']; const p = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 3); return `${(size / 1024 ** p).toFixed(p ? 1 : 0)} ${units[p]}` }

export function Trash({ request, onError }: { request: Request; onError: (message: string) => void }) {
  const [items, setItems] = useState<TrashItem[]>([])
  const [loading, setLoading] = useState(true)
  const [restoring, setRestoring] = useState<string | null>(null)
  async function load() { setLoading(true); try { const data = await request<{ items: TrashItem[] }>('/trash?page_size=100'); setItems(data.items) } catch (error) { onError(error instanceof Error ? error.message : '读取回收站失败') } finally { setLoading(false) } }
  useEffect(() => { void load() }, [])
  async function restore(item: TrashItem) { setRestoring(item.id); try { await request(`/trash/${item.id}/restore`, { method: 'POST', body: {} }); await load() } catch (error) { onError(error instanceof Error ? error.message : '恢复失败') } finally { setRestoring(null) } }
  return <section className="trash-page"><header className="toolbar"><div><p className="eyebrow">云端回收站</p><h1>已删除的文件</h1></div><button className="icon-button" title="刷新回收站" onClick={() => void load()}><RefreshCw size={19} /></button></header><div className="file-area"><div className="list-head"><span>名称</span><span>大小</span><span>删除时间</span><span /></div>{loading ? <div className="empty">正在读取回收站...</div> : !items.length ? <div className="empty"><Trash2 size={36} /><strong>回收站为空</strong><span>删除的云端文件会显示在这里，可随时恢复。</span></div> : items.map((item) => <div className="file-row" key={item.id}><div className="file-name"><span className={item.node_type === 'directory' ? 'folder-icon' : 'file-icon'}>{item.node_type === 'directory' ? <Folder size={21} fill="currentColor" /> : <File size={21} />}</span><span>{item.path}</span></div><span>{item.node_type === 'file' ? formatSize(item.size) : '--'}</span><span>{formatDate(item.deleted_at)}</span><button className="restore-button" disabled={restoring === item.id} onClick={() => void restore(item)}><RotateCcw size={15} />恢复</button></div>)}</div></section>
}
