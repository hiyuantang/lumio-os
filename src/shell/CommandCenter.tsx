// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { APP_ORDER, APPS } from '../apps/registry';
import { listServices, runServiceAction } from '../mock/system';
import { IconSearch } from './icons';
import { useShell } from './ShellContext';
import '../styles/command-center.css';

interface CmdAction {
  id: string;
  title: string;
  group: string;
  keywords: string;
  run: () => void;
}

function fuzzyRank(text: string, query: string): number | null {
  if (!query) return 0;
  const hay = text.toLowerCase();
  const needle = query.toLowerCase();
  const idx = hay.indexOf(needle);
  if (idx >= 0) return idx;
  let i = 0;
  for (const ch of hay) {
    if (ch === needle[i]) i += 1;
    if (i === needle.length) return 100 + hay.length;
  }
  return null;
}

export function CommandCenter() {
  const { state, actions, resolvedTheme, reducedMotion } = useShell();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const allActions = useMemo<CmdAction[]>(() => {
    const list: CmdAction[] = [];
    for (const appId of APP_ORDER) {
      const meta = APPS[appId];
      list.push({
        id: `open-${appId}`,
        title: `Open ${meta.title}`,
        group: 'Applications',
        keywords: `open launch ${meta.title} ${appId}`,
        run: () => actions.openApp(appId),
      });
    }
    for (const svc of listServices()) {
      list.push({
        id: `restart-${svc.name}`,
        title: `Restart ${svc.name}`,
        group: 'Services',
        keywords: `restart service ${svc.name} ${svc.description}`,
        run: () => {
          void runServiceAction(svc.name, 'restart').then((unit) =>
            actions.notify('Service restarted', `${unit.name} is ${unit.state}.`),
          );
        },
      });
    }
    list.push(
      {
        id: 'toggle-theme',
        title: resolvedTheme === 'dark' ? 'Switch to Light Theme' : 'Switch to Dark Theme',
        group: 'Shell',
        keywords: 'theme dark light appearance',
        run: actions.toggleTheme,
      },
      {
        id: 'toggle-motion',
        title: reducedMotion ? 'Allow Full Motion' : 'Reduce Motion',
        group: 'Shell',
        keywords: 'motion animation reduce accessibility',
        run: actions.toggleMotion,
      },
      {
        id: 'show-shortcuts',
        title: 'Show Keyboard Shortcuts',
        group: 'Shell',
        keywords: 'keyboard shortcuts help keys',
        run: () => actions.setShortcutsOpen(true),
      },
      {
        id: 'logout',
        title: 'Log Out',
        group: 'Shell',
        keywords: 'log out sign out logout',
        run: actions.logout,
      },
    );
    return list;
  }, [actions, resolvedTheme, reducedMotion]);

  const results = useMemo(() => {
    return allActions
      .map((action, order) => {
        const rank = fuzzyRank(`${action.title} ${action.keywords}`, query.trim());
        return rank === null ? null : { action, rank, order };
      })
      .filter((r): r is { action: CmdAction; rank: number; order: number } => r !== null)
      .sort((a, b) => a.rank - b.rank || a.order - b.order)
      .map((r) => r.action);
  }, [allActions, query]);

  useEffect(() => {
    if (state.paletteOpen) {
      setQuery('');
      setSelected(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [state.paletteOpen]);

  useEffect(() => {
    setSelected(0);
  }, [query]);

  const selectedSafe = Math.min(selected, Math.max(0, results.length - 1));

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${selectedSafe}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  }, [selectedSafe]);

  if (!state.paletteOpen) return null;

  function runAt(index: number) {
    const action = results[index];
    if (!action) return;
    actions.setPalette(false);
    action.run();
  }

  function onKeyDown(e: ReactKeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelected(Math.min(selectedSafe + 1, results.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelected(Math.max(selectedSafe - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      runAt(selectedSafe);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      actions.setPalette(false);
    }
  }

  return (
    <div className="palette-overlay" onPointerDown={() => actions.setPalette(false)}>
      <div
        className="palette"
        role="dialog"
        aria-modal="true"
        aria-label="Command Center"
        data-testid="command-center"
        onPointerDown={(e) => e.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        <div className="palette-input-row">
          <IconSearch size={16} />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Type a command or search…"
            aria-label="Search actions"
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-results"
          />
          <kbd>esc</kbd>
        </div>
        <div className="palette-results" id="palette-results" role="listbox" ref={listRef}>
          {results.length === 0 && <p className="palette-empty">No matching actions.</p>}
          {results.map((action, i) => (
            <button
              key={action.id}
              type="button"
              role="option"
              aria-selected={i === selectedSafe}
              data-index={i}
              className={`palette-item${i === selectedSafe ? ' selected' : ''}`}
              onMouseMove={() => {
                if (i !== selectedSafe) setSelected(i);
              }}
              onClick={() => runAt(i)}
            >
              <span>{action.title}</span>
              <span className="palette-group">{action.group}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
