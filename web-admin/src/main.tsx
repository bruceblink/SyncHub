import { createRoot } from "react-dom/client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronRight,
  Download,
  Eye,
  File,
  Folder,
  FolderPlus,
  FolderUp,
  History,
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
import { Account } from "./account";
import { canPreview, FilePreview } from "./file-preview";
import { UploadQueue, type UploadTask } from "./upload-queue";
import {
  prepareFile,
  transferredProgress,
  uploadChunkSize,
} from "./chunked-upload";
import {
  api,
  clearAuth,
  errorMessage,
  formatDate,
  formatSize,
  request,
  storeAuth,
  tokenKey,
  userKey,
  type FileListResponse,
  type FileNode,
  type RequestOptions,
  type StorageUsage,
  type UploadSession,
  type User,
  type AuthResponse,
} from "./api";
import { Auth } from "./auth";
import { Dialog, type Modal } from "./dialog";
import { Sidebar, type Page } from "./sidebar";

function relativeUploadPath(file: File) {
  const raw = file.webkitRelativePath.replaceAll("\\", "/");
  const parts = raw
    .split("/")
    .map((part) => part.trim())
    .filter(Boolean);
  if (
    parts.length < 2 ||
    parts.some((part) => part === "." || part === ".." || part.includes("\0"))
  ) {
    throw new Error(`无法识别 ${file.name} 的目录路径`);
  }
  return parts.join("/");
}

