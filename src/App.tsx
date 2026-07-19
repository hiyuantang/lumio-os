// SPDX-License-Identifier: AGPL-3.0-only
import { CommandCenter } from './shell/CommandCenter';
import { Dock } from './shell/Dock';
import { LoginScreen } from './shell/LoginScreen';
import { MenuBar } from './shell/MenuBar';
import { NotificationCenter, ShortcutsDialog } from './shell/NotificationCenter';
import { ShellProvider, useIsNarrow, useShell } from './shell/ShellContext';
import { WindowManager } from './shell/WindowManager';

function Desktop() {
  const narrow = useIsNarrow();
  return (
    <div className={`desktop-root${narrow ? ' narrow' : ''}`}>
      <MenuBar />
      <main className="desktop wallpaper" aria-label="Desktop">
        <WindowManager />
      </main>
      <Dock />
      <CommandCenter />
      <NotificationCenter />
      <ShortcutsDialog />
    </div>
  );
}

function Shell() {
  const { state } = useShell();
  return state.user ? <Desktop /> : <LoginScreen />;
}

export default function App() {
  return (
    <ShellProvider>
      <Shell />
    </ShellProvider>
  );
}
