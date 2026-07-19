// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { describeError, getDataSource, type FsEntry } from '../api/source';
import { formatSize } from '../mock/filesystem';
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

export function Files() {
  const source = getDataSource();
  const [path, setPath] = useState<string[]>(() => source.homePath());
  const [entries, setEntries] = useState<FsEntry[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [quickLook, setQuickLook] = useState<QuickLookState | null>(null);

  useEffect(() => {
    let alive = true;
    source
      .listDir(path)
      .then((list) => {
        if (!alive) return;
        setEntries(list);
        setListError(null);
      })
      .catch((err) => {
        if (!alive) return;
        setEntries([]);
        setListError(describeError(err));
      });
    return () => {
      alive = false;
    };
  }, [source, path]);

  const selected = entries.find((e) => e.name === selectedName) ?? null;

  useEffect(() => {
    if (!quickLook) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setQuickLook(null);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [quickLook]);

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
          className="btn files-quicklook-btn"
          data-testid="quick-look-button"
          disabled={!selected}
          onClick={() => selected && openQuickLook(selected)}
        >
          <IconEye size={13} />
          Quick Look
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
    </div>
  );
}
