// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useMemo, useRef, useState } from 'react';
import { LOG_UNITS, makeLogLine, seedLogLines, type LogLine, type LogPriority } from '../mock/journal';
import { IconPause, IconPlay, IconSearch } from '../shell/icons';
import '../styles/apps.css';
import '../styles/logs.css';

const PRIORITY_FILTERS: { id: 'all' | LogPriority; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'err', label: 'Errors' },
  { id: 'warning', label: 'Warnings' },
  { id: 'info', label: 'Info' },
  { id: 'debug', label: 'Debug' },
];

export function Logs() {
  const [lines, setLines] = useState<LogLine[]>(() => seedLogLines(40));
  const [paused, setPaused] = useState(false);
  const [priority, setPriority] = useState<'all' | LogPriority>('all');
  const [unit, setUnit] = useState('all');
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (paused) return;
    const id = window.setInterval(() => {
      setLines((prev) => [...prev.slice(-400), makeLogLine()]);
    }, 2000);
    return () => window.clearInterval(id);
  }, [paused]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return lines.filter(
      (line) =>
        (priority === 'all' || line.priority === priority) &&
        (unit === 'all' || line.unit === unit) &&
        (!q || line.message.toLowerCase().includes(q)),
    );
  }, [lines, priority, unit, search]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [filtered.length]);

  const selected = lines.find((l) => l.id === selectedId) ?? null;

  return (
    <div className="app logs" data-testid="app-logs">
      <div className="app-toolbar">
        <div className="logs-priorities" role="group" aria-label="Priority filter">
          {PRIORITY_FILTERS.map((p) => (
            <button
              key={p.id}
              type="button"
              className={`btn${priority === p.id ? ' active' : ''}`}
              data-testid={`logs-filter-${p.id}`}
              aria-pressed={priority === p.id}
              onClick={() => setPriority(p.id)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <select
          className="logs-unit"
          value={unit}
          onChange={(e) => setUnit(e.target.value)}
          aria-label="Filter by unit"
        >
          <option value="all">All units</option>
          {LOG_UNITS.map((u) => (
            <option key={u} value={u}>
              {u}
            </option>
          ))}
        </select>
        <label className="app-search">
          <IconSearch size={13} />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search messages"
            aria-label="Search log messages"
          />
        </label>
        <button
          type="button"
          className="btn"
          data-testid="logs-pause"
          aria-pressed={paused}
          onClick={() => setPaused((p) => !p)}
        >
          {paused ? <IconPlay size={12} /> : <IconPause size={12} />}
          {paused ? 'Resume' : 'Pause'}
        </button>
      </div>

      <div className="logs-list" ref={scrollRef} role="log" aria-label="Journal stream" data-testid="logs-list">
        {filtered.map((line) => (
          <button
            key={line.id}
            type="button"
            className={`logs-row${selectedId === line.id ? ' selected' : ''}`}
            data-testid="logs-row"
            onClick={() => setSelectedId(line.id)}
          >
            <span className="logs-time mono">
              {new Date(line.timestamp).toLocaleTimeString([], { hour12: false })}
            </span>
            <span className={`logs-prio prio-${line.priority}`}>{line.priority}</span>
            <span className="logs-unit-name mono">{line.unit}</span>
            <span className="logs-message">{line.message}</span>
          </button>
        ))}
        {filtered.length === 0 && <p className="logs-empty">No log lines match the current filters.</p>}
      </div>

      {selected && (
        <div className="logs-detail" data-testid="logs-detail" aria-label="Log entry detail">
          <dl>
            <div>
              <dt>PRIORITY</dt>
              <dd className="mono">
                {selected.priorityCode} ({selected.priority})
              </dd>
            </div>
            <div>
              <dt>_SYSTEMD_UNIT</dt>
              <dd className="mono">{selected.unit}</dd>
            </div>
            <div>
              <dt>TIMESTAMP</dt>
              <dd className="mono">{new Date(selected.timestamp).toISOString()}</dd>
            </div>
            <div>
              <dt>_PID</dt>
              <dd className="mono">{selected.pid}</dd>
            </div>
            <div className="logs-detail-message">
              <dt>MESSAGE</dt>
              <dd>{selected.message}</dd>
            </div>
          </dl>
        </div>
      )}
    </div>
  );
}
