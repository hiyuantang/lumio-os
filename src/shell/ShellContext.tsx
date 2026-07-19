// SPDX-License-Identifier: AGPL-3.0-only
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  type ReactNode,
} from 'react';
import { getDataSource } from '../api/source';
import { APPS, APP_ORDER, type AppId } from '../apps/registry';

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface WindowState extends Rect {
  appId: AppId;
  z: number;
  minimized: boolean;
  maximized: boolean;
  restore: Rect | null;
}

export interface ShellNotification {
  id: number;
  title: string;
  body: string;
  ts: number;
}

export type ThemePref = 'light' | 'dark' | null;
export type MotionPref = 'system' | 'reduced' | 'full';

export const MENUBAR_H = 32;

interface Viewport {
  w: number;
  h: number;
}

interface ShellState {
  user: string | null;
  authReady: boolean;
  windows: Partial<Record<AppId, WindowState>>;
  zTop: number;
  focused: AppId | null;
  notifications: ShellNotification[];
  unread: number;
  theme: ThemePref;
  motion: MotionPref;
  paletteOpen: boolean;
  notifOpen: boolean;
  shortcutsOpen: boolean;
  viewport: Viewport;
}

type Action =
  | { type: 'login'; user: string }
  | { type: 'logout' }
  | { type: 'auth-ready' }
  | { type: 'open-app'; appId: AppId }
  | { type: 'close-app'; appId: AppId }
  | { type: 'close-focused' }
  | { type: 'focus-app'; appId: AppId }
  | { type: 'minimize-app'; appId: AppId }
  | { type: 'toggle-maximize'; appId: AppId }
  | { type: 'update-rect'; appId: AppId; rect: Rect }
  | { type: 'cycle-window'; dir: 1 | -1 }
  | { type: 'notify'; title: string; body: string }
  | { type: 'clear-notifications' }
  | { type: 'toggle-theme' }
  | { type: 'toggle-motion' }
  | { type: 'set-palette'; open: boolean }
  | { type: 'toggle-palette' }
  | { type: 'set-notif-open'; open: boolean }
  | { type: 'set-shortcuts-open'; open: boolean }
  | { type: 'set-viewport'; viewport: Viewport };

const SESSION_KEY = 'lumio-os.session.v1';
const WINDOWS_KEY = 'lumio-os.windows.v1';
const PREFS_KEY = 'lumio-os.prefs.v1';

let notificationId = 1;

function loadJSON<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

function clampRect(rect: Rect, viewport: Viewport): Rect {
  const w = Math.min(rect.w, viewport.w);
  const h = Math.min(rect.h, Math.max(160, viewport.h - MENUBAR_H));
  const x = Math.min(Math.max(rect.x, 0), Math.max(0, viewport.w - w));
  const y = Math.min(Math.max(rect.y, MENUBAR_H), Math.max(MENUBAR_H, viewport.h - h));
  return { x, y, w, h };
}

function fullAreaRect(viewport: Viewport): Rect {
  return { x: 0, y: MENUBAR_H, w: viewport.w, h: Math.max(160, viewport.h - MENUBAR_H) };
}

