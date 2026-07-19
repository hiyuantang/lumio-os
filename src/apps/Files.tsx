// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { formatSize, homePath, listDir, type FsEntry } from '../mock/filesystem';
import { IconChevronRight, IconEye, IconFile, IconFolder } from '../shell/icons';
import '../styles/apps.css';
import '../styles/files.css';

export function Files() {
  const [path, setPath] = useState<string[]>(() => homePath());
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [quickLook, setQuickLook] = useState<FsEntry | null>(null);

  const entries = listDir(path);
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

  function activate(entry: FsEntry) {
    if (entry.kind === 'dir') navigateTo([...path, entry.name]);
    else setQuickLook(entry);
  }

  function onRowKey(e: ReactKeyboardEvent, entry: FsEntry) {
    if (e.key === 'Enter') {
      e.preventDefault();
      activate(entry);
    } else if (e.key === ' ') {
      e.preventDefault();
      setQuickLook(entry);
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
          <button type="button" className="files-crumb" onClick={() => navigateTo(homePath())}>
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
          onClick={() => selected && setQuickLook(selected)}
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
        {entries.length === 0 && <p className="files-empty">This folder is empty.</p>}
      </div>

      {quickLook && (
        <div className="quicklook-overlay" onPointerDown={() => setQuickLook(null)}>
          <div
            className="quicklook"
            role="dialog"
            aria-modal="true"
            aria-label={`Quick Look ${quickLook.name}`}
            data-testid="quick-look"
            onPointerDown={(e) => e.stopPropagation()}
          >
            <header className="quicklook-header">
              {quickLook.kind === 'dir' ? <IconFolder size={16} /> : <IconFile size={16} />}
              <strong>{quickLook.name}</strong>
              <button type="button" className="btn" onClick={() => setQuickLook(null)}>
                Close
              </button>
            </header>
            <dl className="quicklook-meta">
              <div>
                <dt>Kind</dt>
                <dd>{quickLook.kind === 'dir' ? 'Folder' : 'File'}</dd>
              </div>
              <div>
                <dt>Size</dt>
                <dd className="mono">
                  {quickLook.kind === 'dir' ? `${quickLook.children?.length ?? 0} items` : formatSize(quickLook.size)}
                </dd>
              </div>
              <div>
                <dt>Modified</dt>
                <dd>{quickLook.modified}</dd>
              </div>
              <div>
                <dt>Path</dt>
                <dd className="mono">/home/{[...path, quickLook.name].join('/')}</dd>
              </div>
            </dl>
            {quickLook.content ? (
              <pre className="quicklook-preview">{quickLook.content.slice(0, 800)}</pre>
            ) : (
              <p className="quicklook-none">
                {quickLook.kind === 'dir' ? 'Double-click to open this folder.' : 'No preview available for this file.'}
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
