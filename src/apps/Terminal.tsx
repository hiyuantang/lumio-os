// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { getDataSource } from '../api/source';
import { homePath, listDir } from '../mock/filesystem';
import { uptimeSeconds } from '../mock/system';
import { useShell } from '../shell/ShellContext';
import '../styles/terminal.css';

interface TermLine {
  id: number;
  kind: 'input' | 'output' | 'error';
  text: string;
}

const PROMPT = 'user@atlas:~$';

let lineId = 1;

function evaluate(raw: string, ctx: { user: string }): { out: string[]; clear?: boolean; exit?: boolean } {
  const trimmed = raw.trim();
  if (!trimmed) return { out: [] };
  const [cmd, ...args] = trimmed.split(/\s+/);
  switch (cmd) {
    case 'help':
      return {
        out: [
          'Available commands:',
          '  help              show this help',
          '  ls                list files in the home directory',
          '  pwd               print working directory',
          '  whoami            print the current user',
          '  uname -a          print system information',
          '  uptime            show how long the system has been up',
          '  echo [text]       print text',
          '  clear             clear the screen',
          '  exit              close this terminal',
        ],
      };
    case 'ls':
      return { out: [listDir(homePath()).map((e) => e.name).join('  ')] };
    case 'pwd':
      return { out: ['/home/user'] };
    case 'whoami':
      return { out: [ctx.user] };
    case 'uname': {
      const all = args.includes('-a');
      return {
        out: [
          all
            ? 'Linux atlas 6.8.0-45-generic #45-Ubuntu SMP x86_64 x86_64 GNU/Linux'
            : 'Linux',
        ],
      };
    }
    case 'uptime': {
      const s = uptimeSeconds();
      const days = Math.floor(s / 86400);
      const hours = Math.floor((s % 86400) / 3600);
      const minutes = Math.floor((s % 3600) / 60);
      const now = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      return {
        out: [
          ` ${now} up ${days} days, ${hours}:${String(minutes).padStart(2, '0')}, 1 user, load average: 0.42, 0.31, 0.28`,
        ],
      };
    }
    case 'echo':
      return { out: [args.join(' ')] };
    case 'clear':
      return { out: [], clear: true };
    case 'exit':
      return { out: [], exit: true };
    default:
      return { out: [`lumio-sh: command not found: ${cmd}`] };
  }
}

export function Terminal() {
  const { capabilities } = getDataSource();
  if (!capabilities.canTerminal) {
    return (
      <div className="app terminal" data-testid="app-terminal">
        <div className="terminal-placeholder">
          <p className="terminal-placeholder-title">Terminal</p>
          <p>A live terminal session arrives in a later phase of the server.</p>
        </div>
      </div>
    );
  }
  return <MockTerminal />;
}

function MockTerminal() {
  const { state, actions } = useShell();
  const user = state.user ?? 'user';
  const [lines, setLines] = useState<TermLine[]>([
    { id: lineId++, kind: 'output', text: "Lumio OS mock shell — no commands leave this window. Type 'help'." },
  ]);
  const [input, setInput] = useState('');
  const [history, setHistory] = useState<string[]>([]);
  const [histIdx, setHistIdx] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  function submit() {
    const result = evaluate(input, { user });
    if (result.exit) {
      actions.closeApp('terminal');
      return;
    }
    if (result.clear) {
      setLines([]);
    } else {
      setLines((prev) => [
        ...prev,
        { id: lineId++, kind: 'input', text: `${PROMPT} ${input}` },
        ...result.out.map((text) => ({ id: lineId++, kind: 'output' as const, text })),
      ]);
    }
    if (input.trim()) {
      setHistory((prev) => [...prev, input]);
    }
    setInput('');
    setHistIdx(-1);
  }

  function onKeyDown(e: ReactKeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault();
      submit();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (history.length === 0) return;
      const next = histIdx === -1 ? history.length - 1 : Math.max(0, histIdx - 1);
      setHistIdx(next);
      setInput(history[next]);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (histIdx === -1) return;
      const next = histIdx + 1;
      if (next >= history.length) {
        setHistIdx(-1);
        setInput('');
      } else {
        setHistIdx(next);
        setInput(history[next]);
      }
    }
  }

  return (
    <div className="app terminal" data-testid="app-terminal" onClick={() => inputRef.current?.focus()}>
      <div className="terminal-scroll" ref={scrollRef}>
        {lines.map((line) => (
          <div key={line.id} className={`terminal-line ${line.kind}`}>
            {line.text}
          </div>
        ))}
        <div className="terminal-input-row">
          <span className="terminal-prompt">{PROMPT}</span>
          <input
            ref={inputRef}
            data-testid="terminal-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            aria-label="Terminal input"
            autoComplete="off"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            autoFocus
          />
        </div>
      </div>
    </div>
  );
}
