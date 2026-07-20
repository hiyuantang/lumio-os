// SPDX-License-Identifier: AGPL-3.0-only
import { base64ToText, textToBase64 } from '../api/encoding';
import type { FileRead, PrivilegedFileWrite } from '../api/source';
import { ApiError } from '../api/transport';

let content = 'server {\n    listen 80 default_server;\n    server_name _;\n    root /var/www/html;\n}\n';
let revision = `sha256:${'1'.repeat(64)}`;

export async function readSystemFile(path: string): Promise<FileRead> {
  if (!path.startsWith('/etc/')) throw new ApiError('validation_failed', 'Path must be below /etc.');
  return {
    content,
    contentBase64: textToBase64(content),
    revision,
    truncated: false,
    sizeBytes: new TextEncoder().encode(content).length,
  };
}

export async function writePrivilegedFile(
  path: string,
  contentBase64: string,
  expectedRevision: string,
  restartUnit?: string,
): Promise<PrivilegedFileWrite> {
  if (!path.startsWith('/etc/')) throw new ApiError('validation_failed', 'Path must be below /etc.');
  if (expectedRevision !== revision) {
    throw new ApiError('stale_revision', 'The file changed on disk.', {
      expectedRevision,
      actualRevision: revision,
    });
  }
  content = base64ToText(contentBase64);
  revision = `sha256:${'2'.repeat(64)}`;
  return {
    revision,
    sizeBytes: new TextEncoder().encode(content).length,
    rollbackRef: `mock-${crypto.randomUUID()}`,
    validation: { kind: path.includes('nginx') ? 'nginx' : 'none', checked: path.includes('nginx') },
    restart: restartUnit ? { success: true } : null,
  };
}
