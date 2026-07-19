// SPDX-License-Identifier: AGPL-3.0-only
import type { ComponentType, SVGProps } from 'react';
import { IconFolder, IconGear, IconHome, IconList, IconTerminal } from '../shell/icons';

export type AppId = 'home' | 'services' | 'files' | 'terminal' | 'logs';

export interface AppMeta {
  id: AppId;
  title: string;
  icon: ComponentType<SVGProps<SVGSVGElement> & { size?: number }>;
  defaultSize: { w: number; h: number };
  minSize: { w: number; h: number };
}

export const APP_ORDER: AppId[] = ['home', 'services', 'files', 'terminal', 'logs'];

export const APPS: Record<AppId, AppMeta> = {
  home: {
    id: 'home',
    title: 'Home',
    icon: IconHome,
    defaultSize: { w: 700, h: 520 },
    minSize: { w: 420, h: 380 },
  },
  services: {
    id: 'services',
    title: 'Services',
    icon: IconGear,
    defaultSize: { w: 760, h: 520 },
    minSize: { w: 480, h: 360 },
  },
  files: {
    id: 'files',
    title: 'Files',
    icon: IconFolder,
    defaultSize: { w: 680, h: 480 },
    minSize: { w: 440, h: 340 },
  },
  terminal: {
    id: 'terminal',
    title: 'Terminal',
    icon: IconTerminal,
    defaultSize: { w: 640, h: 420 },
    minSize: { w: 380, h: 260 },
  },
  logs: {
    id: 'logs',
    title: 'Logs',
    icon: IconList,
    defaultSize: { w: 720, h: 500 },
    minSize: { w: 460, h: 340 },
  },
};
