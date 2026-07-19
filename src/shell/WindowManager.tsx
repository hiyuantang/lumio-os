// SPDX-License-Identifier: AGPL-3.0-only
import { useShell, type WindowState } from './ShellContext';
import { Window } from './Window';

export function WindowManager() {
  const { state } = useShell();
  const windows = Object.values(state.windows).filter((w): w is WindowState => Boolean(w));
  return (
    <>
      {windows.map((win) => (
        <Window key={win.appId} win={win} />
      ))}
    </>
  );
}
