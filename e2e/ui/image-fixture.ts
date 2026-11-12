// Images a spec pastes or drops on the composer.
import { deflateSync } from "node:zlib";
import type { Locator } from "@playwright/test";

// png is a size×size solid-colour PNG.
export function png(size: number, [r, g, b]: number[]): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (buf: Buffer) => {
    let c = 0xffffffff;
    for (const byte of buf) c = crcTable[(c ^ byte) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(body.length + 8);
    out.writeUInt32BE(data.length, 0);
    body.copy(out, 4);
    out.writeUInt32BE(crc(body), body.length + 4);
    return out;
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr.set([8, 2, 0, 0, 0], 8);
  const row = Buffer.concat([
    Buffer.from([0]),
    Buffer.from(Array(size).fill([r, g, b]).flat()),
  ]);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(Buffer.concat(Array(size).fill(row)))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

// give fires a paste or drop of one file on the composer.
export async function give(
  target: Locator,
  kind: "paste" | "drop",
  file: { name: string; type: string; bytes?: number[]; size?: number },
) {
  await target.evaluate(
    (el, { kind, file }) => {
      const data = new DataTransfer();
      const content = file.bytes
        ? new Uint8Array(file.bytes)
        : new Uint8Array(file.size ?? 0);
      data.items.add(new File([content], file.name, { type: file.type }));
      const init = { bubbles: true, cancelable: true };
      el.dispatchEvent(
        kind === "paste"
          ? new ClipboardEvent("paste", { ...init, clipboardData: data })
          : new DragEvent("drop", { ...init, dataTransfer: data }),
      );
    },
    { kind, file },
  );
}

export const red = {
  name: "red.png",
  type: "image/png",
  bytes: [...png(8, [200, 30, 30])],
};

export const blue = {
  name: "blue.png",
  type: "image/png",
  bytes: [...png(8, [30, 30, 200])],
};
