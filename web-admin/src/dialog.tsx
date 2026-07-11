import { useState } from "react";
import type { FileNode } from "./api";

export type Modal =
  | { type: "folder" }
  | { type: "rename" | "delete"; item: FileNode };

export function Dialog({ modal, onClose, onConfirm }: {
  modal: Modal;
  onClose: () => void;
  onConfirm: (value: string) => void | Promise<void>;
}) {
  const isDelete = modal.type === "delete";
  const [value, setValue] = useState(modal.type === "folder" ? "" : modal.item.name);
  return (
    <div className="dialog-backdrop" role="presentation">
      <form className="dialog" onSubmit={(event) => { event.preventDefault(); void onConfirm(value); }}>
        <h2>{isDelete ? "删除项目？" : modal.type === "rename" ? "重命名" : "新建文件夹"}</h2>
        {isDelete ? <p>“{modal.item.name}” 将从云端文件中删除。</p> : (
          <label>名称<input autoFocus value={value} onChange={(event) => setValue(event.target.value)} required /></label>
        )}
        <div className="dialog-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button className={isDelete ? "danger-button" : "primary"}>{isDelete ? "删除" : "确认"}</button>
        </div>
      </form>
    </div>
  );
}
