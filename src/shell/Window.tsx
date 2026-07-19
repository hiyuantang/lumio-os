// SPDX-License-Identifier: AGPL-3.0-only
import { useState, type PointerEvent as ReactPointerEvent } from 'react';
import { APP_COMPONENTS } from '../apps';
import { APPS } from '../apps/registry';
import { IconMinus, IconX, IconZoom } from './icons';
import { useShell, type WindowState } from './ShellContext';
import '../styles/window.css';

const RESIZE_DIRS = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'] as const;
type ResizeDir = (typeof RESIZE_DIRS)[number];

export function Window({ win }: { win: WindowState }) {
  const { state, actions, reducedMotion } = useShell();
  const meta = APPS[win.appId];
  const Body = APP_COMPONENTS[win.appId];
  const focused = state.focused === win.appId;
  const [minimizing, setMinimizing] = useState(false);

  function minimize() {
    if (reducedMotion) {
      actions.minimizeApp(win.appId);
      return;
    }
    setMinimizing(true);
    window.setTimeout(() => {
      actions.minimizeApp(win.appId);
      setMinimizing(false);
    }, 180);
  }

  function onTitlePointerDown(e: ReactPointerEvent<HTMLElement>) {
    if (e.button !== 0 || win.maximized) return;
    if ((e.target as HTMLElement).closest('.window-controls')) return;
    const el = e.currentTarget;
    const startX = e.clientX;
    const startY = e.clientY;
    const orig = { x: win.x, y: win.y };
    el.setPointerCapture(e.pointerId);
    const move = (ev: PointerEvent) => {
      actions.updateRect(win.appId, {
        x: orig.x + ev.clientX - startX,
        y: orig.y + ev.clientY - startY,
        w: win.w,
        h: win.h,
      });
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
  }

  function onResizePointerDown(e: ReactPointerEvent<HTMLDivElement>, dir: ResizeDir) {
    if (e.button !== 0 || win.maximized) return;
    e.preventDefault();
    e.stopPropagation();
    const el = e.currentTarget;
    const startX = e.clientX;
    const startY = e.clientY;
    const orig = { x: win.x, y: win.y, w: win.w, h: win.h };
    el.setPointerCapture(e.pointerId);
    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - startX;
      const dy = ev.clientY - startY;
      let { x, y, w, h } = orig;
      if (dir.includes('e')) w = Math.max(meta.minSize.w, orig.w + dx);
      if (dir.includes('s')) h = Math.max(meta.minSize.h, orig.h + dy);
      if (dir.includes('w')) {
        w = Math.max(meta.minSize.w, orig.w - dx);
        x = orig.x + orig.w - w;
      }
      if (dir.includes('n')) {
        h = Math.max(meta.minSize.h, orig.h - dy);
        y = orig.y + orig.h - h;
      }
      actions.updateRect(win.appId, { x, y, w, h });
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
  }

  const Icon = meta.icon;
  const className = [
    'window',
    focused ? 'focused' : '',
    win.maximized ? 'maximized' : '',
    minimizing ? 'minimizing' : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <section
      className={className}
      role="dialog"
      aria-label={meta.title}
      data-testid={`window-${win.appId}`}
      hidden={win.minimized}
      style={{ left: win.x, top: win.y, width: win.w, height: win.h, zIndex: win.z }}
      onPointerDownCapture={() => {
        if (!focused) actions.focusApp(win.appId);
      }}
    >
      <header
        className="window-titlebar"
        onPointerDown={onTitlePointerDown}
        onDoubleClick={(e) => {
          if (!(e.target as HTMLElement).closest('.window-controls')) actions.toggleMaximize(win.appId);
        }}
      >
        <div className="window-controls">
          <button type="button" className="wc wc-min" aria-label={`Minimize ${meta.title}`} onClick={minimize}>
            <IconMinus size={10} />
          </button>
          <button
            type="button"
            className="wc wc-zoom"
            aria-label={win.maximized ? `Restore ${meta.title}` : `Maximize ${meta.title}`}
            onClick={() => actions.toggleMaximize(win.appId)}
          >
            <IconZoom size={10} />
          </button>
          <button
            type="button"
            className="wc wc-close"
            data-testid={`window-close-${win.appId}`}
            aria-label={`Close ${meta.title}`}
            onClick={() => actions.closeApp(win.appId)}
          >
            <IconX size={10} />
          </button>
        </div>
        <span className="window-title">
          <Icon size={13} />
          {meta.title}
        </span>
      </header>
      <div className="window-body">
        <Body />
      </div>
      {!win.maximized &&
        RESIZE_DIRS.map((dir) => (
          <div key={dir} className={`resize-handle rh-${dir}`} aria-hidden="true" onPointerDown={(e) => onResizePointerDown(e, dir)} />
        ))}
    </section>
  );
}
