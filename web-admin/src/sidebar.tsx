import { Clock3, Folder, KeyRound, LogOut, MonitorCog, Trash2 } from "lucide-react";
import { formatSize, type StorageUsage, type User } from "./api";

export type Page = "files" | "sync" | "trash" | "activity" | "account";

const pages: Array<{ id: Page; label: string; icon: typeof Folder }> = [
  { id: "files", label: "文件", icon: Folder },
  { id: "trash", label: "回收站", icon: Trash2 },
  { id: "activity", label: "活动记录", icon: Clock3 },
  { id: "sync", label: "同步状态", icon: MonitorCog },
  { id: "account", label: "账户服务", icon: KeyRound },
];

export function Sidebar({ page, usage, user, onNavigate, onLogout }: {
  page: Page;
  usage: StorageUsage | null;
  user: User | null;
  onNavigate: (page: Page) => void;
  onLogout: () => void;
}) {
  return (
    <aside className="sidebar">
      <div className="sidebar-brand"><div className="brand-mark small">S</div><span>SyncHub</span></div>
      <nav>
        {pages.map(({ id, label, icon: Icon }) => (
          <button key={id} className={`nav-item ${page === id ? "active" : ""}`} onClick={() => onNavigate(id)}>
            <Icon size={18} />{label}
          </button>
        ))}
      </nav>
      {usage && (
        <div className="usage">
          <span>云端空间</span>
          <strong>{formatSize(usage.bytes_used)}{usage.quota_bytes > 0 && ` / ${formatSize(usage.quota_bytes)}`}</strong>
          {usage.quota_bytes > 0 && (
            <div className="usage-meter" role="progressbar" aria-label="云端空间使用率" aria-valuemin={0} aria-valuemax={usage.quota_bytes} aria-valuenow={Math.min(usage.bytes_used, usage.quota_bytes)}>
              <span style={{ width: `${Math.min(100, (usage.bytes_used / usage.quota_bytes) * 100)}%` }} />
            </div>
          )}
          <small>{usage.file_count} 个文件{usage.quota_bytes > 0 && `，剩余 ${formatSize(Math.max(0, usage.quota_bytes - usage.bytes_used))}`}</small>
        </div>
      )}
      <div className="account"><span>{user?.email || "已登录用户"}</span><button title="退出登录" onClick={onLogout}><LogOut size={18} /></button></div>
    </aside>
  );
}
