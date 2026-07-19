// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { getDataSource, type LoadSample } from '../api/source';
import { APPS } from '../apps/registry';
import { IconBell, IconChip, IconNetwork, IconUser } from './icons';
import { MENUBAR_H, useNow, useShell } from './ShellContext';
import '../styles/menubar.css';

type MenuId = 'file' | 'view' | 'user';

interface MenuItemDef {
  id: string;
  label: string;
  hint?: string;
  disabled?: boolean;
  danger?: boolean;
  separatorAbove?: boolean;
  run: () => void;
}

export function MenuBar() {
  const { state, actions, resolvedTheme, reducedMotion } = useShell();
  const source = getDataSource();
  const [openMenu, setOpenMenu] = useState<MenuId | null>(null);
  const [load, setLoad] = useState<LoadSample>(() => source.sampleLoad());
  const [hostname, setHostname] = useState<string | null>(null);
  const now = useNow(1000);
  const barRef = useRef<HTMLDivElement>(null);

  useEffect(() => source.subscribeMetrics(setLoad), [source]);

  useEffect(() => {
    let alive = true;
    source
      .getIdentity()
      .then((identity) => {
        if (alive) setHostname(identity.hostname);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [source]);

  const focusedTitle = state.focused ? APPS[state.focused].title : null;

  const menus: Record<MenuId, { label: string; items: MenuItemDef[] }> = {
    file: {
      label: 'File',
      items: [
        {
          id: 'command-center',
          label: 'Command Center',
          hint: '⌘K',
          run: () => actions.setPalette(true),
        },
        {
          id: 'close-window',
          label: 'Close Window',
          hint: '⌥W',
          disabled: !state.focused,
          run: () => state.focused && actions.closeApp(state.focused),
        },
      ],
    },
    view: {
      label: 'View',
      items: [
        {
          id: 'toggle-theme',
          label: resolvedTheme === 'dark' ? 'Switch to Light Theme' : 'Switch to Dark Theme',
          run: actions.toggleTheme,
        },
        {
          id: 'toggle-motion',
          label: reducedMotion ? 'Allow Full Motion' : 'Reduce Motion',
          run: actions.toggleMotion,
        },
        {
          id: 'shortcuts',
          label: 'Keyboard Shortcuts',
          run: () => actions.setShortcutsOpen(true),
        },
      ],
    },
    user: {
      label: state.user ?? 'user',
      items: [
        {
          id: 'user-shortcuts',
          label: 'Keyboard Shortcuts',
          run: () => actions.setShortcutsOpen(true),
        },
        {
          id: 'user-theme',
          label: resolvedTheme === 'dark' ? 'Switch to Light Theme' : 'Switch to Dark Theme',
          run: actions.toggleTheme,
        },
        {
          id: 'user-motion',
          label: reducedMotion ? 'Allow Full Motion' : 'Reduce Motion',
          run: actions.toggleMotion,
        },
        {
          id: 'logout',
          label: 'Log Out',
          danger: true,
          separatorAbove: true,
          run: actions.logout,
        },
      ],
    },
  };

  function focusTopButton(menuId: MenuId) {
    const btn = barRef.current?.querySelector<HTMLButtonElement>(`[data-menu-button="${menuId}"]`);
    btn?.focus();
  }

  function siblingMenu(menuId: MenuId, dir: 1 | -1): MenuId {
    const order: MenuId[] = ['file', 'view', 'user'];
    const idx = order.indexOf(menuId);
    return order[(idx + dir + order.length) % order.length];
  }

  function onMenuButtonKey(e: ReactKeyboardEvent, menuId: MenuId) {
    if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      setOpenMenu(menuId);
      requestAnimationFrame(() => {
        barRef.current
          ?.querySelector<HTMLButtonElement>(`[data-menu="${menuId}"] [data-menu-item]:not([disabled])`)
          ?.focus();
      });
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault();
      const next = siblingMenu(menuId, e.key === 'ArrowRight' ? 1 : -1);
      if (openMenu) setOpenMenu(next);
      focusTopButton(next);
    }
  }

  function onMenuListKey(e: ReactKeyboardEvent, menuId: MenuId) {
    const items = Array.from(
      barRef.current?.querySelectorAll<HTMLButtonElement>(`[data-menu="${menuId}"] [data-menu-item]`) ?? [],
    ).filter((el) => !el.disabled);
    const idx = items.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'Escape') {
      e.preventDefault();
      setOpenMenu(null);
      focusTopButton(menuId);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      items[(idx + 1 + items.length) % items.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      items[(idx - 1 + items.length) % items.length]?.focus();
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault();
      const next = siblingMenu(menuId, e.key === 'ArrowRight' ? 1 : -1);
      setOpenMenu(next);
      requestAnimationFrame(() => focusTopButton(next));
    } else if (e.key === 'Tab') {
      setOpenMenu(null);
    }
  }

  function renderMenu(menuId: MenuId) {
    const menu = menus[menuId];
    const isOpen = openMenu === menuId;
    return (
      <div className="menubar-menu" key={menuId}>
        <button
          type="button"
          data-menu-button={menuId}
          className="menubar-button"
          role="menuitem"
          aria-haspopup="menu"
          aria-expanded={isOpen}
          onClick={() => setOpenMenu(isOpen ? null : menuId)}
          onKeyDown={(e) => onMenuButtonKey(e, menuId)}
          onMouseEnter={() => {
            if (openMenu && openMenu !== menuId) setOpenMenu(menuId);
          }}
        >
          {menuId === 'user' && <IconUser size={14} />}
          {menu.label}
        </button>
        {isOpen && (
          <div className="menubar-dropdown" role="menu" data-menu={menuId} onKeyDown={(e) => onMenuListKey(e, menuId)}>
            {menu.items.map((item) => (
              <button
                key={item.id}
                type="button"
                role="menuitem"
                data-menu-item
                data-testid={item.id === 'logout' ? 'logout-button' : undefined}
                className={`menubar-item${item.danger ? ' danger' : ''}${item.separatorAbove ? ' separator-above' : ''}`}
                disabled={item.disabled}
                onClick={() => {
                  setOpenMenu(null);
                  item.run();
                }}
              >
                <span>{item.label}</span>
                {item.hint && <kbd>{item.hint}</kbd>}
              </button>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <header className="menubar" data-testid="menu-bar" style={{ height: MENUBAR_H }}>
      {openMenu && <div className="menubar-backdrop" onClick={() => setOpenMenu(null)} />}
      <div className="menubar-left">
        <span className="menubar-brand">Lumio OS</span>
        {hostname && <span className="menubar-host">{hostname}</span>}
        <nav className="menubar-menus" role="menubar" aria-label="Application menus">
          {renderMenu('file')}
          {renderMenu('view')}
        </nav>
        {focusedTitle && <span className="menubar-focused">{focusedTitle}</span>}
      </div>
      <div className="menubar-right">
        <span className="menubar-indicator" title={source.capabilities.isLive ? 'CPU load' : 'CPU load (mock)'}>
          <IconChip size={13} />
          {load.cpuPercent}%
        </span>
        <span className="menubar-indicator" title={source.capabilities.isLive ? 'Network throughput' : 'Network throughput (mock)'}>
          <IconNetwork size={13} />
          {load.netDownKbps >= 1024 ? `${(load.netDownKbps / 1024).toFixed(1)} MB/s` : `${load.netDownKbps} KB/s`}
        </span>
        <time className="menubar-clock" dateTime={new Date(now).toISOString()}>
          {new Date(now).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })}{' '}
          {new Date(now).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
        </time>
        <button
          type="button"
          className="menubar-icon-button"
          data-testid="notifications-button"
          aria-label={state.unread > 0 ? `Notifications, ${state.unread} unread` : 'Notifications'}
          aria-expanded={state.notifOpen}
          onClick={() => actions.setNotifOpen(!state.notifOpen)}
        >
          <IconBell size={15} />
          {state.unread > 0 && (
            <span className="menubar-badge" data-testid="notifications-badge">
              {state.unread}
            </span>
          )}
        </button>
        <nav className="menubar-menus" role="menubar" aria-label="User menu">
          {renderMenu('user')}
        </nav>
      </div>
    </header>
  );
}
