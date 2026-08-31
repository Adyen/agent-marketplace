import crypto from "node:crypto";
import fs from "node:fs";

export const sha256File = (file: string): string => {
  const hash = crypto.createHash("sha256");
  const descriptor = fs.openSync(file, "r");
  try {
    const buffer = Buffer.allocUnsafe(64 * 1024);
    for (;;) {
      const count = fs.readSync(descriptor, buffer, 0, buffer.length, null);
      if (count === 0) break;
      hash.update(buffer.subarray(0, count));
    }
  } finally {
    fs.closeSync(descriptor);
  }
  return hash.digest("hex");
};
