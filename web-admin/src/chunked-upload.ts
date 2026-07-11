import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";

export const uploadChunkSize = 4 * 1024 * 1024;

export type FileChunk = {
  index: number;
  blob: Blob;
  checksum: string;
};

export async function prepareFile(
  file: File,
  onProgress: (progress: number) => void,
) {
  const hasher = sha256.create();
  const chunks: FileChunk[] = [];
  const count = Math.max(1, Math.ceil(file.size / uploadChunkSize));
  for (let index = 0; index < count; index += 1) {
    const start = index * uploadChunkSize;
    const blob = file.slice(start, Math.min(start + uploadChunkSize, file.size));
    const bytes = new Uint8Array(await blob.arrayBuffer());
    hasher.update(bytes);
    chunks.push({ index, blob, checksum: bytesToHex(sha256(bytes)) });
    onProgress((index + 1) / count);
  }
  return { checksum: bytesToHex(hasher.digest()), chunks };
}

export function transferredProgress(
  completedBytes: number,
  currentChunkBytes: number,
  fileSize: number,
) {
  if (fileSize === 0) return 1;
  return Math.min(1, (completedBytes + currentChunkBytes) / fileSize);
}