function joinCloudPath(base: string, relative: string) {
  return `${base.replace(/\/$/, "")}/${relative.replace(/^\//, "")}`;
}

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

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const oauthCode = params.get("oauth_code");
    const oauthError = params.get("oauth_error");
    if (!oauthCode && !oauthError) return;
    window.history.replaceState({}, "", window.location.pathname);
    if (oauthError) {
      setError(oauthError);
      return;
    }
    void request<AuthResponse>("/auth/oauth/exchange", {
      method: "POST",
      body: { code: oauthCode },
    })
      .then((data) => {
        storeAuth(data);
        setToken(data.tokens.access_token);
        setUser(data.user);
      })
      .catch((error) => setError(errorMessage(error)));
  }, []);
  const [user, setUser] = useState<User | null>(savedUser);
  const [page, setPage] = useState<Page>("files");
  const [parent, setParent] = useState<string | null>(null);
  const [trail, setTrail] = useState<FileNode[]>([]);
  const [items, setItems] = useState<FileNode[]>([]);
  const [filesCursor, setFilesCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [modal, setModal] = useState<Modal | null>(null);
  const [menuID, setMenuID] = useState<string | null>(null);
  const [versionFile, setVersionFile] = useState<FileNode | null>(null);
  const [previewFile, setPreviewFile] = useState<FileNode | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<FileNode[] | null>(null);
  const [searchCursor, setSearchCursor] = useState("");
  const [searchLoadingMore, setSearchLoadingMore] = useState(false);
  const [usage, setUsage] = useState<StorageUsage | null>(null);
  const [uploadTasks, setUploadTasks] = useState<UploadTask[]>([]);
  const [preparingDirectory, setPreparingDirectory] = useState(false);
  const uploadControllers = useRef(new Map<string, AbortController>());
  const filesRequestID = useRef(0);
  const searchRequestID = useRef(0);
  const currentPath = trail.length ? trail[trail.length - 1].path : "我的文件";
  function navigateToPage(nextPage: typeof page) {
    setError("");
    setMenuID(null);
    setPage(nextPage);
  }
  function logout() {
    uploadControllers.current.forEach((controller) => controller.abort());
    uploadControllers.current.clear();
    clearAuth();
    setToken("");
    setUser(null);
    setItems([]);
    setTrail([]);
    setParent(null);
    setUploadTasks([]);
  }
  async function load(parentID = parent, cursor = "") {
    if (!token) return;
    const requestID = ++filesRequestID.current;
    cursor ? setLoadingMore(true) : setLoading(true);
    setError("");
    try {
      const data = await request<FileListResponse>(
        `/files?${parentID ? `parent_id=${encodeURIComponent(parentID)}&` : ""}page_size=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
        { token },
      );
      if (requestID !== filesRequestID.current) return;
      setItems((current) => (cursor ? [...current, ...data.items] : data.items));
      setFilesCursor(data.next_cursor);
    } catch (error) {
      if (requestID !== filesRequestID.current) return;
      if (errorMessage(error).includes("access token")) logout();
      else setError(errorMessage(error));
    } finally {
      if (requestID === filesRequestID.current) {
        setLoading(false);
        setLoadingMore(false);
      }
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
  const authenticatedRequest = useCallback(async function authenticatedRequest<T>(
    path: string,
    options: Omit<RequestOptions, "token"> = {},
  ) {
    return request<T>(path, { ...options, token });
  }, [token]);
  async function search(query: string, cursor = "") {
    const requestID = ++searchRequestID.current;
    const trimmed = query.trim();
    setSearchQuery(query);
    if (!trimmed) {
      setSearchResults(null);
      setSearchCursor("");
      return;
    }
    if (cursor) setSearchLoadingMore(true);
    else {
      setSearchResults([]);
      setSearchCursor("");
    }
    try {
      const data = await request<FileListResponse>(
        `/files/search?q=${encodeURIComponent(trimmed)}&page_size=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
        { token },
      );
      if (requestID !== searchRequestID.current) return;
      setSearchResults((current) =>
        cursor ? [...(current ?? []), ...data.items] : data.items,
      );
      setSearchCursor(data.next_cursor);
    } catch (error) {
      if (requestID !== searchRequestID.current) return;
      setError(errorMessage(error));
    } finally {
      if (requestID === searchRequestID.current) setSearchLoadingMore(false);
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
  async function ensureDirectory(path: string) {
    try {
      return await request<FileNode>("/files/directories", {
        token,
        method: "POST",
        body: { path },
      });
    } catch (createError) {
      try {
        const existing = await request<FileNode>(
          `/files/by-path?path=${encodeURIComponent(path)}`,
          { token },
        );
        if (existing.node_type !== "directory")
          throw new Error(`${path} 已被同名文件占用`);
        return existing;
      } catch (lookupError) {
        if (
          lookupError instanceof Error &&
          lookupError.message.includes("已被同名文件占用")
        )
          throw lookupError;
        throw createError;
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
  function appendUploadTasks(entries: Array<{ file: File; targetPath: string }>) {
    const existingPaths = new Set(
      uploadTasks
        .filter((task) => task.state !== "cancelled")
        .map((task) => task.targetPath),
    );
    const unique = entries.filter((entry) => {
      if (existingPaths.has(entry.targetPath)) return false;
      existingPaths.add(entry.targetPath);
      return true;
    });
    if (unique.length !== entries.length)
      setError("已跳过上传队列中的重复路径");
    const tasks = unique.map<UploadTask>(({ file, targetPath }) => ({
      id: crypto.randomUUID(),
      file,
      targetPath,
      state: "queued",
      progress: 0,
    }));
    setUploadTasks((current) => [...current, ...tasks]);
  }
  function queueUploads(files: FileList) {
    const parentPath = trail.length ? trail[trail.length - 1].path : "";
    appendUploadTasks(
      Array.from(files).map((file) => ({
        file,
        targetPath: joinCloudPath(parentPath, file.name),
      })),
    );
  }
  async function queueDirectory(files: FileList) {
    setPreparingDirectory(true);
    try {
      const parentPath = trail.length ? trail[trail.length - 1].path : "";
      const entries = Array.from(files).map((file) => ({
        file,
        relativePath: relativeUploadPath(file),
      }));
      const directories = new Set<string>();
      for (const entry of entries) {
        const parts = entry.relativePath.split("/");
        for (let depth = 1; depth < parts.length; depth += 1)
          directories.add(parts.slice(0, depth).join("/"));
      }
      const ordered = [...directories].sort(
        (left, right) =>
          left.split("/").length - right.split("/").length ||
          left.localeCompare(right),
      );
      for (const directory of ordered)
        await ensureDirectory(joinCloudPath(parentPath, directory));
      appendUploadTasks(
        entries.map(({ file, relativePath }) => ({
          file,
          targetPath: joinCloudPath(parentPath, relativePath),
        })),
      );
      await load();
    } finally {
      setPreparingDirectory(false);
    }
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
      <Sidebar
        page={page}
        usage={usage}
        user={user}
        onNavigate={navigateToPage}
        onLogout={logout}
      />
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
        ) : page === "account" ? (
          <Account request={authenticatedRequest} email={user?.email ?? ""} onDeleted={logout} onError={setError} />
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
                <label
                  className={`primary upload-button ${preparingDirectory ? "disabled" : ""}`}
                >
                  <Upload size={18} />
                  上传文件
                  <input
                    type="file"
                    multiple
                    disabled={preparingDirectory}
                    onChange={(event) => {
                      if (event.target.files?.length)
                        queueUploads(event.target.files);
                      event.target.value = "";
                    }}
                  />
                </label>
                <label
                  className={`secondary upload-button ${preparingDirectory ? "disabled" : ""}`}
                >
                  <FolderUp size={18} />
                  {preparingDirectory ? "正在准备..." : "上传文件夹"}
                  <input
                    type="file"
                    multiple
                    disabled={preparingDirectory}
                    ref={(input) => input?.setAttribute("webkitdirectory", "")}
                    onChange={(event) => {
                      const files = event.target.files;
                      if (files?.length)
                        void queueDirectory(files).catch((error: unknown) =>
                          setError(errorMessage(error)),
                        );
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
                <>
                {visibleItems.map((item) => (
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
                ))}
                {(searchResults ? searchCursor : filesCursor) && (
                  <button
                    className="load-more"
                    disabled={searchResults ? searchLoadingMore : loadingMore}
                    onClick={() =>
                      void (searchResults
                        ? search(searchQuery, searchCursor)
                        : load(parent, filesCursor))
                    }
                  >
                    {(searchResults ? searchLoadingMore : loadingMore)
                      ? "正在加载..."
                      : "加载更多"}
                  </button>
                )}
                </>
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

const root = document.getElementById("root");
if (!root) throw new Error("missing application root");
createRoot(root).render(<App />);
