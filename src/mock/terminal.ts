// SPDX-License-Identifier: AGPL-3.0-only
import type { TerminalHandlers, TerminalOpenOptions, TerminalSession } from '../api/source';
import { homePath, listDir } from './filesystem';
import { uptimeSeconds } from './system';

const PROMPT = 'user@atlas:~$ ';

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
        out: [all ? 'Linux atlas 6.8.0-45-generic #45-Ubuntu SMP x86_64 x86_64 GNU/Linux' : 'Linux'],
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

export class MockTerminalSession implements TerminalSession {
  private buffer = '';
  private history: string[] = [];
  private historyIndex = -1;
  private escape: string | null = null;
  private closed = false;

  constructor(
    private opts: TerminalOpenOptions,
    private handlers: TerminalHandlers,
  ) {
    queueMicrotask(() => {
      if (this.closed) return;
      this.emit("Lumio OS mock shell — no commands leave this window. Type 'help'.\r\n");
      this.emit(PROMPT);
    });
  }

  write(data: string): void {
    for (const ch of data) this.feed(ch);
  }

  resize(): void {}

  close(): void {
    this.closed = true;
  }

  private emit(text: string): void {
    if (!this.closed) this.handlers.onData(text);
  }

  private feed(ch: string): void {
    if (this.escape !== null) {
      this.escape += ch;
      const seq = this.escape;
      const complete =
        seq.length >= 3 ? /[A-Za-z~]/.test(ch) : seq[1] !== '[' && seq[1] !== 'O';
      if (complete) {
        this.escape = null;
        this.handleSequence(seq);
      }
      return;
    }
    if (ch === '\x1b') {
      this.escape = ch;
      return;
    }
    if (ch === '\r') {
      this.acceptLine();
      return;
    }
    if (ch === '\x7f') {
      if (this.buffer.length > 0) {
        this.buffer = this.buffer.slice(0, -1);
        this.emit('\b \b');
      }
      return;
    }
    if (ch === '\x03') {
      this.buffer = '';
      this.emit('^C\r\n' + PROMPT);
      return;
    }
    if (ch < ' ' && ch !== '\t') return;
    this.buffer += ch;
    this.emit(ch);
  }

  private handleSequence(seq: string): void {
    if (seq !== '\x1b[A' && seq !== '\x1b[B') return;
    if (this.history.length === 0) return;
    if (seq === '\x1b[A') {
      this.historyIndex = this.historyIndex === -1 ? this.history.length - 1 : Math.max(0, this.historyIndex - 1);
    } else if (this.historyIndex !== -1) {
      this.historyIndex = this.historyIndex + 1 >= this.history.length ? -1 : this.historyIndex + 1;
    }
    const next = this.historyIndex === -1 ? '' : (this.history[this.historyIndex] ?? '');
    this.buffer = next;
    this.emit('\r\x1b[K' + PROMPT + next);
  }

  private acceptLine(): void {
    const line = this.buffer;
    this.buffer = '';
    this.historyIndex = -1;
    this.emit('\r\n');
    const result = evaluate(line, { user: this.opts.user ?? 'user' });
    if (result.exit) {
      this.handlers.onExit(0);
      return;
    }
    if (result.clear) {
      this.emit('\x1b[2J\x1b[3J\x1b[H' + PROMPT);
      return;
    }
    if (result.out.length > 0) this.emit(result.out.join('\r\n') + '\r\n');
    if (line.trim()) this.history.push(line);
    this.emit(PROMPT);
  }
}
