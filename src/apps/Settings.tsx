// SPDX-License-Identifier: AGPL-3.0-only
import { useState } from 'react';
import {
  describeError,
  getDataSource,
  isReauthRequired,
  type PowerAction,
} from '../api/source';
import { useReauth } from '../shell/ReauthSheet';
import { useShell } from '../shell/ShellContext';
import '../styles/apps.css';
import '../styles/settings.css';

const POWER_COPY: Record<PowerAction, { title: string; prompt: string; result: string }> = {
  reboot: {
    title: 'Restart',
    prompt: 'Restart the server? Active sessions will be disconnected.',
    result: 'The server will restart in a few seconds.',
  },
  poweroff: {
    title: 'Shut down',
    prompt: 'Shut down the server? It will stay offline until it is powered on again.',
    result: 'The server will shut down in a few seconds.',
  },
};

export function Settings() {
  const source = getDataSource();
  const { actions } = useShell();
  const requireReauth = useReauth();
  const [confirm, setConfirm] = useState<PowerAction | null>(null);
  const [busy, setBusy] = useState<PowerAction | null>(null);
  const canPower = source.capabilities.canPowerControl;

  function run(action: PowerAction) {
    setBusy(action);
    source
      .runPowerAction(action)
      .then(() => actions.notify(`${POWER_COPY[action].title} scheduled`, POWER_COPY[action].result))
      .catch((err) => {
        if (isReauthRequired(err)) {
          requireReauth(() => run(action));
          return;
        }
        actions.notify('Power action failed', describeError(err));
      })
      .finally(() => setBusy(null));
  }

  return (
    <div className="app settings" data-testid="app-settings">
      <nav className="settings-sidebar" aria-label="Settings sections">
        <button type="button" className="settings-nav-item selected" aria-current="page">
          System
        </button>
      </nav>
      <main className="settings-content">
        <header className="settings-heading">
          <h2>System</h2>
          <p>Restart or shut down this server.</p>
        </header>
        <section className="settings-group" aria-label="Power">
          <div className="settings-row">
            <div>
              <h3>Restart</h3>
              <p>Stop active sessions, then start the server again.</p>
            </div>
            <button
              type="button"
              className="btn"
              data-testid="settings-reboot"
              disabled={!canPower || busy !== null}
              onClick={() => setConfirm('reboot')}
            >
              {busy === 'reboot' ? <span className="spinner" aria-hidden="true" /> : null}
              {busy === 'reboot' ? 'Scheduling…' : 'Restart…'}
            </button>
          </div>
          <div className="settings-row">
            <div>
              <h3>Shut down</h3>
              <p>Power off the server and disconnect every session.</p>
            </div>
            <button
              type="button"
              className="btn btn-danger"
              data-testid="settings-poweroff"
              disabled={!canPower || busy !== null}
              onClick={() => setConfirm('poweroff')}
            >
              {busy === 'poweroff' ? <span className="spinner" aria-hidden="true" /> : null}
              {busy === 'poweroff' ? 'Scheduling…' : 'Shut down…'}
            </button>
          </div>
        </section>
        {!canPower ? <p className="settings-unavailable">Power controls are not available on this host.</p> : null}
      </main>

      {confirm ? (
        <div className="quicklook-overlay" onPointerDown={() => setConfirm(null)}>
          <div
            className="file-confirm"
            role="alertdialog"
            aria-modal="true"
            aria-label={`${POWER_COPY[confirm].title} server`}
            data-testid="settings-power-confirm"
            onPointerDown={(event) => event.stopPropagation()}
          >
            <p>{POWER_COPY[confirm].prompt}</p>
            <div className="file-confirm-actions">
              <button type="button" className="btn" onClick={() => setConfirm(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-danger"
                data-testid="settings-confirm-action"
                onClick={() => {
                  const action = confirm;
                  setConfirm(null);
                  run(action);
                }}
              >
                {POWER_COPY[confirm].title}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
