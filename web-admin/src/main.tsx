import { createRoot } from 'react-dom/client'
import { useEffect, useMemo, useState } from 'react'
import {
  ChevronRight, Download, File, Folder, FolderPlus, LogIn, LogOut,
  MoreHorizontal, Pencil, Trash2, Upload, X,
} from 'lucide-react'
import './styles.css'

const api = '/api/v1'
const tokenKey = 'synchub.accessToken'
const userKey = 'synchub.user'

type User = { id: string; email: string; status: string }
type FileNode = { id: string; name: string; path: string; node_type: 'file' | 'directory'; size: number; version: number; updated_at: string }
type FileList = { items: FileNode[]; next_cursor: string }
type AuthResponse = { user: User; tokens: { access_token: string; refresh_token: string; expires_in: number } }
type UploadSession = { upload_id: string }
type Modal = { type: 'folder' } | { type: 'rename' | 'delete'; item: FileNode }
type RequestOptions = { token?: string; method?: string; body?: unknown; headers?: Record<string, string> }

function errorMessage(error: unknown) { return error instanceof Error ? error.message : '请求失败' }

async function request<T>(path: string, { token, method = 'GET', body, headers = {} }: RequestOptions = {}): Promise<T> {
  const isBinary = body instanceof Blob
  const response = await fetch(`${api}${path}`, {
    method,
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...headers, ...(body && !isBinary ? { 'Content-Type': 'application/json' } : {}) },
    body: body === undefined ? undefined : isBinary ? body : JSON.stringify(body),
  })
  const payload: { code?: number; message?: string; data?: T } = await response.json().catch(() => ({}))
  if (!response.ok || payload.code !== 0 || payload.data === undefined) throw new Error(payload.message || '请求失败')
  return payload.data
}

function formatSize(size: number) {
  if (!size) return '--'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const power = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1)
  return `${(size / (1024 ** power)).toFixed(power ? 1 : 0)} ${units[power]}`
}
function formatDate(value: string) { return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '--' }

function Auth({ onAuthenticated }: { onAuthenticated: (token: string, user: User) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError('')
    try {
      const data = await request<AuthResponse>(`/auth/${mode}`, { method: 'POST', body: { email, password } })
      localStorage.setItem(tokenKey, data.tokens.access_token); localStorage.setItem(userKey, JSON.stringify(data.user)); onAuthenticated(data.tokens.access_token, data.user)
    } catch (error) { setError(errorMessage(error)) } finally { setBusy(false) }
  }
  return <main className="auth-page"><section className="auth-panel"><div className="brand-mark">S</div><p className="eyebrow">SYNCHUB</p><h1>{mode === 'login' ? '访问你的云端文件' : '创建 SyncHub 账户'}</h1><form onSubmit={submit}><label>邮箱<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required autoComplete="email" /></label><label>密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} required minLength={8} autoComplete={mode === 'login' ? 'current-password' : 'new-password'} /></label>{error && <p className="form-error">{error}</p>}<button className="primary wide" disabled={busy}><LogIn size={18} />{busy ? '正在处理...' : mode === 'login' ? '登录' : '注册并登录'}</button></form><button className="text-button" onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError('') }}>{mode === 'login' ? '没有账户？创建一个' : '已有账户？登录'}</button></section></main>
}

