// SPDX-License-Identifier: AGPL-3.0-only
import { useState, type FormEvent } from 'react';
import { useShell } from './ShellContext';
import '../styles/login.css';

export function LoginScreen() {
  const { actions } = useShell();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  const initial = username.trim().charAt(0).toUpperCase() || 'L';
  const canSubmit = username.trim().length > 0 && password.length > 0;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (canSubmit) actions.login(username.trim());
  }

  return (
    <div className="login wallpaper" data-testid="login-screen">
      <form className="login-card" onSubmit={onSubmit} aria-label="Log in to Lumio OS">
        <div className="login-avatar" aria-hidden="true">
          <span>{initial}</span>
        </div>
        <h1 className="login-title">Lumio OS</h1>
        <p className="login-subtitle">atlas.lan · Ubuntu 24.04 LTS</p>
        <label className="login-field">
          <span>Username</span>
          <input
            data-testid="login-username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
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
          />
        </label>
        <button className="login-submit" data-testid="login-submit" type="submit" disabled={!canSubmit}>
          Log in
        </button>
        <p className="login-hint">Phase 1 preview — any credentials sign you in.</p>
      </form>
    </div>
  );
}
