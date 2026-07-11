import { createRoot } from "react-dom/client";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronRight,
  Clock3,
  Download,
  Eye,
  File,
  Folder,
  FolderPlus,
  History,
  LogIn,
  LogOut,
  MonitorCog,
  Search,
  MoreHorizontal,
  Pencil,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import "./styles.css";
import { SyncStatus } from "./sync-status";
import { VersionHistory } from "./version-history";
import { Trash } from "./trash";
import { Activity } from "./activity";
import { canPreview, FilePreview } from "./file-preview";
import { UploadQueue, type UploadTask } from "./upload-queue";
import {
  prepareFile,
  transferredProgress,
  uploadChunkSize,
} from "./chunked-upload";

const api = "/api/v1";
const tokenKey = "synchub.accessToken";
const userKey = "synchub.user";

type User = { id: string; email: string; status: string };
type FileNode = {
  id: string;
  name: string;
  path: string;
  node_type: "file" | "directory";
  size: number;
  version: number;
  updated_at: string;
};
type FileListResponse = { items: FileNode[]; next_cursor: string };
type AuthResponse = {
  user: User;
  tokens: { access_token: string; refresh_token: string; expires_in: number };
};
type UploadSession = { upload_id: string };
type StorageUsage = { file_count: number; bytes_used: number };
type Modal = { type: "folder" } | { type: "rename" | "delete"; item: FileNode };
type RequestOptions = {
  token?: string;
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
};

function uploadChunk(
  path: string,
  token: string,
  blob: Blob,
  checksum: string,
  onProgress: (progress: number) => void,
  signal?: AbortSignal,
) {
  return new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", `${api}${path}`);
    xhr.setRequestHeader("Authorization", `Bearer ${token}`);
    xhr.setRequestHeader("X-Chunk-Sha256", checksum);
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    };
    xhr.onerror = () => reject(new Error("网络连接中断"));
    xhr.onabort = () => reject(new DOMException("上传已取消", "AbortError"));
    xhr.onload = () => {
      let payload: { code?: number; message?: string } = {};
      try {
        payload = JSON.parse(xhr.responseText) as typeof payload;
      } catch {
        // The status code below still provides a useful fallback error.
      }
      if (xhr.status >= 200 && xhr.status < 300 && payload.code === 0) resolve();
      else reject(new Error(payload.message || "上传失败"));
    };
    signal?.addEventListener("abort", () => xhr.abort(), { once: true });
    if (signal?.aborted) xhr.abort();
    else xhr.send(blob);
  });
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请求失败";
}

async function request<T>(
  path: string,
  { token, method = "GET", body, headers = {} }: RequestOptions = {},
): Promise<T> {
  const isBinary = body instanceof Blob;
  const response = await fetch(`${api}${path}`, {
    method,
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...headers,
      ...(body && !isBinary ? { "Content-Type": "application/json" } : {}),
    },
    body:
      body === undefined ? undefined : isBinary ? body : JSON.stringify(body),
  });
  const payload: { code?: number; message?: string; data?: T } = await response
    .json()
    .catch(() => ({}));
  if (!response.ok || payload.code !== 0 || payload.data === undefined)
    throw new Error(payload.message || "请求失败");
  return payload.data;
}

function formatSize(size: number) {
  if (!size) return "--";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const power = Math.min(
    Math.floor(Math.log(size) / Math.log(1024)),
    units.length - 1,
  );
  return `${(size / 1024 ** power).toFixed(power ? 1 : 0)} ${units[power]}`;
}
function formatDate(value: string) {
  return value
    ? new Intl.DateTimeFormat("zh-CN", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(value))
    : "--";
}

function Auth({
  onAuthenticated,
}: {
  onAuthenticated: (token: string, user: User) => void;
}) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const data = await request<AuthResponse>(`/auth/${mode}`, {
        method: "POST",
        body: { email, password },
      });
      localStorage.setItem(tokenKey, data.tokens.access_token);
      localStorage.setItem(userKey, JSON.stringify(data.user));
      onAuthenticated(data.tokens.access_token, data.user);
    } catch (error) {
      setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-page">
      <section className="auth-panel">
        <div className="brand-mark">S</div>
        <p className="eyebrow">SYNCHUB</p>
        <h1>{mode === "login" ? "访问你的云端文件" : "创建 SyncHub 账户"}</h1>
        <form onSubmit={submit}>
          <label>
            邮箱
            <input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
              autoComplete="email"
            />
          </label>
          <label>
            密码
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
              minLength={8}
              autoComplete={
                mode === "login" ? "current-password" : "new-password"
              }
            />
          </label>
          {error && <p className="form-error">{error}</p>}
          <button className="primary wide" disabled={busy}>
            <LogIn size={18} />
            {busy ? "正在处理..." : mode === "login" ? "登录" : "注册并登录"}
          </button>
        </form>
        <button
          className="text-button"
          onClick={() => {
            setMode(mode === "login" ? "register" : "login");
            setError("");
          }}
        >
          {mode === "login" ? "没有账户？创建一个" : "已有账户？登录"}
        </button>
      </section>
    </main>
  );
}

