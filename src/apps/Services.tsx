// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useMemo, useState } from 'react';
import {
  describeError,
  getDataSource,
  isReauthRequired,
  type ServiceAction,
  type ServiceDetail,
  type ServiceUnit,
} from '../api/source';
import { ApiError } from '../api/transport';
import { useReauth } from '../shell/ReauthSheet';
import { useShell } from '../shell/ShellContext';
import { IconSearch } from '../shell/icons';
import '../styles/apps.css';
import '../styles/services.css';

const ACTION_LABEL: Record<ServiceAction, string> = {
  start: 'Start',
  stop: 'Stop',
  restart: 'Restart',
  reload: 'Reload',
  enable: 'Enable',
  disable: 'Disable',
};

const ACTION_DONE: Record<ServiceAction, string> = {
  start: 'started',
  stop: 'stopped',
  restart: 'restarted',
  reload: 'reloaded',
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
    case 'reload':
      return unit.state === 'active';
    case 'enable':
      return !unit.enabled;
    case 'disable':
      return unit.enabled;
  }
}

export function Services() {
  const { actions, state } = useShell();
  const source = getDataSource();
  const requireReauth = useReauth();
  const canAct = source.capabilities.canServiceActions;
  const [services, setServices] = useState<ServiceUnit[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [retryNonce, setRetryNonce] = useState(0);
  const [query, setQuery] = useState('');
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [detail, setDetail] = useState<ServiceDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [busyAction, setBusyAction] = useState<ServiceAction | null>(null);
  const [confirm, setConfirm] = useState<{ unit: ServiceUnit; action: ServiceAction } | null>(null);

  useEffect(() => {
    let alive = true;
    source
      .listServices()
      .then((units) => {
        if (!alive) return;
        setServices(units);
        setLoadError(null);
      })
      .catch((err) => {
        if (alive) setLoadError(describeError(err));
      });
    const unsubscribe = source.subscribeServices((units) => {
      if (alive) setServices(units);
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [source, retryNonce]);

  useEffect(() => {
    if (state.navigation?.target === 'services') {
      setSelectedName(state.navigation.unit);
    }
  }, [state.navigation?.nonce, state.navigation?.target, state.navigation?.unit]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return services;
    return services.filter(
      (s) => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q),
    );
  }, [services, query]);

  const selected = services.find((s) => s.name === selectedName) ?? filtered[0] ?? null;

  useEffect(() => {
    const name = selected?.name;
    if (!name) {
      setDetail(null);
      setDetailError(null);
      return;
    }
    let alive = true;
    setDetailLoading(true);
    setDetail(null);
    setDetailError(null);
    source
      .getServiceDetail(name)
      .then((next) => {
        if (alive) setDetail(next);
      })
      .catch((err) => {
        if (alive) {
          setDetail(null);
          setDetailError(describeError(err));
        }
      })
      .finally(() => {
        if (alive) setDetailLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [selected?.name, source]);

  function run(unit: ServiceUnit, action: ServiceAction) {
    setBusyAction(action);
    source
      .runServiceAction(unit.name, action, unit.state)
      .then((next) => {
        const detail =
          action === 'enable'
            ? `${next.name} will start at boot.`
            : action === 'disable'
              ? `${next.name} will no longer start at boot.`
              : `${next.name} is ${next.state}.`;
        actions.notify(`Service ${ACTION_DONE[action]}`, detail);
      })
      .catch((err) => {
        if (isReauthRequired(err)) {
          requireReauth(() => run(unit, action));
          return;
        }
        if (err instanceof ApiError && err.code === 'conflict') {
          void source
            .listServices()
            .then((units) => setServices(units))
            .catch(() => {});
          actions.notify('Service state changed', `${unit.name} was refreshed from the live system.`);
          return;
        }
        actions.notify('Service action failed', describeError(err));
      })
      .finally(() => setBusyAction(null));
  }

  function request(unit: ServiceUnit, action: ServiceAction) {
    if (action === 'stop' || action === 'disable') setConfirm({ unit, action });
    else run(unit, action);
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
          {loadError && (
            <p className="services-empty">
              {loadError}{' '}
              <button type="button" className="btn" data-testid="services-retry" onClick={() => setRetryNonce((n) => n + 1)}>
                Retry
              </button>
            </p>
          )}
          {!loadError && filtered.length === 0 && <p className="services-empty">No units match this filter.</p>}
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
                    disabled={!canAct || busyAction !== null || !actionAvailable(selected, action)}
                    title={canAct ? undefined : 'Service actions are not available on this host'}
                    onClick={() => request(selected, action)}
                  >
                    {busyAction === action ? <span className="spinner" aria-hidden="true" /> : null}
                    {busyAction === action ? 'Working…' : ACTION_LABEL[action]}
                  </button>
                ))}
                <button
                  type="button"
                  className="btn"
                  data-testid="service-open-logs"
                  onClick={() => actions.openLogs(selected.name)}
                >
                  Open logs
                </button>
              </div>
              {!canAct && (
                <p className="services-actions-note" data-testid="services-actions-note">
                  Service actions are not available on this host.
                </p>
              )}
              <section className="services-section" aria-labelledby="service-dependencies-heading">
                <h3 id="service-dependencies-heading">Dependencies</h3>
                {detailLoading && <p className="services-section-empty">Loading dependencies…</p>}
                {!detailLoading && detailError && <p className="services-section-empty">{detailError}</p>}
                {!detailLoading && !detailError && detail?.dependencies.length === 0 && (
                  <p className="services-section-empty">No direct dependencies reported.</p>
                )}
                {detail && detail.dependencies.length > 0 && (
                  <div className="services-dependency-graph" data-testid="service-dependencies">
                    <span className="services-dependency-root mono">{detail.name}</span>
                    <div className="services-dependency-edges">
                      {detail.dependencies.map((dependency) => (
                        <div className="services-dependency-edge" key={`${dependency.relation}:${dependency.name}`}>
                          <span>{dependency.relation}</span>
                          {dependency.name.endsWith('.service') ? (
                            <button type="button" className="mono" onClick={() => actions.openService(dependency.name)}>
                              {dependency.name}
                            </button>
                          ) : (
                            <code>{dependency.name}</code>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </section>
              <section className="services-section" aria-labelledby="service-files-heading">
                <h3 id="service-files-heading">Unit files and overrides</h3>
                {!detailLoading && !detailError && detail?.files.length === 0 && (
                  <p className="services-section-empty">This unit has no file on disk.</p>
                )}
                <div className="services-unit-files" data-testid="service-unit-files">
                  {detail?.files.map((file) => (
                    <details key={file.path}>
                      <summary>
                        <span className="mono">{file.path}</span>
                        {file.override && <span className="pill">override</span>}
                      </summary>
                      {file.error ? <p>{file.error}</p> : <pre>{file.content}</pre>}
                    </details>
                  ))}
                </div>
              </section>
            </>
          ) : (
            <p className="services-empty">Select a service to see details.</p>
          )}
        </aside>
      </div>

      {confirm && (
        <div className="quicklook-overlay" onPointerDown={() => setConfirm(null)}>
          <div
            className="file-confirm"
            role="alertdialog"
            aria-modal="true"
            aria-label={`${ACTION_LABEL[confirm.action]} ${confirm.unit.name}`}
            data-testid="service-confirm"
            onPointerDown={(e) => e.stopPropagation()}
          >
            <p>
              {confirm.action === 'stop'
                ? `Stop ${confirm.unit.name}? Running processes will be terminated.`
                : `Disable ${confirm.unit.name}? It will no longer start at boot.`}
            </p>
            <div className="file-confirm-actions">
              <button type="button" className="btn" data-testid="service-confirm-cancel" onClick={() => setConfirm(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-danger"
                data-testid="service-confirm-ok"
                onClick={() => {
                  const pending = confirm;
                  setConfirm(null);
                  run(pending.unit, pending.action);
                }}
              >
                {ACTION_LABEL[confirm.action]}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