function App() {
  const savedUser = useMemo<User | null>(() => { try { return JSON.parse(localStorage.getItem(userKey) || 'null') as User | null } catch { return null } }, [])
  const [token, setToken] = useState(() => localStorage.getItem(tokenKey) || '')
  const [user, setUser] = useState<User | null>(savedUser)
  const [parent, setParent] = useState<string | null>(null)
  const [trail, setTrail] = useState<FileNode[]>([])
  const [items, setItems] = useState<FileNode[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [modal, setModal] = useState<Modal | null>(null)
  const [menuID, setMenuID] = useState<string | null>(null)
  const currentPath = trail.length ? trail[trail.length - 1].path : '我的文件'
  function logout() { localStorage.removeItem(tokenKey); localStorage.removeItem(userKey); setToken(''); setUser(null); setItems([]); setTrail([]); setParent(null) }
  async function load(parentID = parent) {
    if (!token) return
    setLoading(true); setError('')
    try { const data = await request<FileList>(`/files${parentID ? `?parent_id=${encodeURIComponent(parentID)}&page_size=100` : '?page_size=100'}`, { token }); setItems(data.items) }
    catch (error) { if (errorMessage(error).includes('access token')) logout(); else setError(errorMessage(error)) } finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [token, parent])
  function navigate(folder: FileNode) { setTrail((previous) => [...previous, folder]); setParent(folder.id) }
  function goTo(index: number) { const next = trail.slice(0, index + 1); setTrail(next); setParent(next.length ? next[next.length - 1].id : null) }
  async function createFolder(name: string) { await request('/files/directories', { token, method: 'POST', body: { path: `${trail.length ? trail[trail.length - 1].path : ''}/${name}` } }); await load() }
  async function rename(item: FileNode, name: string) { const base = item.path.slice(0, item.path.length - item.name.length); await request(`/files/${item.id}`, { token, method: 'PATCH', body: { path: `${base}${name}`, base_version: item.version } }); await load() }
  async function remove(item: FileNode) { await request(`/files/${item.id}`, { token, method: 'DELETE', body: { base_version: item.version } }); await load() }
  async function upload(file: File) {
    const bytes = new Uint8Array(await file.arrayBuffer()); const digest = await crypto.subtle.digest('SHA-256', bytes); const checksum = [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, '0')).join('')
    const parentPath = trail.length ? trail[trail.length - 1].path : ''
    const session = await request<UploadSession>('/uploads', { token, method: 'POST', body: { path: `${parentPath}/${file.name}`, size: file.size, sha256: checksum, chunk_size: Math.max(file.size, 1) } })
    await request(`/uploads/${session.upload_id}/chunks/0`, { token, method: 'PUT', body: file, headers: { 'X-Chunk-Sha256': checksum } }); await request(`/uploads/${session.upload_id}/commit`, { token, method: 'POST' }); await load()
  }
  async function download(item: FileNode) {
    const response = await fetch(`${api}/files/${item.id}/content`, { headers: { Authorization: `Bearer ${token}` } }); if (!response.ok) throw new Error('下载失败')
    const url = URL.createObjectURL(await response.blob()); const link = document.createElement('a'); link.href = url; link.download = item.name; link.click(); URL.revokeObjectURL(url)
  }
  if (!token) return <Auth onAuthenticated={(nextToken, nextUser) => { setToken(nextToken); setUser(nextUser) }} />
  return <main className="app-shell" onClick={() => setMenuID(null)}><aside className="sidebar"><div className="sidebar-brand"><div className="brand-mark small">S</div><span>SyncHub</span></div><nav><a className="nav-item active"><Folder size={18} />文件</a></nav><div className="account"><span>{user?.email || '已登录用户'}</span><button title="退出登录" onClick={logout}><LogOut size={18} /></button></div></aside><section className="workspace"><header className="toolbar"><div><p className="eyebrow">云端文件</p><div className="breadcrumbs"><button onClick={() => goTo(-1)}>我的文件</button>{trail.map((entry, index) => <span key={entry.id}><ChevronRight size={16} /><button onClick={() => goTo(index)}>{entry.name}</button></span>)}</div></div><div className="actions"><button className="icon-button" title="新建文件夹" onClick={() => setModal({ type: 'folder' })}><FolderPlus size={19} /></button><label className="primary upload-button"><Upload size={18} />上传文件<input type="file" onChange={async (event) => { const file = event.target.files?.[0]; if (!file) return; try { await upload(file) } catch (error) { setError(errorMessage(error)) } finally { event.target.value = '' } }} /></label></div></header>{error && <div className="notice"><span>{error}</span><button onClick={() => setError('')}><X size={16} /></button></div>}<div className="file-area"><div className="list-head"><span>名称</span><span>大小</span><span>修改时间</span><span /></div>{loading ? <div className="empty">正在读取文件...</div> : items.length === 0 ? <div className="empty"><Folder size={36} /><strong>{currentPath} 还是空的</strong><span>上传文件或新建文件夹开始管理云端内容。</span></div> : items.map((item) => <div className="file-row" key={item.id}><button className="file-name" onClick={() => item.node_type === 'directory' && navigate(item)}><span className={item.node_type === 'directory' ? 'folder-icon' : 'file-icon'}>{item.node_type === 'directory' ? <Folder size={21} fill="currentColor" /> : <File size={21} />}</span><span>{item.name}</span></button><span>{item.node_type === 'file' ? formatSize(item.size) : '--'}</span><span>{formatDate(item.updated_at)}</span><div className="row-menu"><button className="icon-button compact" title="更多操作" onClick={(event) => { event.stopPropagation(); setMenuID(menuID === item.id ? null : item.id) }}><MoreHorizontal size={19} /></button>{menuID === item.id && <div className="menu" onClick={(event) => event.stopPropagation()}>{item.node_type === 'file' && <button onClick={() => { void download(item).catch((error: unknown) => setError(errorMessage(error))); setMenuID(null) }}><Download size={16} />下载</button>}<button onClick={() => { setModal({ type: 'rename', item }); setMenuID(null) }}><Pencil size={16} />重命名</button><button className="danger" onClick={() => { setModal({ type: 'delete', item }); setMenuID(null) }}><Trash2 size={16} />删除</button></div>}</div></div>)}</div></section>{modal && <Dialog modal={modal} onClose={() => setModal(null)} onConfirm={async (value) => { try { if (modal.type === 'folder') await createFolder(value); else if (modal.type === 'rename') await rename(modal.item, value); else await remove(modal.item); setModal(null) } catch (error) { setError(errorMessage(error)) } }} />}</main>
}

function Dialog({ modal, onClose, onConfirm }: { modal: Modal; onClose: () => void; onConfirm: (value: string) => void | Promise<void> }) {
  const isDelete = modal.type === 'delete'; const [value, setValue] = useState(modal.type === 'folder' ? '' : modal.item.name)
  return <div className="dialog-backdrop" role="presentation"><form className="dialog" onSubmit={(event) => { event.preventDefault(); void onConfirm(value) }}><h2>{isDelete ? '删除项目？' : modal.type === 'rename' ? '重命名' : '新建文件夹'}</h2>{isDelete ? <p>“{modal.item.name}” 将从云端文件中删除。</p> : <label>名称<input autoFocus value={value} onChange={(event) => setValue(event.target.value)} required /></label>}<div className="dialog-actions"><button type="button" onClick={onClose}>取消</button><button className={isDelete ? 'danger-button' : 'primary'}>{isDelete ? '删除' : '确认'}</button></div></form></div>
}

const root = document.getElementById('root')
if (!root) throw new Error('missing application root')
createRoot(root).render(<App />)
