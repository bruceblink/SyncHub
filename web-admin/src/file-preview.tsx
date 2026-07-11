import { useEffect, useState } from "react";
import { Download, Eye, FileWarning, LoaderCircle, X } from "lucide-react";

type PreviewFile = { id: string; name: string; size: number };
type PreviewState = { kind: "text" | "image" | "pdf"; content: string };

const textExtensions = new Set([
  "txt",
  "md",
  "json",
  "yaml",
  "yml",
  "toml",
  "ini",
  "csv",
  "log",
  "xml",
  "css",
  "ts",
  "tsx",
  "js",
  "jsx",
  "go",
  "rs",
  "py",
  "java",
  "kt",
  "sh",
  "ps1",
]);
const imageTypes: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  bmp: "image/bmp",
};
const textLimit = 1024 * 1024;
const mediaLimit = 10 * 1024 * 1024;

function extension(name: string) {
  return name.includes(".") ? name.split(".").pop()!.toLowerCase() : "";
}

export function canPreview(file: PreviewFile) {
  const ext = extension(file.name);
  if (textExtensions.has(ext)) return file.size <= textLimit;
  if (ext === "pdf" || imageTypes[ext]) return file.size <= mediaLimit;
  return false;
}

export function FilePreview({
  file,
  token,
  onClose,
  onDownload,
  onError,
}: {
  file: PreviewFile;
  token: string;
  onClose: () => void;
  onDownload: () => Promise<void>;
  onError: (message: string) => void;
}) {
  const [preview, setPreview] = useState<PreviewState | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    let objectURL = "";
    async function load() {
      try {
        const ext = extension(file.name);
        const response = await fetch(`/api/v1/files/${file.id}/content`, {
          headers: { Authorization: `Bearer ${token}` },
          signal: controller.signal,
        });
        if (!response.ok) throw new Error("读取预览失败");
        const data = await response.blob();
        if (textExtensions.has(ext)) {
          setPreview({ kind: "text", content: await data.text() });
        } else {
          const mime = ext === "pdf" ? "application/pdf" : imageTypes[ext];
          objectURL = URL.createObjectURL(new Blob([data], { type: mime }));
          setPreview({
            kind: ext === "pdf" ? "pdf" : "image",
            content: objectURL,
          });
        }
      } catch (error) {
        if (!controller.signal.aborted)
          onError(error instanceof Error ? error.message : "读取预览失败");
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [file.id, file.name, token]);

  return (
    <div
      className="preview-backdrop"
      role="presentation"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        className="preview-panel"
        role="dialog"
        aria-modal="true"
        aria-label={`${file.name} 的预览`}
      >
        <header>
          <div>
            <p className="eyebrow">文件预览</p>
            <h2>{file.name}</h2>
          </div>
          <div className="preview-actions">
            <button
              className="icon-button"
              title="下载文件"
              onClick={() => void onDownload()}
            >
              <Download size={18} />
            </button>
            <button className="icon-button" title="关闭预览" onClick={onClose}>
              <X size={19} />
            </button>
          </div>
        </header>
        <div className="preview-content">
          {loading ? (
            <div className="preview-state">
              <LoaderCircle className="spin" size={30} />
              <span>正在加载预览...</span>
            </div>
          ) : !preview ? (
            <div className="preview-state">
              <FileWarning size={32} />
              <strong>无法显示预览</strong>
            </div>
          ) : preview.kind === "text" ? (
            <pre>{preview.content}</pre>
          ) : preview.kind === "image" ? (
            <img src={preview.content} alt={file.name} />
        ) : (
          <object data={preview.content} type="application/pdf" aria-label={file.name} />
        )}
        </div>
        <footer>
          <Eye size={15} />
          <span>预览内容仅在当前浏览器会话中加载</span>
        </footer>
      </section>
    </div>
  );
}