function reducer(state: ShellState, action: Action): ShellState {
  switch (action.type) {
    case 'login':
      return { ...state, user: action.user };
    case 'logout':
      return {
        ...state,
        user: null,
        windows: {},
        focused: null,
        paletteOpen: false,
        notifOpen: false,
        shortcutsOpen: false,
      };
    case 'auth-ready':
      return { ...state, authReady: true };
    case 'open-app': {
      const existing = state.windows[action.appId];
      const z = state.zTop + 1;
      if (existing) {
        return {
          ...state,
          zTop: z,
          focused: action.appId,
          windows: { ...state.windows, [action.appId]: { ...existing, minimized: false, z } },
        };
      }
      const meta = APPS[action.appId];
      const openCount = Object.keys(state.windows).length;
      const rect = clampRect(
        {
          x: 96 + openCount * 40,
          y: MENUBAR_H + 40 + openCount * 32,
          w: meta.defaultSize.w,
          h: meta.defaultSize.h,
        },
        state.viewport,
      );
      const win: WindowState = {
        appId: action.appId,
        ...rect,
        z,
        minimized: false,
        maximized: false,
        restore: null,
      };
      return {
        ...state,
        zTop: z,
        focused: action.appId,
        windows: { ...state.windows, [action.appId]: win },
      };
    }
    case 'close-app': {
      if (!state.windows[action.appId]) return state;
      const windows = { ...state.windows };
      delete windows[action.appId];
      let focused = state.focused;
      if (focused === action.appId) {
        const remaining = Object.values(windows)
          .filter((w): w is WindowState => Boolean(w))
          .sort((a, b) => b.z - a.z);
        focused = remaining[0]?.appId ?? null;
      }
      return { ...state, windows, focused };
    }
    case 'close-focused':
      return state.focused ? reducer(state, { type: 'close-app', appId: state.focused }) : state;
    case 'focus-app': {
      const win = state.windows[action.appId];
      if (!win) return state;
      if (state.focused === action.appId && !win.minimized) return state;
      const z = state.zTop + 1;
      return {
        ...state,
        zTop: z,
        focused: action.appId,
        windows: { ...state.windows, [action.appId]: { ...win, minimized: false, z } },
      };
    }
    case 'minimize-app': {
      const win = state.windows[action.appId];
      if (!win) return state;
      const windows = { ...state.windows, [action.appId]: { ...win, minimized: true } };
      const remaining = Object.values(windows)
        .filter((w): w is WindowState => Boolean(w) && !w.minimized)
        .sort((a, b) => b.z - a.z);
      const focused = state.focused === action.appId ? (remaining[0]?.appId ?? null) : state.focused;
      return { ...state, windows, focused };
    }
    case 'toggle-maximize': {
      const win = state.windows[action.appId];
      if (!win) return state;
      const z = state.zTop + 1;
      const next: WindowState = win.maximized
        ? { ...win, maximized: false, ...(win.restore ?? win), restore: null, z }
        : { ...win, maximized: true, restore: { x: win.x, y: win.y, w: win.w, h: win.h }, ...fullAreaRect(state.viewport), z };
      return {
        ...state,
        zTop: z,
        focused: action.appId,
        windows: { ...state.windows, [action.appId]: next },
      };
    }
    case 'update-rect': {
      const win = state.windows[action.appId];
      if (!win || win.maximized) return state;
      const rect = clampRect(action.rect, state.viewport);
      return { ...state, windows: { ...state.windows, [action.appId]: { ...win, ...rect } } };
    }
    case 'cycle-window': {
      const visible = APP_ORDER.filter((id) => {
        const w = state.windows[id];
        return w && !w.minimized;
      });
      if (visible.length === 0) return state;
      const raw = visible.indexOf(state.focused as AppId);
      const idx = raw === -1 ? (action.dir === 1 ? -1 : 0) : raw;
      const nextId = visible[(idx + action.dir + visible.length) % visible.length] ?? visible[0];
      return reducer(state, { type: 'focus-app', appId: nextId });
    }
    case 'notify':
      return {
        ...state,
        unread: state.unread + 1,
        notifications: [
          { id: notificationId++, title: action.title, body: action.body, ts: Date.now() },
          ...state.notifications,
        ].slice(0, 50),
      };
    case 'clear-notifications':
      return { ...state, notifications: [], unread: 0 };
    case 'toggle-theme': {
      const resolved = state.theme ?? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
      return { ...state, theme: resolved === 'dark' ? 'light' : 'dark' };
    }
    case 'toggle-motion': {
      const reduced =
        state.motion === 'reduced' ||
        (state.motion === 'system' && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      return { ...state, motion: reduced ? 'full' : 'reduced' };
    }
    case 'set-palette':
      return { ...state, paletteOpen: action.open, notifOpen: action.open ? false : state.notifOpen };
    case 'toggle-palette':
      return { ...state, paletteOpen: !state.paletteOpen, notifOpen: false };
    case 'set-notif-open':
      return {
        ...state,
        notifOpen: action.open,
        paletteOpen: action.open ? false : state.paletteOpen,
        unread: action.open ? 0 : state.unread,
      };
    case 'set-shortcuts-open':
      return { ...state, shortcutsOpen: action.open };
    case 'set-viewport': {
      if (action.viewport.w === state.viewport.w && action.viewport.h === state.viewport.h) return state;
      const windows: Partial<Record<AppId, WindowState>> = {};
      for (const [id, win] of Object.entries(state.windows)) {
        if (!win) continue;
        windows[id as AppId] = win.maximized
          ? { ...win, ...fullAreaRect(action.viewport) }
          : { ...win, ...clampRect(win, action.viewport) };
      }
      return { ...state, viewport: action.viewport, windows };
    }
    default:
      return state;
  }
}

function initState(): ShellState {
  const session = loadJSON<{ user: string }>(SESSION_KEY);
  const prefs = loadJSON<{ theme: ThemePref; motion: MotionPref }>(PREFS_KEY);
  const isLive = getDataSource().capabilities.isLive;
  const stored = loadJSON<{
    windows: Partial<Record<AppId, WindowState>>;
    zTop: number;
    focused: AppId | null;
  }>(WINDOWS_KEY);
  const viewport = { w: window.innerWidth, h: window.innerHeight };
  const windows: Partial<Record<AppId, WindowState>> = {};
  if (stored?.windows) {
    for (const [id, win] of Object.entries(stored.windows)) {
      if (!win || !APPS[id as AppId]) continue;
      windows[id as AppId] = win.maximized
        ? { ...win, ...fullAreaRect(viewport) }
        : { ...win, ...clampRect(win, viewport) };
    }
  }
  return {
    user: isLive ? null : (session?.user ?? null),
    authReady: !isLive,
    windows,
    zTop: stored?.zTop ?? 0,
    focused: stored?.focused && windows[stored.focused] ? stored.focused : null,
    notifications: [],
    unread: 0,
    theme: prefs?.theme ?? null,
    motion: prefs?.motion ?? 'system',
    paletteOpen: false,
    notifOpen: false,
    shortcutsOpen: false,
    viewport,
  };
}

export interface ShellActions {
  login(user: string): void;
  logout(): void;
  openApp(appId: AppId): void;
  closeApp(appId: AppId): void;
  focusApp(appId: AppId): void;
  minimizeApp(appId: AppId): void;
  toggleMaximize(appId: AppId): void;
  updateRect(appId: AppId, rect: Rect): void;
  notify(title: string, body: string): void;
  clearNotifications(): void;
  toggleTheme(): void;
  toggleMotion(): void;
  setPalette(open: boolean): void;
  setNotifOpen(open: boolean): void;
  setShortcutsOpen(open: boolean): void;
}

interface ShellContextValue {
  state: ShellState;
  actions: ShellActions;
  resolvedTheme: 'light' | 'dark';
  reducedMotion: boolean;
}

const ShellContext = createContext<ShellContextValue | null>(null);

export function ShellProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, null as unknown as ShellState, initState);

  const systemDark = useMediaQuery('(prefers-color-scheme: dark)');
  const systemReduced = useMediaQuery('(prefers-reduced-motion: reduce)');
  const resolvedTheme: 'light' | 'dark' = state.theme ?? (systemDark ? 'dark' : 'light');
  const reducedMotion = state.motion === 'reduced' || (state.motion === 'system' && systemReduced);

  useEffect(() => {
    const source = getDataSource();
    if (!source.capabilities.isLive) return;
    let alive = true;
    source
      .getSession()
      .then((user) => {
        if (!alive) return;
        if (user) dispatch({ type: 'login', user: user.name });
        dispatch({ type: 'auth-ready' });
      })
      .catch(() => {
        if (alive) dispatch({ type: 'auth-ready' });
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    const source = getDataSource();
    if (!source.capabilities.isLive) return;
    return source.onSessionExpired(() => dispatch({ type: 'logout' }));
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme;
  }, [resolvedTheme]);

  useEffect(() => {
    document.documentElement.classList.toggle('motion-reduced', reducedMotion);
  }, [reducedMotion]);

  useEffect(() => {
    try {
      localStorage.setItem(
        WINDOWS_KEY,
        JSON.stringify({ windows: state.windows, zTop: state.zTop, focused: state.focused }),
      );
    } catch {
      /* storage unavailable */
    }
  }, [state.windows, state.zTop, state.focused]);

  useEffect(() => {
    try {
      if (state.user) localStorage.setItem(SESSION_KEY, JSON.stringify({ user: state.user }));
      else localStorage.removeItem(SESSION_KEY);
    } catch {
      /* storage unavailable */
    }
  }, [state.user]);

  useEffect(() => {
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify({ theme: state.theme, motion: state.motion }));
    } catch {
      /* storage unavailable */
    }
  }, [state.theme, state.motion]);

  useEffect(() => {
    const onResize = () => dispatch({ type: 'set-viewport', viewport: { w: window.innerWidth, h: window.innerHeight } });
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && !e.altKey && e.code === 'KeyK') {
        e.preventDefault();
        dispatch({ type: 'toggle-palette' });
      } else if (e.altKey && !mod && e.code === 'KeyW') {
        e.preventDefault();
        dispatch({ type: 'close-focused' });
      } else if (e.ctrlKey && e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
        e.preventDefault();
        dispatch({ type: 'cycle-window', dir: e.key === 'ArrowRight' ? 1 : -1 });
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const actions = useMemo<ShellActions>(
    () => ({
      login: (user) => dispatch({ type: 'login', user }),
      logout: () => {
        const source = getDataSource();
        if (source.capabilities.isLive) {
          void source
            .logout()
            .catch(() => {})
            .finally(() => dispatch({ type: 'logout' }));
        } else {
          dispatch({ type: 'logout' });
        }
      },
      openApp: (appId) => dispatch({ type: 'open-app', appId }),
      closeApp: (appId) => dispatch({ type: 'close-app', appId }),
      focusApp: (appId) => dispatch({ type: 'focus-app', appId }),
      minimizeApp: (appId) => dispatch({ type: 'minimize-app', appId }),
      toggleMaximize: (appId) => dispatch({ type: 'toggle-maximize', appId }),
      updateRect: (appId, rect) => dispatch({ type: 'update-rect', appId, rect }),
      notify: (title, body) => dispatch({ type: 'notify', title, body }),
      clearNotifications: () => dispatch({ type: 'clear-notifications' }),
      toggleTheme: () => dispatch({ type: 'toggle-theme' }),
      toggleMotion: () => dispatch({ type: 'toggle-motion' }),
      setPalette: (open) => dispatch({ type: 'set-palette', open }),
      setNotifOpen: (open) => dispatch({ type: 'set-notif-open', open }),
      setShortcutsOpen: (open) => dispatch({ type: 'set-shortcuts-open', open }),
    }),
    [],
  );

  const value = useMemo(
    () => ({ state, actions, resolvedTheme, reducedMotion }),
    [state, actions, resolvedTheme, reducedMotion],
  );

  return <ShellContext.Provider value={value}>{children}</ShellContext.Provider>;
}

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useReducer(
    (_: boolean, next: boolean) => next,
    false,
    () => typeof window !== 'undefined' && window.matchMedia(query).matches,
  );
  useEffect(() => {
    const mql = window.matchMedia(query);
    const onChange = () => setMatches(mql.matches);
    onChange();
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [query]);
  return matches;
}

export function useShell(): ShellContextValue {
  const ctx = useContext(ShellContext);
  if (!ctx) throw new Error('useShell must be used inside ShellProvider');
  return ctx;
}

export function useIsNarrow(): boolean {
  return useMediaQuery('(max-width: 700px)');
}

export function useNow(intervalMs: number): number {
  const [now, setNow] = useReducer((_: number, next: number) => next, Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
