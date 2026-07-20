// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  describeError,
  getDataSource,
  type JournalBoot,
  type LogLine,
  type LogPriority,
} from '../api/source';
import { useShell } from '../shell/ShellContext';
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

type TimeRange = 'all' | '15m' | '1h' | '24h';
type BootFilter = 'all' | JournalBoot;

interface SavedSearch {
  id: string;
  label: string;
  priority: 'all' | LogPriority;
  unit: string;
  boot: BootFilter;
  timeRange: TimeRange;
  search: string;
}

const SAVED_SEARCHES_KEY = 'lumio-os.logs.saved.v1';
const SAVED_PRIORITIES = new Set(['all', 'err', 'warning', 'info', 'debug']);
const SAVED_BOOTS = new Set(['all', 'current', 'previous']);
const SAVED_TIME_RANGES = new Set(['all', '15m', '1h', '24h']);

function loadSavedSearches(): SavedSearch[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(SAVED_SEARCHES_KEY) ?? '[]') as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is SavedSearch => {
        if (!item || typeof item !== 'object') return false;
        const value = item as Partial<SavedSearch>;
        return (
          typeof value.id === 'string' &&
          typeof value.label === 'string' &&
          typeof value.unit === 'string' &&
          typeof value.priority === 'string' &&
          SAVED_PRIORITIES.has(value.priority) &&
          typeof value.boot === 'string' &&
          SAVED_BOOTS.has(value.boot) &&
          typeof value.timeRange === 'string' &&
          SAVED_TIME_RANGES.has(value.timeRange) &&
          typeof value.search === 'string'
        );
      })
      .slice(0, 20);
  } catch {
    return [];
  }
}

function sinceFor(range: TimeRange): string | undefined {
  const duration = range === '15m' ? 15 * 60_000 : range === '1h' ? 60 * 60_000 : range === '24h' ? 24 * 60 * 60_000 : 0;
  return duration > 0 ? new Date(Date.now() - duration).toISOString() : undefined;
}

function savedSearchLabel(search: Omit<SavedSearch, 'id' | 'label'>): string {
  const parts = [
    search.unit === 'all' ? 'All units' : search.unit,
    search.priority === 'all' ? null : search.priority,
    search.boot === 'all' ? null : search.boot === 'current' ? 'current boot' : 'previous boot',
    search.timeRange === 'all' ? null : `last ${search.timeRange}`,
    search.search.trim() || null,
  ];
  return parts.filter(Boolean).join(' · ');
}

