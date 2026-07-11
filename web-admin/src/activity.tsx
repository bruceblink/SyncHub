import { useEffect, useState } from 'react'
import { Clock3, FilePlus2, FolderInput, Pencil, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { formatDate } from './api'

type ActivityEvent = {
  id: number
  event_type: 'create' | 'update' | 'move' | 'delete' | 'restore'
  path: string
  old_path: string | null
  source_device_id: string | null
  created_at: string
}

type ActivityPage = { items: ActivityEvent[]; next_cursor: number }
type Request = <T>(path: string, options?: { method?: string; body?: unknown }) => Promise<T>

const labels: Record<ActivityEvent['event_type'], string> = {
  create: '已创建', update: '已更新', move: '已移动', delete: '已删除', restore: '已恢复',
}

function icon(type: ActivityEvent['event_type']) {
  switch (type) {
    case 'create': return <FilePlus2 size={18} />
    case 'update': return <Pencil size={18} />
    case 'move': return <FolderInput size={18} />
    case 'delete': return <Trash2 size={18} />
    case 'restore': return <RotateCcw size={18} />
  }
}

export function Activity({ request, onError }: { request: Request; onError: (message: string) => void }) {
  const [items, setItems] = useState<ActivityEvent[]>([])
  const [nextCursor, setNextCursor] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)

  async function load(beforeEventID = 0) {
    if (beforeEventID) setLoadingMore(true); else setLoading(true)
    try {
      const data = await request<ActivityPage>(`/activity?limit=50${beforeEventID ? `&before_event_id=${beforeEventID}` : ''}`)
      setItems((current) => beforeEventID ? [...current, ...data.items] : data.items)
      setNextCursor(data.next_cursor)
    } catch (error) {
      onError(error instanceof Error ? error.message : '读取活动记录失败')
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }

  useEffect(() => { void load() }, [])

  return <section className="activity-page">
    <header className="toolbar"><div><p className="eyebrow">云端活动</p><h1>最近操作</h1></div><button className="icon-button" title="刷新活动记录" onClick={() => void load()}><RefreshCw size={19} /></button></header>
    <section className="activity-list">
      {loading ? <div className="empty compact-empty">正在读取活动记录...</div> : items.length === 0 ? <div className="empty compact-empty"><Clock3 size={32} /><strong>还没有云端操作</strong><span>上传、重命名、移动和删除文件后，记录会显示在这里。</span></div> : <>{items.map((event) => <article className={`activity-row activity-${event.event_type}`} key={event.id}><span className="activity-icon">{icon(event.event_type)}</span><div className="activity-summary"><strong>{labels[event.event_type]}</strong><span>{event.path}</span>{event.old_path && event.old_path !== event.path && <small>原路径：{event.old_path}</small>}</div><time>{formatDate(event.created_at)}</time></article>)}{nextCursor > 0 && <button className="load-more" disabled={loadingMore} onClick={() => void load(nextCursor)}>{loadingMore ? '正在加载...' : '加载更早记录'}</button>}</>}
    </section>
  </section>
}
