// SPDX-License-Identifier: AGPL-3.0-only
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react';
import { describeError, getDataSource } from '../api/source';
import { ApiError } from '../api/transport';
import '../styles/reauth.css';

interface ReauthContextValue {
  requireReauth: (action: () => void) => void;
}

const ReauthContext = createContext<ReauthContextValue | null>(null);

export function useReauth(): (action: () => void) => void {
  const ctx = useContext(ReauthContext);
  if (!ctx) throw new Error('useReauth must be used inside ReauthProvider');
  return ctx.requireReauth;
}

export function ReauthProvider({ children }: { children: ReactNode }) {
  const source = getDataSource();
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const requireReauth = useCallback((action: () => void) => {
    setPending(() => action);
    setPassword('');
    setError(null);
    setBusy(false);
  }, []);

  useEffect(() => {
    if (pending) inputRef.current?.focus();
  }, [pending]);

  useEffect(() => {
    if (!pending) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) setPending(null);
      if (e.key === 'Tab') {
        const sheet = document.querySelector<HTMLElement>('[data-testid="reauth-sheet"]');
        if (!sheet) return;
        const focusables = Array.from(sheet.querySelectorAll<HTMLElement>('input, button:not([disabled])'));
        const first = focusables[0];
        const last = focusables[focusables.length - 1];
        if (!first || !last) return;
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [pending, busy]);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const action = pending;
    if (!action || !password || busy) return;
    setBusy(true);
    setError(null);
    source
      .reauth(password)
      .then(() => {
        setPending(null);
        action();
      })
      .catch((err) => {
        setBusy(false);
        setPassword('');
        setError(
          err instanceof ApiError && err.code === 'unauthorized' ? 'Incorrect password.' : describeError(err),
        );
        inputRef.current?.focus();
      });
  }

  return (
    <ReauthContext.Provider value={{ requireReauth }}>
      {children}
      {pending && (
        <div className="reauth-overlay">
          <div
            className="reauth-sheet"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="reauth-title"
            aria-describedby="reauth-desc"
            data-testid="reauth-sheet"
          >
            <h2 id="reauth-title" className="reauth-title">
              Confirm it’s you
            </h2>
            <p id="reauth-desc" className="reauth-desc">
              The server needs your password again before it will run this action.
            </p>
            <form onSubmit={onSubmit}>
              <input
                ref={inputRef}
                type="password"
                data-testid="reauth-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                aria-label="Password"
                placeholder="Password"
                disabled={busy}
              />
              {error && (
                <p className="reauth-error" data-testid="reauth-error" role="alert">
                  {error}
                </p>
              )}
              <div className="reauth-actions">
                <button type="button" className="btn" data-testid="reauth-cancel" disabled={busy} onClick={() => setPending(null)}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" data-testid="reauth-submit" disabled={busy || !password}>
                  {busy ? 'Confirming…' : 'Confirm'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </ReauthContext.Provider>
  );
}
