// SPDX-License-Identifier: AGPL-3.0-only
import type { ComponentType } from 'react';
import { Files } from './Files';
import { Home } from './Home';
import { Logs } from './Logs';
import type { AppId } from './registry';
import { Services } from './Services';
import { Terminal } from './Terminal';

export const APP_COMPONENTS: Record<AppId, ComponentType> = {
  home: Home,
  services: Services,
  files: Files,
  terminal: Terminal,
  logs: Logs,
};
