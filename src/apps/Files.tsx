// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useRef, useState, type ChangeEvent, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { base64ToBytes, bytesToBase64, textToBase64 } from '../api/encoding';
import { describeError, getDataSource, type FsEntry, type PrivilegedFileWrite } from '../api/source';
import { ApiError } from '../api/transport';
import { formatSize } from '../mock/filesystem';
import { useShell } from '../shell/ShellContext';
import { IconChevronRight, IconEye, IconFile, IconFolder } from '../shell/icons';
import '../styles/apps.css';
import '../styles/files.css';

interface QuickLookState {
  entry: FsEntry;
  content: string | null;
  revision: string | null;
  loading: boolean;
  error: string | null;
}

interface EditorState {
  path: string[];
  name: string;
  content: string;
  savedRevision: string | null;
  saving: boolean;
  error: string | null;
  conflict: { expected: string; actual: string } | null;
}

interface SystemEditorState {
  path: string;
  original: string | null;
  content: string;
  revision: string | null;
  restartUnit: string;
  loading: boolean;
  saving: boolean;
  error: string | null;
  result: PrivilegedFileWrite | null;
}

const MAX_UPLOAD_BYTES = 8 * 1024 * 1024;

export function Files() {
  const source = getDataSource();
  const { actions } = useShell();
  const [path, setPath] = useState<string[]>(() => source.homePath());
  const [entries, setEntries] = useState<FsEntry[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [quickLook, setQuickLook] = useState<QuickLookState | null>(null);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<FsEntry | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [systemEditor, setSystemEditor] = useState<SystemEditorState | null>(null);
  const uploadInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let alive = true;
    (async () => {
      await source.getIdentity().catch(() => null);
      if (!alive) return;
      const home = source.homePath();
      if (path.length === 1 && path[0] !== home[0]) {
        setPath(home);
        return;
      }
      try {
        const list = await source.listDir(path);
        if (!alive) return;
        setEntries(list);
        setListError(null);
      } catch (err) {
        if (!alive) return;
        setEntries([]);
        setListError(describeError(err));
      }
    })();
    return () => {
      alive = false;
    };
  }, [source, path, refreshNonce]);

  const selected = entries.find((e) => e.name === selectedName) ?? null;

  useEffect(() => {
    if (!quickLook) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setQuickLook(null);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [quickLook]);

  function refresh() {
    setRefreshNonce((n) => n + 1);
  }

  function navigateTo(nextPath: string[]) {
    setPath(nextPath);
    setSelectedName(null);
  }

  function openQuickLook(entry: FsEntry) {
    setQuickLook({ entry, content: null, revision: null, loading: entry.kind === 'file', error: null });
    if (entry.kind !== 'file') return;
    source
      .readFile([...path, entry.name])
      .then((read) => {
        setQuickLook((prev) =>
          prev?.entry.name === entry.name
            ? { ...prev, content: read.content, revision: read.revision, loading: false }
            : prev,
        );
      })
      .catch((err) => {
        setQuickLook((prev) =>
          prev?.entry.name === entry.name ? { ...prev, loading: false, error: describeError(err) } : prev,
        );
      });
  }

  function activate(entry: FsEntry) {
    if (entry.kind === 'dir') navigateTo([...path, entry.name]);
    else openQuickLook(entry);
  }

  function onRowKey(e: ReactKeyboardEvent, entry: FsEntry) {
    if (e.key === 'Enter') {
      e.preventDefault();
      activate(entry);
    } else if (e.key === ' ') {
      e.preventDefault();
      openQuickLook(entry);
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const idx = entries.findIndex((en) => en.name === entry.name);
      const next = entries[e.key === 'ArrowDown' ? idx + 1 : idx - 1];
      if (next) {
        setSelectedName(next.name);
        const row = document.querySelector<HTMLElement>(`[data-file-row="${CSS.escape(next.name)}"]`);
        row?.focus();
      }
    }
  }

  function openEditor(look: QuickLookState) {
    setQuickLook(null);
    setEditor({
      path: [...path, look.entry.name],
      name: look.entry.name,
      content: look.content ?? '',
      savedRevision: look.revision,
      saving: false,
      error: null,
      conflict: null,
    });
  }

  async function saveEditor() {
    const current = editor;
    if (!current || current.saving) return;
    setEditor({ ...current, saving: true, error: null });
    try {
      await source.writeFile(current.path, textToBase64(current.content), current.savedRevision);
      setEditor(null);
      refresh();
    } catch (err) {
      setEditor((prev) => {
        if (!prev) return prev;
        if (err instanceof ApiError && err.code === 'stale_revision') {
          return {
            ...prev,
            saving: false,
            conflict: {
              expected: String(err.details.expectedRevision ?? prev.savedRevision ?? ''),
              actual: String(err.details.actualRevision ?? ''),
            },
          };
        }
        return { ...prev, saving: false, error: describeError(err) };
      });
    }
  }

  async function reloadEditor() {
    const current = editor;
    if (!current) return;
    try {
      const read = await source.readFile(current.path);
      setEditor((prev) =>
        prev ? { ...prev, content: read.content ?? '', savedRevision: read.revision, conflict: null, error: null } : prev,
      );
    } catch (err) {
      setEditor((prev) => (prev ? { ...prev, error: describeError(err) } : prev));
    }
  }

  async function saveEditorAsCopy() {
    const current = editor;
    if (!current) return;
    try {
      await source.writeFile([...current.path.slice(0, -1), `${current.name}.copy`], textToBase64(current.content), null);
      setEditor(null);
      refresh();
      actions.notify('Saved as copy', `${current.name}.copy`);
    } catch (err) {
      setEditor((prev) => (prev ? { ...prev, error: describeError(err) } : prev));
    }
  }

  async function confirmDelete() {
    const target = deleteTarget;
    if (!target || deleting) return;
    setDeleting(true);
    try {
      await source.deleteFile([...path, target.name]);
      setDeleteTarget(null);
      setQuickLook(null);
      setSelectedName(null);
      refresh();
    } catch (err) {
      setDeleteTarget(null);
      actions.notify('Could not move to Trash', describeError(err));
    } finally {
      setDeleting(false);
    }
  }

  async function download(entry: FsEntry) {
    try {
      const read = await source.readFile([...path, entry.name]);
      if (!read.contentBase64) {
        actions.notify('Download unavailable', `${entry.name} cannot be downloaded yet.`);
        return;
      }
      const bytes = base64ToBytes(read.contentBase64);
      const url = URL.createObjectURL(new Blob([bytes.buffer as ArrayBuffer], { type: 'application/octet-stream' }));
      const link = document.createElement('a');
      link.href = url;
      link.download = entry.name;
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      actions.notify('Download failed', describeError(err));
    }
  }

  async function upload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    if (file.size > MAX_UPLOAD_BYTES) {
      actions.notify('Upload too large', 'Files up to 8 MiB can be uploaded.');
      return;
    }
    setUploading(true);
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      await source.writeFile([...path, file.name], bytesToBase64(bytes), null);
      refresh();
    } catch (err) {
      actions.notify('Upload failed', describeError(err));
    } finally {
      setUploading(false);
    }
  }

  function openSystemEditor() {
    setSystemEditor({
      path: '/etc/nginx/nginx.conf',
      original: null,
      content: '',
      revision: null,
      restartUnit: 'nginx.service',
      loading: false,
      saving: false,
      error: null,
      result: null,
    });
  }

  async function loadSystemFile() {
    const current = systemEditor;
    if (!current || current.loading) return;
    setSystemEditor({ ...current, loading: true, error: null, result: null });
    try {
      const read = await source.readSystemFile(current.path);
      if (read.truncated || read.content === null || !read.revision) {
        throw new Error('This protected file cannot be edited as text.');
      }
      const content = read.content;
      setSystemEditor((prev) => prev ? {
        ...prev,
        original: content,
        content,
        revision: read.revision,
        restartUnit: suggestedService(prev.path),
        loading: false,
        error: null,
      } : prev);
    } catch (err) {
      setSystemEditor((prev) => prev ? { ...prev, loading: false, error: describeError(err) } : prev);
    }
  }

  async function saveSystemFile() {
    const current = systemEditor;
    if (!current || current.saving || !current.revision || current.original === current.content) return;
    setSystemEditor({ ...current, saving: true, error: null, result: null });
    try {
      const result = await source.writePrivilegedFile(
        current.path,
        textToBase64(current.content),
        current.revision,
        current.restartUnit.trim() || undefined,
      );
      setSystemEditor((prev) => prev ? {
        ...prev,
        original: prev.content,
        revision: result.revision,
        saving: false,
        result,
      } : prev);
      actions.notify('Protected file saved', result.restart?.success === false ? 'The file was saved, but its service did not restart.' : 'A rollback copy was kept.');
    } catch (err) {
      setSystemEditor((prev) => prev ? { ...prev, saving: false, error: describeError(err) } : prev);
    }
  }

  return (
    <div className="app files" data-testid="app-files">
      <div className="app-toolbar">
        <nav className="files-breadcrumbs" aria-label="Path">
          <button type="button" className="files-crumb" onClick={() => navigateTo(source.homePath())}>
            Home
          </button>
          {path.slice(1).map((segment, i) => (
            <span key={segment} className="files-crumb-group">
              <IconChevronRight size={11} />
              <button
                type="button"
                className="files-crumb"
                onClick={() => navigateTo(path.slice(0, i + 2))}
              >
                {segment}
              </button>
            </span>
          ))}
        </nav>
        <button
          type="button"
          className="btn"
          data-testid="upload-button"
          disabled={uploading}
          onClick={() => uploadInputRef.current?.click()}
        >
          {uploading ? 'Uploading…' : 'Upload'}
        </button>
        <input ref={uploadInputRef} type="file" hidden data-testid="upload-input" onChange={(e) => void upload(e)} />
        <button
          type="button"
          className="btn files-quicklook-btn"
          data-testid="quick-look-button"
          disabled={!selected}
          onClick={() => selected && openQuickLook(selected)}
        >
          <IconEye size={13} />
          Quick Look
        </button>
        <button type="button" className="btn" data-testid="system-file-button" onClick={openSystemEditor}>
          Edit protected file
        </button>
      </div>

      <div className="files-head" role="row" aria-hidden="true">
        <span>Name</span>
        <span>Size</span>
        <span>Modified</span>
      </div>
      <div className="files-list" role="listbox" aria-label="Files" aria-activedescendant={selectedName ? `file-${selectedName}` : undefined}>
        {entries.map((entry) => (
          <div
            key={entry.name}
            id={`file-${entry.name}`}
            role="option"
            aria-selected={selectedName === entry.name}
            tabIndex={0}
            data-file-row={entry.name}
            data-testid={`file-row-${entry.name}`}
            className={`file-row${selectedName === entry.name ? ' selected' : ''}`}
            onClick={() => setSelectedName(entry.name)}
            onDoubleClick={() => activate(entry)}
            onKeyDown={(e) => onRowKey(e, entry)}
          >
            <span className="file-name">
              {entry.kind === 'dir' ? <IconFolder size={15} /> : <IconFile size={15} />}
              {entry.name}
            </span>
            <span className="file-size mono">{entry.kind === 'dir' ? '—' : formatSize(entry.size)}</span>
            <span className="file-modified">{entry.modified}</span>
          </div>
        ))}
        {listError && (
          <p className="files-empty">
            {listError}{' '}
            <button type="button" className="btn" data-testid="files-retry" onClick={() => setPath((p) => [...p])}>
              Retry
            </button>
          </p>
        )}
        {!listError && entries.length === 0 && <p className="files-empty">This folder is empty.</p>}
      </div>

      {quickLook && (
        <div className="quicklook-overlay" onPointerDown={() => setQuickLook(null)}>
          <div
            className="quicklook"
            role="dialog"
            aria-modal="true"
            aria-label={`Quick Look ${quickLook.entry.name}`}
            data-testid="quick-look"
            onPointerDown={(e) => e.stopPropagation()}
          >
            <header className="quicklook-header">
              {quickLook.entry.kind === 'dir' ? <IconFolder size={16} /> : <IconFile size={16} />}
              <strong>{quickLook.entry.name}</strong>
              <button type="button" className="btn" onClick={() => setQuickLook(null)}>
                Close
              </button>
            </header>
            <div className="quicklook-actions">
              {quickLook.entry.kind === 'file' && quickLook.content !== null && (
                <button type="button" className="btn" data-testid="quicklook-edit" onClick={() => openEditor(quickLook)}>
                  Edit
                </button>
              )}
              {quickLook.entry.kind === 'file' && (
                <button
                  type="button"
                  className="btn"
                  data-testid="quicklook-download"
                  disabled={quickLook.loading || quickLook.error !== null}
                  onClick={() => void download(quickLook.entry)}
                >
                  Download
                </button>
              )}
              <button type="button" className="btn" data-testid="quicklook-delete" onClick={() => setDeleteTarget(quickLook.entry)}>
                Move to Trash
              </button>
            </div>
            <dl className="quicklook-meta">
              <div>
                <dt>Kind</dt>
                <dd>{quickLook.entry.kind === 'dir' ? 'Folder' : 'File'}</dd>
              </div>
              <div>
                <dt>Size</dt>
                <dd className="mono">
                  {quickLook.entry.kind === 'dir'
                    ? quickLook.entry.children
                      ? `${quickLook.entry.children.length} items`
                      : formatSize(quickLook.entry.size)
                    : formatSize(quickLook.entry.size)}
                </dd>
              </div>
              <div>
                <dt>Modified</dt>
                <dd>{quickLook.entry.modified}</dd>
              </div>
              <div>
                <dt>Path</dt>
                <dd className="mono">/home/{[...path, quickLook.entry.name].join('/')}</dd>
              </div>
              {quickLook.revision && (
                <div>
                  <dt>Revision</dt>
                  <dd className="mono" data-testid="quick-look-revision">
                    {quickLook.revision}
                  </dd>
                </div>
              )}
            </dl>
            {quickLook.loading ? (
              <p className="quicklook-none">Loading…</p>
            ) : quickLook.error ? (
              <p className="quicklook-none">{quickLook.error}</p>
            ) : quickLook.content ? (
              <pre className="quicklook-preview">{quickLook.content.slice(0, 800)}</pre>
            ) : (
              <p className="quicklook-none">
                {quickLook.entry.kind === 'dir'
                  ? 'Double-click to open this folder.'
                  : 'No preview available for this file.'}
              </p>
            )}
          </div>
        </div>
      )}

      {editor && (
        <div className="quicklook-overlay">
          <div
            className="file-editor"
            role="dialog"
            aria-modal="true"
            aria-label={`Edit ${editor.name}`}
            data-testid="file-editor"
          >
            <header className="quicklook-header">
              <IconFile size={16} />
              <strong>{editor.name}</strong>
              <span className="file-editor-actions">
                <button type="button" className="btn" data-testid="editor-close" onClick={() => setEditor(null)}>
                  Close
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  data-testid="editor-save"
                  disabled={editor.saving}
                  onClick={() => void saveEditor()}
                >
                  {editor.saving ? 'Saving…' : 'Save'}
                </button>
              </span>
            </header>
            {editor.conflict && (
              <div className="file-editor-conflict" data-testid="editor-conflict" role="alert">
                <p>This file changed on disk since you opened it.</p>
                <div className="file-editor-conflict-actions">
                  <button type="button" className="btn" data-testid="editor-reload" onClick={() => void reloadEditor()}>
                    Reload latest
                  </button>
                  <button type="button" className="btn" data-testid="editor-save-copy" onClick={() => void saveEditorAsCopy()}>
                    Save as copy
                  </button>
                  <button
                    type="button"
                    className="btn"
                    data-testid="editor-conflict-cancel"
                    onClick={() => setEditor({ ...editor, conflict: null })}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
            {editor.error && (
              <p className="file-editor-error" role="alert">
                {editor.error}
              </p>
            )}
            <textarea
              className="file-editor-input"
              data-testid="editor-input"
              value={editor.content}
              onChange={(e) => setEditor({ ...editor, content: e.target.value })}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === 's') {
                  e.preventDefault();
                  void saveEditor();
                }
              }}
              aria-label={`Contents of ${editor.name}`}
              spellCheck={false}
              autoFocus
            />
            <footer className="file-editor-footer">
              <span className="mono">{new TextEncoder().encode(editor.content).length} bytes</span>
              {editor.savedRevision && (
                <span className="mono" data-testid="editor-revision">
                  {editor.savedRevision}
                </span>
              )}
            </footer>
          </div>
        </div>
      )}

      {deleteTarget && (
        <div className="quicklook-overlay" onPointerDown={() => !deleting && setDeleteTarget(null)}>
          <div
            className="file-confirm"
            role="alertdialog"
            aria-modal="true"
            aria-label={`Move ${deleteTarget.name} to Trash`}
            data-testid="delete-confirm"
            onPointerDown={(e) => e.stopPropagation()}
          >
            <p>
              Move “{deleteTarget.name}” to Trash?
            </p>
            <div className="file-confirm-actions">
              <button
                type="button"
                className="btn"
                data-testid="delete-cancel-button"
                disabled={deleting}
                onClick={() => setDeleteTarget(null)}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-danger"
                data-testid="delete-confirm-button"
                disabled={deleting}
                onClick={() => void confirmDelete()}
              >
                {deleting ? 'Moving…' : 'Move to Trash'}
              </button>
            </div>
          </div>
        </div>
      )}

      {systemEditor && (
        <div className="quicklook-overlay">
          <div className="system-file-editor" role="dialog" aria-modal="true" aria-label="Edit protected file" data-testid="system-file-editor">
            <header className="quicklook-header">
              <IconFile size={16} />
              <strong>Protected file</strong>
              <button type="button" className="btn" onClick={() => setSystemEditor(null)}>Close</button>
            </header>
            {systemEditor.original === null ? (
              <div className="system-file-open">
                <label>
                  Path
                  <input
                    className="system-file-path mono"
                    data-testid="system-file-path"
                    value={systemEditor.path}
                    onChange={(event) => setSystemEditor({ ...systemEditor, path: event.target.value, error: null })}
                    spellCheck={false}
                  />
                </label>
                <p>Only existing, regular files below /etc can be edited. Symlinks are rejected.</p>
                {systemEditor.error && <p className="file-editor-error" role="alert">{systemEditor.error}</p>}
                <button type="button" className="btn btn-primary" data-testid="system-file-open" disabled={systemEditor.loading} onClick={() => void loadSystemFile()}>
                  {systemEditor.loading ? 'Opening…' : 'Open file'}
                </button>
              </div>
            ) : (
              <>
                <div className="system-file-controls">
                  <span className="mono">{systemEditor.path}</span>
                  <label>
                    Restart after save
                    <input
                      className="system-file-service mono"
                      value={systemEditor.restartUnit}
                      onChange={(event) => setSystemEditor({ ...systemEditor, restartUnit: event.target.value })}
                      placeholder="Optional service unit"
                      aria-label="Restart service after save"
                    />
                  </label>
                </div>
                {systemEditor.error && <p className="file-editor-error" role="alert">{systemEditor.error}</p>}
                {systemEditor.result && (
                  <p className="system-file-success" role="status" data-testid="system-file-result">
                    Saved. {systemEditor.result.validation.checked ? `${systemEditor.result.validation.kind} validation passed. ` : ''}Rollback copy kept.
                    {systemEditor.result.restart?.success === false ? ' Service restart failed.' : ''}
                  </p>
                )}
                <div className="system-file-panes">
                  <label>
                    Proposed content
                    <textarea
                      className="file-editor-input"
                      data-testid="system-file-input"
                      value={systemEditor.content}
                      onChange={(event) => setSystemEditor({ ...systemEditor, content: event.target.value, result: null })}
                      spellCheck={false}
                    />
                  </label>
                  <section aria-label="Proposed diff" data-testid="system-file-diff">
                    <span>Proposed diff</span>
                    <pre className="system-file-diff">{formatDiff(systemEditor.original, systemEditor.content)}</pre>
                  </section>
                </div>
                <footer className="system-file-footer">
                  <p>Known formats are validated before the atomic write. A rollback copy is created first.</p>
                  <button
                    type="button"
                    className="btn btn-primary"
                    data-testid="system-file-save"
                    disabled={systemEditor.saving || systemEditor.original === systemEditor.content}
                    onClick={() => void saveSystemFile()}
                  >
                    {systemEditor.saving ? 'Validating and saving…' : 'Validate and save'}
                  </button>
                </footer>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function suggestedService(path: string): string {
  if (path.includes('/nginx/')) return 'nginx.service';
  if (path.includes('/ssh/')) return 'ssh.service';
  if (path.includes('/systemd/system/')) return '';
  return '';
}

function formatDiff(original: string, next: string): string {
  if (original === next) return 'No changes.';
  const before = original.split('\n');
  const after = next.split('\n');
  let start = 0;
  while (start < before.length && start < after.length && before[start] === after[start]) start++;
  let beforeEnd = before.length - 1;
  let afterEnd = after.length - 1;
  while (beforeEnd >= start && afterEnd >= start && before[beforeEnd] === after[afterEnd]) {
    beforeEnd--;
    afterEnd--;
  }
  const removed = before.slice(start, beforeEnd + 1).slice(0, 120).map((line) => `- ${line}`);
  const added = after.slice(start, afterEnd + 1).slice(0, 120).map((line) => `+ ${line}`);
  const header = `@@ line ${start + 1} @@`;
  const clipped = beforeEnd - start + 1 > 120 || afterEnd - start + 1 > 120 ? ['… diff truncated …'] : [];
  return [header, ...removed, ...added, ...clipped].join('\n');
}