function App() {
  const savedUser = useMemo<User | null>(() => {
    try {
      return JSON.parse(localStorage.getItem(userKey) || "null") as User | null;
    } catch {
      return null;
    }
  }, []);
  const [token, setToken] = useState(
    () => localStorage.getItem(tokenKey) || "",
  );
  const [user, setUser] = useState<User | null>(savedUser);
  const [page, setPage] = useState<"files" | "sync" | "trash" | "activity">(
    "files",
  );
  const [parent, setParent] = useState<string | null>(null);
  const [trail, setTrail] = useState<FileNode[]>([]);
  const [items, setItems] = useState<FileNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [modal, setModal] = useState<Modal | null>(null);
  const [menuID, setMenuID] = useState<string | null>(null);
  const [versionFile, setVersionFile] = useState<FileNode | null>(null);
  const [previewFile, setPreviewFile] = useState<FileNode | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<FileNode[] | null>(null);
  const [usage, setUsage] = useState<StorageUsage | null>(null);
  const [uploadTasks, setUploadTasks] = useState<UploadTask[]>([]);
  const uploadControllers = useRef(new Map<string, AbortController>());
  const currentPath = trail.length ? trail[trail.length - 1].path : "我的文件";
  function logout() {
    uploadControllers.current.forEach((controller) => controller.abort());
    uploadControllers.current.clear();
    localStorage.removeItem(tokenKey);
    localStorage.removeItem(userKey);
    setToken("");
    setUser(null);
    setItems([]);
    setTrail([]);
    setParent(null);
    setUploadTasks([]);
  }
  async function load(parentID = parent) {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const data = await request<FileListResponse>(
        `/files${parentID ? `?parent_id=${encodeURIComponent(parentID)}&page_size=100` : "?page_size=100"}`,
        { token },
      );
      setItems(data.items);
    } catch (error) {
      if (errorMessage(error).includes("access token")) logout();
      else setError(errorMessage(error));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, [token, parent]);
  useEffect(() => {
    if (token) {
      void request<StorageUsage>("/account/usage", { token })
        .then(setUsage)
        .catch(() => {});
    }
  }, [token, items]);
  async function authenticatedRequest<T>(
    path: string,
    options: Omit<RequestOptions, "token"> = {},
  ) {
    return request<T>(path, { ...options, token });
  }
  async function search(query: string) {
    const trimmed = query.trim();
    setSearchQuery(query);
    if (!trimmed) {
      setSearchResults(null);
      return;
    }
    try {
      const data = await request<FileListResponse>(
        `/files/search?q=${encodeURIComponent(trimmed)}&page_size=100`,
        { token },
      );
      setSearchResults(data.items);
    } catch (error) {
      setError(errorMessage(error));
    }
  }
  function navigate(folder: FileNode) {
    setTrail((previous) => [...previous, folder]);
    setParent(folder.id);
  }
  function goTo(index: number) {
    const next = trail.slice(0, index + 1);
    setTrail(next);
    setParent(next.length ? next[next.length - 1].id : null);
  }
  async function createFolder(name: string) {
    await request("/files/directories", {
      token,
      method: "POST",
      body: {
        path: `${trail.length ? trail[trail.length - 1].path : ""}/${name}`,
      },
    });
    await load();
  }
  async function rename(item: FileNode, name: string) {
    const base = item.path.slice(0, item.path.length - item.name.length);
    await request(`/files/${item.id}`, {
      token,
      method: "PATCH",
      body: { path: `${base}${name}`, base_version: item.version },
    });
    await load();
  }
  async function remove(item: FileNode) {
    await request(`/files/${item.id}`, {
      token,
      method: "DELETE",
      body: { base_version: item.version },
    });
    await load();
  }
  function updateUploadTask(id: string, update: Partial<UploadTask>) {
    setUploadTasks((tasks) =>
      tasks.map((task) => (task.id === id ? { ...task, ...update } : task)),
    );
  }
  async function upload(task: UploadTask) {
    const controller = new AbortController();
    let uploadID = "";
    uploadControllers.current.set(task.id, controller);
    updateUploadTask(task.id, {
      state: "hashing",
      progress: 3,
      error: undefined,
    });
    try {
      const file = task.file;
      const prepared = await prepareFile(
        file,
        (progress) =>
          updateUploadTask(task.id, { progress: 3 + Math.round(progress * 9) }),
        controller.signal,
      );
      const session = await request<UploadSession>("/uploads", {
        token,
        method: "POST",
        body: {
          path: task.targetPath,
          size: file.size,
          sha256: prepared.checksum,
          chunk_size: uploadChunkSize,
        },
      });
      uploadID = session.upload_id;
      updateUploadTask(task.id, { uploadID: session.upload_id });
      if (controller.signal.aborted) {
        await request(`/uploads/${session.upload_id}`, { token, method: "DELETE" });
        throw new DOMException("上传已取消", "AbortError");
      }
      updateUploadTask(task.id, { state: "uploading", progress: 12 });
      let completedBytes = 0;
      for (const chunk of prepared.chunks) {
        await uploadChunk(
          `/uploads/${session.upload_id}/chunks/${chunk.index}`,
          token,
          chunk.blob,
          chunk.checksum,
          (progress) => {
            const transferred = transferredProgress(
              completedBytes,
              progress * chunk.blob.size,
              file.size,
            );
            updateUploadTask(task.id, {
              progress: 12 + Math.round(transferred * 80),
            });
          },
          controller.signal,
        );
        completedBytes += chunk.blob.size;
      }
      updateUploadTask(task.id, { state: "committing", progress: 94 });
      await request(`/uploads/${session.upload_id}/commit`, {
        token,
        method: "POST",
      });
      updateUploadTask(task.id, { state: "complete", progress: 100 });
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") {
        if (uploadID) {
          try {
            await request(`/uploads/${uploadID}`, { token, method: "DELETE" });
          } catch (abortError) {
            setError(`取消上传失败：${errorMessage(abortError)}`);
          }
        }
        updateUploadTask(task.id, { state: "cancelled", error: undefined });
      } else {
        updateUploadTask(task.id, { state: "failed", error: errorMessage(error) });
      }
    } finally {
      uploadControllers.current.delete(task.id);
    }
  }
  async function cancelUpload(id: string) {
    const task = uploadTasks.find((item) => item.id === id);
    if (!task) return;
    uploadControllers.current.get(id)?.abort();
    updateUploadTask(id, { state: "cancelled", error: undefined });
    if (task.uploadID) {
      try {
        await request(`/uploads/${task.uploadID}`, { token, method: "DELETE" });
      } catch (error) {
        setError(`取消上传失败：${errorMessage(error)}`);
      }
    }
  }
  async function retryUpload(id: string) {
    const task = uploadTasks.find((item) => item.id === id);
    if (!task || task.state !== "failed") return;
    if (task.uploadID) {
      try {
        await request(`/uploads/${task.uploadID}`, { token, method: "DELETE" });
      } catch (error) {
        setError(`清理失败上传会话失败：${errorMessage(error)}`);
        return;
      }
    }
    updateUploadTask(id, {
      state: "queued",
      progress: 0,
      error: undefined,
      uploadID: undefined,
    });
  }
  useEffect(() => {
    const next = uploadTasks.find((task) => task.state === "queued");
    const busy = uploadTasks.some((task) =>
      ["hashing", "uploading", "committing"].includes(task.state),
    );
    if (!next || busy) return;
    void upload(next).then(() => load());
  }, [uploadTasks]);
  function queueUploads(files: FileList) {
    const parentPath = trail.length ? trail[trail.length - 1].path : "";
    const tasks = Array.from(files).map<UploadTask>((file) => ({
      id: crypto.randomUUID(),
      file,
      targetPath: `${parentPath}/${file.name}`,
      state: "queued",
      progress: 0,
    }));
    setUploadTasks((current) => [...current, ...tasks]);
  }
  async function download(item: FileNode) {
    const response = await fetch(`${api}/files/${item.id}/content`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!response.ok) throw new Error("下载失败");
    const url = URL.createObjectURL(await response.blob());
    const link = document.createElement("a");
    link.href = url;
    link.download = item.name;
    link.click();
    URL.revokeObjectURL(url);
  }
  if (!token)
    return (
      <Auth
        onAuthenticated={(nextToken, nextUser) => {
          setToken(nextToken);
          setUser(nextUser);
        }}
      />
    );
  const visibleItems = searchResults ?? items;
  return (
    <main className="app-shell" onClick={() => setMenuID(null)}>
      <aside className="sidebar">
        <div className="sidebar-brand">
          <div className="brand-mark small">S</div>
          <span>SyncHub</span>
        </div>
        <nav>
          <button
            className={`nav-item ${page === "files" ? "active" : ""}`}
            onClick={() => setPage("files")}
          >
            <Folder size={18} />
            文件
          </button>
          <button
            className={`nav-item ${page === "trash" ? "active" : ""}`}
            onClick={() => setPage("trash")}
          >
            <Trash2 size={18} />
            回收站
          </button>
          <button
            className={`nav-item ${page === "activity" ? "active" : ""}`}
            onClick={() => setPage("activity")}
          >
            <Clock3 size={18} />
            活动记录
          </button>
          <button
            className={`nav-item ${page === "sync" ? "active" : ""}`}
            onClick={() => setPage("sync")}
          >
            <MonitorCog size={18} />
            同步状态
          </button>
        </nav>
        {usage && (
          <div className="usage">
            <span>云端空间</span>
            <strong>{formatSize(usage.bytes_used)}</strong>
            <small>{usage.file_count} 个文件</small>
          </div>
        )}
        <div className="account">
          <span>{user?.email || "已登录用户"}</span>
          <button title="退出登录" onClick={logout}>
            <LogOut size={18} />
          </button>
        </div>
      </aside>
      <section className="workspace">
        {error && (
          <div className="notice">
            <span>{error}</span>
            <button onClick={() => setError("")}>
              <X size={16} />
            </button>
          </div>
        )}
        {page === "sync" ? (
          <SyncStatus request={authenticatedRequest} onError={setError} />
        ) : page === "trash" ? (
          <Trash request={authenticatedRequest} onError={setError} />
        ) : page === "activity" ? (
          <Activity request={authenticatedRequest} onError={setError} />
        ) : (
          <>
            <header className="toolbar">
              <div>
                <p className="eyebrow">云端文件</p>
                <div className="breadcrumbs">
                  <button onClick={() => goTo(-1)}>我的文件</button>
                  {trail.map((entry, index) => (
                    <span key={entry.id}>
                      <ChevronRight size={16} />
                      <button onClick={() => goTo(index)}>{entry.name}</button>
                    </span>
                  ))}
                </div>
              </div>
              <div className="actions">
                <label className="search-box">
                  <Search size={17} />
                  <input
                    value={searchQuery}
                    placeholder="搜索文件和文件夹"
                    onChange={(event) => void search(event.target.value)}
                  />
                  {searchQuery && (
                    <button title="清除搜索" onClick={() => void search("")}>
                      <X size={15} />
                    </button>
                  )}
                </label>
                <button
                  className="icon-button"
                  title="新建文件夹"
                  onClick={() => setModal({ type: "folder" })}
                >
                  <FolderPlus size={19} />
                </button>
                <label className="primary upload-button">
                  <Upload size={18} />
                  上传文件
                  <input
                    type="file"
                    multiple
                    onChange={(event) => {
                      if (event.target.files?.length)
                        queueUploads(event.target.files);
                      event.target.value = "";
                    }}
                  />
                </label>
              </div>
            </header>
            {searchResults && (
              <p className="search-summary">
                “{searchQuery.trim()}” 的搜索结果：{searchResults.length} 项
              </p>
            )}
            <div className="file-area">
              <div className="list-head">
                <span>名称</span>
                <span>大小</span>
                <span>修改时间</span>
                <span />
              </div>
              {loading && !searchResults ? (
                <div className="empty">正在读取文件...</div>
              ) : visibleItems.length === 0 ? (
                <div className="empty">
                  <Folder size={36} />
                  <strong>
                    {searchResults
                      ? "没有匹配的文件"
                      : `${currentPath} 还是空的`}
                  </strong>
                  <span>
                    {searchResults
                      ? "尝试使用其他关键词。"
                      : "上传文件或新建文件夹开始管理云端内容。"}
                  </span>
                </div>
              ) : (
                visibleItems.map((item) => (
                  <div className="file-row" key={item.id}>
                    <button
                      className="file-name"
                      onClick={() =>
                        item.node_type === "directory" &&
                        (setSearchResults(null),
                        setSearchQuery(""),
                        navigate(item))
                      }
                    >
                      <span
                        className={
                          item.node_type === "directory"
                            ? "folder-icon"
                            : "file-icon"
                        }
                      >
                        {item.node_type === "directory" ? (
                          <Folder size={21} fill="currentColor" />
                        ) : (
                          <File size={21} />
                        )}
                      </span>
                      <span>{searchResults ? item.path : item.name}</span>
                    </button>
                    <span>
                      {item.node_type === "file" ? formatSize(item.size) : "--"}
                    </span>
                    <span>{formatDate(item.updated_at)}</span>
                    <div className="row-menu">
                      <button
                        className="icon-button compact"
                        title="更多操作"
                        onClick={(event) => {
                          event.stopPropagation();
                          setMenuID(menuID === item.id ? null : item.id);
                        }}
                      >
                        <MoreHorizontal size={19} />
                      </button>
                      {menuID === item.id && (
                        <div
                          className="menu"
                          onClick={(event) => event.stopPropagation()}
                        >
                          {item.node_type === "file" && (
                            <>
                              {canPreview(item) && (
                                <button
                                  onClick={() => {
                                    setPreviewFile(item);
                                    setMenuID(null);
                                  }}
                                >
                                  <Eye size={16} />
                                  预览
                                </button>
                              )}
                              <button
                                onClick={() => {
                                  void download(item).catch((error: unknown) =>
                                    setError(errorMessage(error)),
                                  );
                                  setMenuID(null);
                                }}
                              >
                                <Download size={16} />
                                下载
                              </button>
                              <button
                                onClick={() => {
                                  setVersionFile(item);
                                  setMenuID(null);
                                }}
                              >
                                <History size={16} />
                                版本历史
                              </button>
                            </>
                          )}
                          <button
                            onClick={() => {
                              setModal({ type: "rename", item });
                              setMenuID(null);
                            }}
                          >
                            <Pencil size={16} />
                            重命名
                          </button>
                          <button
                            className="danger"
                            onClick={() => {
                              setModal({ type: "delete", item });
                              setMenuID(null);
                            }}
                          >
                            <Trash2 size={16} />
                            删除
                          </button>
                        </div>
                      )}
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        )}
        {modal && (
          <Dialog
            modal={modal}
            onClose={() => setModal(null)}
            onConfirm={async (value) => {
              try {
                if (modal.type === "folder") await createFolder(value);
                else if (modal.type === "rename")
                  await rename(modal.item, value);
                else await remove(modal.item);
                setModal(null);
              } catch (error) {
                setError(errorMessage(error));
              }
            }}
          />
        )}
        {versionFile && (
          <VersionHistory
            fileID={versionFile.id}
            fileName={versionFile.name}
            request={authenticatedRequest}
            onClose={() => setVersionFile(null)}
            onUpdated={load}
            onError={setError}
          />
        )}
        {previewFile && (
          <FilePreview
            file={previewFile}
            token={token}
            onClose={() => setPreviewFile(null)}
            onDownload={() => download(previewFile)}
            onError={setError}
          />
        )}
        <UploadQueue
          tasks={uploadTasks}
          formatSize={formatSize}
          onRetry={(id) => void retryUpload(id)}
          onDismiss={(id) =>
            setUploadTasks((tasks) =>
              tasks.filter((task) => task.id !== id),
            )
          }
          onCancel={(id) => void cancelUpload(id)}
          onClearCompleted={() =>
            setUploadTasks((tasks) =>
              tasks.filter(
                (task) =>
                  task.state !== "complete" && task.state !== "cancelled",
              ),
            )
          }
        />
      </section>
    </main>
  );
}

function Dialog({
  modal,
  onClose,
  onConfirm,
}: {
  modal: Modal;
  onClose: () => void;
  onConfirm: (value: string) => void | Promise<void>;
}) {
  const isDelete = modal.type === "delete";
  const [value, setValue] = useState(
    modal.type === "folder" ? "" : modal.item.name,
  );
  return (
    <div className="dialog-backdrop" role="presentation">
      <form
        className="dialog"
        onSubmit={(event) => {
          event.preventDefault();
          void onConfirm(value);
        }}
      >
        <h2>
          {isDelete
            ? "删除项目？"
            : modal.type === "rename"
              ? "重命名"
              : "新建文件夹"}
        </h2>
        {isDelete ? (
          <p>“{modal.item.name}” 将从云端文件中删除。</p>
        ) : (
          <label>
            名称
            <input
              autoFocus
              value={value}
              onChange={(event) => setValue(event.target.value)}
              required
            />
          </label>
        )}
        <div className="dialog-actions">
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button className={isDelete ? "danger-button" : "primary"}>
            {isDelete ? "删除" : "确认"}
          </button>
        </div>
      </form>
    </div>
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("missing application root");
createRoot(root).render(<App />);
