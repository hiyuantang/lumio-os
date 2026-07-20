// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useState } from 'react';
import {
  describeError,
  getDataSource,
  isReauthRequired,
  type NetworkConfig,
  type NetworkInterface,
  type NetworkSnapshot,
  type PendingNetworkChange,
} from '../api/source';
import { useReauth } from '../shell/ReauthSheet';
import { useShell } from '../shell/ShellContext';
import '../styles/apps.css';
import '../styles/network.css';

const PENDING_KEY = 'lumio.network.pending.v1';

function loadPending(): PendingNetworkChange | null {
  try {
    const value = sessionStorage.getItem(PENDING_KEY);
    if (!value) return null;
    const pending = JSON.parse(value) as PendingNetworkChange;
    if (!pending.token || Date.parse(pending.expiresAt) <= Date.now()) {
      sessionStorage.removeItem(PENDING_KEY);
      return null;
    }
    return pending;
  } catch {
    sessionStorage.removeItem(PENDING_KEY);
    return null;
  }
}

function preferredInterface(interfaces: NetworkInterface[]): NetworkInterface | undefined {
  return interfaces.find((item) => item.up && !item.loopback) ?? interfaces.find((item) => !item.loopback) ?? interfaces[0];
}

function currentAddress(item: NetworkInterface | undefined): string {
  return item?.addresses.find((address) => !address.startsWith('fe80:')) ?? '';
}

