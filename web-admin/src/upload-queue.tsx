import { Check, LoaderCircle, RotateCcw, Upload, X } from "lucide-react";

export type UploadState =
  | "queued"
  | "hashing"
  | "uploading"
  | "committing"
  | "complete"
  | "failed"
  | "cancelled";

export type UploadTask = {
  id: string;
  file: File;
  targetPath: string;
  state: UploadState;
  progress: number;
  error?: string;
  uploadID?: string;
};

const stateLabel: Record<UploadState, string> = {
  queued: "等待上传",
  hashing: "正在校验",
  uploading: "正在上传",
  committing: "正在保存",
  complete: "上传完成",
  failed: "上传失败",
  cancelled: "已取消",
};

export function UploadQueue({
  tasks,
  formatSize,
  onRetry,
  onDismiss,
  onCancel,
  onClearCompleted,
}: {
  tasks: UploadTask[];
  formatSize: (size: number) => string;
  onRetry: (id: string) => void;
  onDismiss: (id: string) => void;
  onCancel: (id: string) => void;
  onClearCompleted: () => void;
}) {
  if (tasks.length === 0) return null;
  const terminal = tasks.filter(
    (task) => task.state === "complete" || task.state === "cancelled",
  ).length;
  return (
    <aside className="upload-queue" aria-label="上传队列">
      <header>
        <span>
          <Upload size={17} />
          上传任务
        </span>
        {terminal > 0 && (
          <button onClick={onClearCompleted}>清除已结束</button>
        )}
      </header>
      <div className="upload-task-list">
        {tasks.map((task) => (
          <div className={`upload-task ${task.state}`} key={task.id}>
            <div className="upload-task-icon">
              {task.state === "complete" ? (
                <Check size={17} />
              ) : task.state === "failed" || task.state === "cancelled" ? (
                <X size={17} />
              ) : (
                <LoaderCircle size={17} />
              )}
            </div>
            <div className="upload-task-detail">
              <strong title={task.targetPath}>{task.file.name}</strong>
              <span>
                {task.error || stateLabel[task.state]} · {formatSize(task.file.size)}
              </span>
              <div className="upload-progress" aria-label={`${task.progress}%`}>
                <span style={{ width: `${task.progress}%` }} />
              </div>
            </div>
            {task.state === "failed" ? (
              <button
                className="upload-task-action"
                title="重试"
                onClick={() => onRetry(task.id)}
              >
                <RotateCcw size={16} />
              </button>
            ) : task.state === "complete" || task.state === "cancelled" ? (
              <button
                className="upload-task-action"
                title="移除"
                onClick={() => onDismiss(task.id)}
              >
                <X size={16} />
              </button>
            ) : task.state !== "committing" ? (
              <button
                className="upload-task-action"
                title="取消上传"
                onClick={() => onCancel(task.id)}
              >
                <X size={16} />
              </button>
            ) : null}
          </div>
        ))}
      </div>
    </aside>
  );
}
