// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useMemo, useState } from 'react';
import {
  listServices,
  runServiceAction,
  subscribeServices,
  type ServiceAction,
  type ServiceUnit,
} from '../mock/system';
import { useShell } from '../shell/ShellContext';
import { IconSearch } from '../shell/icons';
import '../styles/apps.css';
import '../styles/services.css';

const ACTION_LABEL: Record<ServiceAction, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  enable: 'Enable',
  disable: 'Disable',
};

const ACTION_DONE: Record<ServiceAction, string> = {
  start: 'started',
  stop: 'stopped',
  restart: 'restarted',
  enable: 'enabled',
  disable: 'disabled',
};

function actionAvailable(unit: ServiceUnit, action: ServiceAction): boolean {
  switch (action) {
    case 'start':
      return unit.state !== 'active';
    case 'stop':
      return unit.state === 'active';
    case 'restart':
      return true;
    case 'enable':
      return !unit.enabled;
    case 'disable':
      return unit.enabled;
  }
}

export function Services() {
  const { actions } = useShell();
  const [services, setServices] = useState<ServiceUnit[]>(() => listServices());
  const [query, setQuery] = useState('');
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<ServiceAction | null>(null);

  useEffect(() => subscribeServices(() => setServices(listServices())), []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return services;
    return services.filter(
      (s) => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q),
    );
  }, [services, query]);

  const selected = services.find((s) => s.name === selectedName) ?? filtered[0] ?? null;

  function run(unit: ServiceUnit, action: ServiceAction) {
    setBusyAction(action);
    runServiceAction(unit.name, action)
      .then((next) => {
        const detail =
          action === 'enable'
            ? `${next.name} will start at boot.`
            : action === 'disable'
              ? `${next.name} will no longer start at boot.`
              : `${next.name} is ${next.state}.`;
        actions.notify(`Service ${ACTION_DONE[action]}`, detail);
      })
      .catch((err: Error) => actions.notify('Service action failed', err.message))
      .finally(() => setBusyAction(null));
  }

  return (
    <div className="app services" data-testid="app-services">
      <div className="app-toolbar">
        <label className="app-search">
          <IconSearch size={13} />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter services"
            aria-label="Filter services"
          />
        </label>
        <span className="services-count">
          {filtered.length} of {services.length} units
        </span>
      </div>
      <div className="services-body">
        <div className="services-list" data-testid="services-list" role="list" aria-label="Services">
          {filtered.map((unit) => (
            <button
              key={unit.name}
              type="button"
              role="listitem"
              data-testid={`service-row-${unit.name}`}
              className={`service-row${selected?.name === unit.name ? ' selected' : ''}${unit.state === 'failed' ? ' failed' : ''}`}
              onClick={() => setSelectedName(unit.name)}
            >
              <span className={`service-dot state-${unit.state}`} aria-hidden="true" />
              <span className="service-names">
                <span className="service-name mono">{unit.name}</span>
                <span className="service-desc">{unit.description}</span>
              </span>
              <span className={`pill pill-${unit.state}`}>{unit.state}</span>
            </button>
          ))}
          {filtered.length === 0 && <p className="services-empty">No units match this filter.</p>}
        </div>
        <aside className="services-detail" aria-label="Service detail">
          {selected ? (
            <>
              <header className="services-detail-header">
                <h2 className="mono">{selected.name}</h2>
                <span className={`pill pill-${selected.state}`}>{selected.state}</span>
              </header>
              <p className="services-detail-desc">{selected.description}</p>
              <dl className="services-detail-grid">
                <div>
                  <dt>PID</dt>
                  <dd className="mono">{selected.pid ?? '—'}</dd>
                </div>
                <div>
                  <dt>Memory</dt>
                  <dd className="mono">{selected.memoryMb > 0 ? `${selected.memoryMb} MB` : '—'}</dd>
                </div>
                <div>
                  <dt>Active since</dt>
                  <dd>{selected.since}</dd>
                </div>
                <div>
                  <dt>At boot</dt>
                  <dd>{selected.enabled ? 'enabled' : 'disabled'}</dd>
                </div>
              </dl>
              <div className="services-actions">
                {(Object.keys(ACTION_LABEL) as ServiceAction[]).map((action) => (
                  <button
                    key={action}
                    type="button"
                    className={`btn${action === 'restart' ? ' btn-primary' : ''}`}
                    data-testid={`service-action-${action}`}
                    disabled={busyAction !== null || !actionAvailable(selected, action)}
                    onClick={() => run(selected, action)}
                  >
                    {busyAction === action ? <span className="spinner" aria-hidden="true" /> : null}
                    {busyAction === action ? 'Working…' : ACTION_LABEL[action]}
                  </button>
                ))}
              </div>
            </>
          ) : (
            <p className="services-empty">Select a service to see details.</p>
          )}
        </aside>
      </div>
    </div>
  );
}