export function Network() {
  const source = getDataSource();
  const { actions } = useShell();
  const requireReauth = useReauth();
  const [snapshot, setSnapshot] = useState<NetworkSnapshot | null>(null);
  const [selectedName, setSelectedName] = useState('');
  const [mode, setMode] = useState<'dhcp' | 'static' | null>(null);
  const [address, setAddress] = useState('');
  const [gateway, setGateway] = useState('');
  const [dns, setDNS] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<'load' | 'apply' | 'confirm' | null>('load');
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState<PendingNetworkChange | null>(loadPending);
  const [now, setNow] = useState(Date.now());
  const canConfigure = source.capabilities.canConfigureNetwork;

  function loadSnapshot() {
    setBusy('load');
    setError(null);
    return source
      .getNetworkSnapshot()
      .then((next) => {
        setSnapshot(next);
        setSelectedName((current) => {
          if (current && next.interfaces.some((item) => item.name === current)) return current;
          return preferredInterface(next.interfaces)?.name ?? '';
        });
      })
      .catch((err) => setError(describeError(err)))
      .finally(() => setBusy(null));
  }

  useEffect(() => {
    loadSnapshot();
  }, [source]);

  useEffect(() => {
    if (!pending) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [pending]);

  useEffect(() => {
    if (!pending || Date.parse(pending.expiresAt) > now) return;
    sessionStorage.removeItem(PENDING_KEY);
    setPending(null);
    actions.notify('Network confirmation ended', 'The previous Netplan configuration is being restored.');
  }, [actions, now, pending]);

  const selected = snapshot?.interfaces.find((item) => item.name === selectedName);
  const remainingSeconds = pending ? Math.max(0, Math.ceil((Date.parse(pending.expiresAt) - now) / 1000)) : 0;

  function selectInterface(item: NetworkInterface) {
    setSelectedName(item.name);
    setMode(null);
    setAddress(currentAddress(item));
    setGateway('');
    setDNS('');
    setError(null);
  }

  function buildConfig(): NetworkConfig | null {
    if (!selected || !mode) return null;
    if (mode === 'dhcp') {
      return { version: 2, ethernets: { [selected.name]: { dhcp4: true, dhcp6: true } } };
    }
    if (!address) return null;
    return {
      version: 2,
      ethernets: {
        [selected.name]: {
          dhcp4: false,
          dhcp6: false,
          addresses: [address],
          nameservers: dns ? { addresses: [dns] } : undefined,
          routes: gateway ? [{ to: 'default', via: gateway }] : undefined,
        },
      },
    };
  }

  function applyCandidate() {
    const config = buildConfig();
    if (!config || !snapshot) return;
    setConfirming(false);
    setBusy('apply');
    setError(null);
    source
      .applyNetworkConfig(config, snapshot.revision)
      .then((next) => {
        sessionStorage.setItem(PENDING_KEY, JSON.stringify(next));
        setPending(next);
        setNow(Date.now());
        actions.notify('Network change is being tested', 'Reconnect if needed, then keep the change before the timer ends.');
      })
      .catch((err) => {
        if (isReauthRequired(err)) {
          requireReauth(applyCandidate);
          return;
        }
        setError(describeError(err));
      })
      .finally(() => setBusy(null));
  }

  function keepChanges() {
    if (!pending) return;
    setBusy('confirm');
    setError(null);
    source
      .confirmNetworkConfig(pending.token)
      .then(() => {
        sessionStorage.removeItem(PENDING_KEY);
        setPending(null);
        actions.notify('Network change kept', 'The Netplan configuration is now committed.');
        return loadSnapshot();
      })
      .catch((err) => {
        if (isReauthRequired(err)) {
          requireReauth(keepChanges);
          return;
        }
        setError(describeError(err));
      })
      .finally(() => setBusy(null));
  }

  const candidate = buildConfig();

  return (
    <div className="app network-app" data-testid="app-network">
      <aside className="network-sidebar" aria-label="Network interfaces">
        <header>
          <strong>Interfaces</strong>
          <button type="button" className="btn network-refresh" disabled={busy !== null} onClick={loadSnapshot}>
            Refresh
          </button>
        </header>
        <div className="network-interface-list">
          {snapshot?.interfaces.map((item) => (
            <button
              type="button"
              className={`network-interface ${item.name === selectedName ? 'selected' : ''}`}
              data-testid={`network-interface-${item.name}`}
              aria-current={item.name === selectedName ? 'page' : undefined}
              key={item.name}
              onClick={() => selectInterface(item)}
            >
              <span className={`network-status ${item.up ? 'up' : ''}`} aria-hidden="true" />
              <span><strong>{item.name}</strong><small>{item.up ? 'Connected' : 'Inactive'}</small></span>
            </button>
          ))}
        </div>
      </aside>

      <main className="network-content">
        <header className="network-heading">
          <div>
            <h2>{selected?.name ?? 'Network'}</h2>
            <p>{selected ? (selected.up ? 'Connected interface' : 'Inactive interface') : 'No interface selected'}</p>
          </div>
          {selected ? <span className={`network-state ${selected.up ? 'up' : ''}`}>{selected.up ? 'Up' : 'Down'}</span> : null}
        </header>

        {error ? <p className="network-error" role="alert">{error}</p> : null}

        {pending ? (
          <section className="network-pending" data-testid="network-pending" aria-live="polite">
            <div>
              <strong>Keep this network change?</strong>
              <span>Automatic rollback in {remainingSeconds} seconds.</span>
            </div>
            <button type="button" className="btn btn-primary" data-testid="network-keep" disabled={busy !== null} onClick={keepChanges}>
              {busy === 'confirm' ? 'Keeping…' : 'Keep changes'}
            </button>
          </section>
        ) : null}

        {selected ? (
          <>
            <section className="network-details" aria-label="Interface details">
              <div><span>Hardware address</span><strong className="mono">{selected.hardwareAddress || 'Not reported'}</strong></div>
              <div><span>Current addresses</span><strong className="mono">{selected.addresses.join(', ') || 'None'}</strong></div>
            </section>

            <section className="network-editor" aria-label="Network configuration">
              <header>
                <h3>IP configuration</h3>
                <p>Choose a complete typed configuration. Lumio will test it before committing.</p>
              </header>
              <div className="network-mode" role="group" aria-label="Address mode">
                <button type="button" aria-pressed={mode === 'dhcp'} data-testid="network-mode-dhcp" onClick={() => setMode('dhcp')}>Automatic (DHCP)</button>
                <button type="button" aria-pressed={mode === 'static'} data-testid="network-mode-static" onClick={() => setMode('static')}>Manual</button>
              </div>

              {mode === 'static' ? (
                <div className="network-fields">
                  <label>Address and prefix<input value={address} placeholder="192.0.2.10/24" onChange={(event) => setAddress(event.target.value)} /></label>
                  <label>Default gateway<input value={gateway} placeholder="192.0.2.1" onChange={(event) => setGateway(event.target.value)} /></label>
                  <label>DNS server<input value={dns} placeholder="1.1.1.1" onChange={(event) => setDNS(event.target.value)} /></label>
                </div>
              ) : null}

              <footer>
                <span>{mode ? 'Changes automatically roll back unless you keep them.' : 'Select Automatic or Manual to begin.'}</span>
                <button
                  type="button"
                  className="btn btn-primary"
                  data-testid="network-apply"
                  disabled={!canConfigure || busy !== null || pending !== null || candidate === null}
                  onClick={() => setConfirming(true)}
                >
                  {busy === 'apply' ? 'Applying…' : 'Apply and test…'}
                </button>
              </footer>
            </section>
          </>
        ) : busy === 'load' ? <div className="network-empty">Loading network interfaces…</div> : <div className="network-empty">No network interfaces are available.</div>}
      </main>

      {confirming && candidate ? (
        <div className="quicklook-overlay" onPointerDown={() => setConfirming(false)}>
          <div className="file-confirm network-confirm" role="alertdialog" aria-modal="true" aria-label="Test network change" data-testid="network-confirm-dialog" onPointerDown={(event) => event.stopPropagation()}>
            <p>Test this network change?</p>
            <span>Lumio restores the previous Netplan configuration unless you reconnect and keep the change within 90 seconds.</span>
            <div className="file-confirm-actions">
              <button type="button" className="btn" onClick={() => setConfirming(false)}>Cancel</button>
              <button type="button" className="btn btn-primary" data-testid="network-confirm-apply" onClick={applyCandidate}>Apply and test</button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
