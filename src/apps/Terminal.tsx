// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useRef, useState } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal as XTerm, type ITheme } from '@xterm/xterm';
import { describeError, getDataSource } from '../api/source';
import { useShell } from '../shell/ShellContext';
import '@xterm/xterm/css/xterm.css';
import '../styles/terminal.css';

interface TabState {
  id: number;
  epoch: number;
  exitCode: number | null;
  error: string | null;
}

let nextTabId = 1;

function readTheme(): ITheme {
  const styles = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string) => styles.getPropertyValue(name).trim() || fallback;
  return {
    background: v('--surface-sunken', '#161513'),
    foreground: v('--text', '#f0ede7'),
    cursor: v('--accent', '#2fbfae'),
    selectionBackground: v('--accent-soft', 'rgba(47, 191, 174, 0.16)'),
  };
}

export function Terminal() {
  const { capabilities } = getDataSource();
  if (!capabilities.canTerminal) {
    return (
      <div className="app terminal" data-testid="app-terminal">
        <div className="terminal-placeholder">
          <p className="terminal-placeholder-title">Terminal</p>
          <p>Terminal is not available from this server.</p>
        </div>
      </div>
    );
  }
  return <TerminalTabs />;
}

function TerminalTabs() {
  const { state, actions } = useShell();
  const user = state.user ?? 'user';
  const [tabs, setTabs] = useState<TabState[]>(() => [{ id: nextTabId++, epoch: 0, exitCode: null, error: null }]);
  const [activeId, setActiveId] = useState<number | null>(null);
  const effectiveActive = tabs.some((tab) => tab.id === activeId) ? activeId : (tabs[0]?.id ?? null);

  function updateTab(id: number, patch: Partial<TabState>) {
    setTabs((prev) => prev.map((tab) => (tab.id === id ? { ...tab, ...patch } : tab)));
  }

  function addTab() {
    const id = nextTabId++;
    setTabs((prev) => [...prev, { id, epoch: 0, exitCode: null, error: null }]);
    setActiveId(id);
  }

  function closeTab(id: number) {
    if (tabs.length === 1 && tabs[0].id === id) {
      actions.closeApp('terminal');
      return;
    }
    setTabs((prev) => prev.filter((tab) => tab.id !== id));
  }

  return (
    <div className="app terminal" data-testid="app-terminal">
      <div className="terminal-tabs" role="tablist" aria-label="Terminal tabs">
        {tabs.map((tab, index) => (
          <div
            key={tab.id}
            className={`terminal-tab${tab.id === effectiveActive ? ' active' : ''}`}
            role="tab"
            aria-selected={tab.id === effectiveActive}
            data-testid={`terminal-tab-${tab.id}`}
          >
            <button type="button" className="terminal-tab-label" onClick={() => setActiveId(tab.id)}>
              {tab.exitCode !== null ? 'exited' : 'shell'} {index + 1}
            </button>
            <button
              type="button"
              className="terminal-tab-close"
              aria-label={`Close tab ${index + 1}`}
              data-testid={`terminal-close-tab-${tab.id}`}
              onClick={() => closeTab(tab.id)}
            >
              ×
            </button>
          </div>
        ))}
        <button type="button" className="terminal-tab-new" data-testid="terminal-new-tab" aria-label="New tab" onClick={addTab}>
          +
        </button>
      </div>
      <div className="terminal-panes">
        {tabs.map((tab) => (
          <TerminalPane
            key={`${tab.id}:${tab.epoch}`}
            tab={tab}
            user={user}
            visible={tab.id === effectiveActive}
            onExit={(code) => updateTab(tab.id, { exitCode: code })}
            onError={(message) => updateTab(tab.id, { error: message })}
            onRestart={() => updateTab(tab.id, { epoch: tab.epoch + 1, exitCode: null, error: null })}
          />
        ))}
      </div>
    </div>
  );
}

interface PaneProps {
  tab: TabState;
  user: string;
  visible: boolean;
  onExit: (code: number) => void;
  onError: (message: string) => void;
  onRestart: () => void;
}

function TerminalPane({ tab, user, visible, onExit, onError, onRestart }: PaneProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const callbacksRef = useRef({ onExit, onError });
  callbacksRef.current = { onExit, onError };
  const { resolvedTheme } = useShell();

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const term = new XTerm({
      fontFamily: 'var(--font-mono)',
      fontSize: 12.5,
      cursorBlink: true,
      theme: readTheme(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    termRef.current = term;
    const safeFit = () => {
      if (host.clientWidth === 0 || host.clientHeight === 0) return;
      fit.fit();
    };
    safeFit();
    term.focus();

    const session = getDataSource().openTerminal(
      { cols: term.cols, rows: term.rows, user },
      {
        onData: (data) => term.write(data),
        onExit: (code) => callbacksRef.current.onExit(code),
        onError: (err) => callbacksRef.current.onError(describeError(err)),
        onReset: () => term.reset(),
      },
    );
    const inputSub = term.onData((data) => session.write(data));
    const resizeSub = term.onResize(({ cols, rows }) => session.resize(cols, rows));
    const observer = new ResizeObserver(safeFit);
    observer.observe(host);
    return () => {
      observer.disconnect();
      inputSub.dispose();
      resizeSub.dispose();
      session.close();
      term.dispose();
      termRef.current = null;
    };
  }, [user]);

  useEffect(() => {
    const term = termRef.current;
    if (term) term.options.theme = readTheme();
  }, [resolvedTheme]);

  useEffect(() => {
    const textarea = hostRef.current?.querySelector('textarea');
    if (textarea) {
      if (visible) textarea.setAttribute('data-testid', 'terminal-input');
      else textarea.removeAttribute('data-testid');
    }
    if (visible) termRef.current?.focus();
  }, [visible]);

  return (
    <div className={`terminal-pane${visible ? '' : ' hidden'}`}>
      <div className="terminal-host" ref={hostRef} />
      {tab.error && (
        <div className="terminal-error" data-testid="terminal-error" role="alert">
          {tab.error}
        </div>
      )}
      {tab.exitCode !== null && (
        <div className="terminal-exited" data-testid="terminal-exited">
          <p>Process exited{tab.exitCode !== 0 ? ` (code ${tab.exitCode})` : ''}.</p>
          <button type="button" className="btn" data-testid="terminal-restart" onClick={onRestart}>
            Restart
          </button>
        </div>
      )}
    </div>
  );
}
