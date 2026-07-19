// SPDX-License-Identifier: AGPL-3.0-only
import { useState, type FormEvent } from 'react';
import { describeError, getDataSource } from '../api/source';
import { ApiError } from '../api/transport';
import { useShell } from './ShellContext';
import '../styles/login.css';

export function LoginScreen() {
  const { actions } = useShell();
  const source = getDataSource();
  const isLive = source.capabilities.isLive;
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const initial = username.trim().charAt(0).toUpperCase() || 'L';
  const canSubmit = username.trim().length > 0 && password.length > 0 && !busy;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    if (!isLive) {
      actions.login(username.trim());
      return;
    }
    setBusy(true);
    setError(null);
    source
      .login(username.trim(), password)
      .then((user) => actions.login(user.name))
      .catch((err) => {
        setBusy(false);
        setError(
          err instanceof ApiError && err.code === 'unauthorized'
            ? 'Incorrect username or password.'
            : describeError(err),
        );
      });
  }

  return (
    <div className="login wallpaper" data-testid="login-screen">
      <form className="login-card" onSubmit={onSubmit} aria-label="Log in to Lumio OS">
        <div className="login-avatar" aria-hidden="true">
          <span>{initial}</span>
        </div>
        <h1 className="login-title">Lumio OS</h1>
        <p className="login-subtitle">{isLive ? 'Sign in with your server account' : 'atlas.lan · Ubuntu 24.04 LTS'}</p>
        <label className="login-field">
          <span>Username</span>
          <input
            data-testid="login-username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            disabled={busy}
            autoFocus
          />
        </label>
        <label className="login-field">
          <span>Password</span>
          <input
            data-testid="login-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            disabled={busy}
          />
        </label>
        {error && (
          <p className="login-error" data-testid="login-error" role="alert">
            {error}
          </p>
        )}
        <button className="login-submit" data-testid="login-submit" type="submit" disabled={!canSubmit}>
          {busy ? 'Signing in…' : 'Log in'}
        </button>
        {!isLive && <p className="login-hint">Phase 1 preview — any credentials sign you in.</p>}
      </form>
    </div>
  );
}