function exportLines(lines: LogLine[]) {
  const body = lines
    .map((line) =>
      JSON.stringify({
        timestamp: new Date(line.timestamp).toISOString(),
        priority: line.priority,
        unit: line.unit,
        message: line.message,
        fields: line.fields,
      }),
    )
    .join('\n');
  const url = URL.createObjectURL(new Blob([body + (body ? '\n' : '')], { type: 'application/x-ndjson' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = `lumio-journal-${new Date().toISOString().replaceAll(':', '-')}.jsonl`;
  link.click();
  URL.revokeObjectURL(url);
}

export function Logs() {
  const { actions, state } = useShell();
  const source = getDataSource();
  const [lines, setLines] = useState<LogLine[]>([]);
  const [units, setUnits] = useState<string[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [streamError, setStreamError] = useState<string | null>(null);
  const [retryNonce, setRetryNonce] = useState(0);
  const [paused, setPaused] = useState(false);
  const [priority, setPriority] = useState<'all' | LogPriority>('all');
  const [unit, setUnit] = useState('all');
  const [boot, setBoot] = useState<BootFilter>('current');
  const [timeRange, setTimeRange] = useState<TimeRange>('1h');
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [savedSearches, setSavedSearches] = useState<SavedSearch[]>(loadSavedSearches);
  const [selectedSavedId, setSelectedSavedId] = useState('');
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (state.navigation?.target === 'logs') {
      setUnit(state.navigation.unit);
    }
  }, [state.navigation?.nonce, state.navigation?.target, state.navigation?.unit]);

  useEffect(() => {
    try {
      localStorage.setItem(SAVED_SEARCHES_KEY, JSON.stringify(savedSearches));
    } catch {
      return;
    }
  }, [savedSearches]);

  useEffect(() => {
    let alive = true;
    source
      .listJournalUnits()
      .then((list) => {
        if (alive) setUnits(list);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [source, retryNonce]);

  useEffect(() => {
    let alive = true;
    source
      .queryJournal({
        limit: 500,
        unit: unit === 'all' ? undefined : unit,
        priority: priority === 'all' ? undefined : priority,
        since: sinceFor(timeRange),
        boot: boot === 'all' ? undefined : boot,
      })
      .then((page) => {
        if (!alive) return;
        setLines(page.entries);
        setSelectedId(null);
        setLoadError(null);
      })
      .catch((err) => {
        if (alive) setLoadError(describeError(err));
      });
    return () => {
      alive = false;
    };
  }, [boot, priority, retryNonce, source, timeRange, unit]);

  useEffect(() => {
    if (paused || boot === 'previous') return;
    setStreamError(null);
    return source.streamJournal(
      (line) => {
        setStreamError(null);
        setLines((prev) => [...prev.slice(-999), line]);
      },
      () => setStreamError('Log stream interrupted. Reconnecting…'),
    );
  }, [boot, source, paused]);

  const unitOptions = useMemo(
    () => [...new Set([...units, ...lines.map((line) => line.unit), ...(unit === 'all' ? [] : [unit])])].sort(),
    [units, lines, unit],
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const since = sinceFor(timeRange);
    const sinceMs = since ? Date.parse(since) : 0;
    return lines.filter(
      (line) =>
        (priority === 'all' || line.priority === priority) &&
        (unit === 'all' || line.unit === unit) &&
        (sinceMs === 0 || line.timestamp >= sinceMs) &&
        (!q ||
          line.message.toLowerCase().includes(q) ||
          line.unit.toLowerCase().includes(q) ||
          Object.values(line.fields).some((value) => value.toLowerCase().includes(q))),
    );
  }, [lines, priority, unit, timeRange, search]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && !paused) el.scrollTop = el.scrollHeight;
  }, [filtered.length, paused]);

  const selected = lines.find((line) => line.id === selectedId) ?? null;
  const selectedFields = selected ? Object.entries(selected.fields).sort(([a], [b]) => a.localeCompare(b)) : [];

  function saveCurrentSearch() {
    const value = { priority, unit, boot, timeRange, search };
    const signature = JSON.stringify(value);
    const existing = savedSearches.find((item) => JSON.stringify({
      priority: item.priority,
      unit: item.unit,
      boot: item.boot,
      timeRange: item.timeRange,
      search: item.search,
    }) === signature);
    if (existing) {
      setSelectedSavedId(existing.id);
      return;
    }
    const saved: SavedSearch = {
      id: crypto.randomUUID(),
      label: savedSearchLabel(value),
      ...value,
    };
    setSavedSearches((current) => [saved, ...current].slice(0, 20));
    setSelectedSavedId(saved.id);
  }

  function applySavedSearch(id: string) {
    setSelectedSavedId(id);
    const saved = savedSearches.find((item) => item.id === id);
    if (!saved) return;
    setPriority(saved.priority);
    setUnit(saved.unit);
    setBoot(saved.boot);
    setTimeRange(saved.timeRange);
    setSearch(saved.search);
  }

  return (
    <div className="app logs" data-testid="app-logs">
      <div className="app-toolbar logs-toolbar">
        <div className="logs-priorities" role="group" aria-label="Priority filter">
          {PRIORITY_FILTERS.map((item) => (
            <button
              key={item.id}
              type="button"
              className={`btn${priority === item.id ? ' active' : ''}`}
              data-testid={`logs-filter-${item.id}`}
              aria-pressed={priority === item.id}
              onClick={() => setPriority(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <select className="logs-select" value={unit} onChange={(event) => setUnit(event.target.value)} aria-label="Filter by unit">
          <option value="all">All units</option>
          {unitOptions.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>
        <select className="logs-select" value={boot} onChange={(event) => setBoot(event.target.value as BootFilter)} aria-label="Filter by boot">
          <option value="all">All boots</option>
          <option value="current">Current boot</option>
          <option value="previous">Previous boot</option>
        </select>
        <select className="logs-select" value={timeRange} onChange={(event) => setTimeRange(event.target.value as TimeRange)} aria-label="Filter by time">
          <option value="15m">Last 15 minutes</option>
          <option value="1h">Last hour</option>
          <option value="24h">Last 24 hours</option>
          <option value="all">Any time</option>
        </select>
        <label className="app-search">
          <IconSearch size={13} />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search fields and messages" aria-label="Search logs" />
        </label>
        <button type="button" className="btn" data-testid="logs-pause" aria-pressed={paused} onClick={() => setPaused((value) => !value)}>
          {paused ? <IconPlay size={12} /> : <IconPause size={12} />}
          {paused ? 'Resume' : 'Pause'}
        </button>
      </div>

      <div className="logs-saved-bar">
        <select value={selectedSavedId} onChange={(event) => applySavedSearch(event.target.value)} aria-label="Saved searches">
          <option value="">Saved searches</option>
          {savedSearches.map((saved) => <option key={saved.id} value={saved.id}>{saved.label}</option>)}
        </select>
        <button type="button" className="btn" data-testid="logs-save-search" onClick={saveCurrentSearch}>Save search</button>
        <button
          type="button"
          className="btn"
          data-testid="logs-delete-search"
          disabled={!selectedSavedId}
          onClick={() => {
            setSavedSearches((current) => current.filter((item) => item.id !== selectedSavedId));
            setSelectedSavedId('');
          }}
        >
          Delete
        </button>
        <span>{filtered.length} entries</span>
        <button type="button" className="btn" data-testid="logs-export" disabled={filtered.length === 0} onClick={() => exportLines(filtered)}>Export JSONL</button>
      </div>

      {streamError && <p className="logs-banner" data-testid="logs-stream-error">{streamError}</p>}

      <div className="logs-list" ref={scrollRef} role="log" aria-label="Journal stream" data-testid="logs-list">
        {filtered.map((line) => (
          <button key={line.id} type="button" className={`logs-row${selectedId === line.id ? ' selected' : ''}`} data-testid="logs-row" onClick={() => setSelectedId(line.id)}>
            <span className="logs-time mono">{new Date(line.timestamp).toLocaleTimeString([], { hour12: false })}</span>
            <span className={`logs-prio prio-${line.priority}`}>{line.priority}</span>
            <span className="logs-unit-name mono">{line.unit}</span>
            <span className="logs-message">{line.message}</span>
          </button>
        ))}
        {loadError && (
          <p className="logs-empty">
            {loadError}{' '}
            <button type="button" className="btn" data-testid="logs-retry" onClick={() => setRetryNonce((value) => value + 1)}>Retry</button>
          </p>
        )}
        {!loadError && filtered.length === 0 && <p className="logs-empty">No log lines match the current filters.</p>}
      </div>

      {selected && (
        <div className="logs-detail" data-testid="logs-detail" aria-label="Log entry detail">
          <header>
            <h2>Structured fields</h2>
            {selected.unit.endsWith('.service') && (
              <button type="button" className="btn" data-testid="logs-open-service" onClick={() => actions.openService(selected.unit)}>Open service</button>
            )}
          </header>
          <dl>
            <div><dt>PRIORITY</dt><dd className="mono">{selected.priorityCode} ({selected.priority})</dd></div>
            <div><dt>_SYSTEMD_UNIT</dt><dd className="mono">{selected.unit}</dd></div>
            <div><dt>TIMESTAMP</dt><dd className="mono">{new Date(selected.timestamp).toISOString()}</dd></div>
            <div className="logs-detail-message"><dt>MESSAGE</dt><dd>{selected.message}</dd></div>
            {selectedFields.map(([name, value]) => <div key={name}><dt>{name}</dt><dd className="mono">{value}</dd></div>)}
          </dl>
        </div>
      )}
    </div>
  );
}
