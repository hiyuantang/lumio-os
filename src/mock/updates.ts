// SPDX-License-Identifier: AGPL-3.0-only
import type { Unsubscribe, UpdatePlan, UpdateProgress } from '../api/source';

const progressByRequest = new Map<string, UpdateProgress>();
const listeners = new Map<string, Set<(progress: UpdateProgress) => void>>();

export async function refreshUpdates(): Promise<string> {
  await delay(500);
  return new Date().toISOString();
}

export async function calculateUpdatePlan(): Promise<UpdatePlan> {
  await delay(400);
  const now = Date.now();
  return {
    id: `pln_${crypto.randomUUID().replaceAll('-', '').slice(0, 24)}`,
    createdAt: new Date(now).toISOString(),
    expiresAt: new Date(now + 15 * 60_000).toISOString(),
    packages: [
      {
        name: 'openssl',
        fromVersion: '3.0.13-0ubuntu3.4',
        toVersion: '3.0.13-0ubuntu3.5',
        security: true,
        downloadBytes: 1015808,
        installedDeltaBytes: 0,
      },
      {
        name: 'systemd',
        fromVersion: '255.4-1ubuntu8.8',
        toVersion: '255.4-1ubuntu8.9',
        security: false,
        downloadBytes: 3977216,
        installedDeltaBytes: 32768,
      },
    ],
    securityCount: 1,
    downloadBytes: 4993024,
    installedDeltaBytes: 32768,
    rebootRequired: false,
  };
}

export async function applyUpdatePlan(planId: string): Promise<string> {
  const requestId = crypto.randomUUID();
  const progress: UpdateProgress = {
    requestId,
    planId,
    phase: 'queued',
    percent: 0,
    message: 'Waiting for the package manager',
    done: false,
    success: false,
    updatedAt: new Date().toISOString(),
  };
  progressByRequest.set(requestId, progress);
  let step = 0;
  const stages = [
    { phase: 'downloading', percent: 18, message: 'Downloading packages' },
    { phase: 'installing', percent: 58, message: 'Installing openssl' },
    { phase: 'installing', percent: 86, message: 'Installing systemd' },
    { phase: 'complete', percent: 100, message: 'Updates installed' },
  ];
  const timer = window.setInterval(() => {
    const stage = stages[step++];
    if (!stage) {
      window.clearInterval(timer);
      return;
    }
    const next: UpdateProgress = {
      ...progress,
      ...stage,
      done: stage.phase === 'complete',
      success: stage.phase === 'complete',
      updatedAt: new Date().toISOString(),
    };
    progressByRequest.set(requestId, next);
    listeners.get(requestId)?.forEach((listener) => listener(next));
    if (next.done) window.clearInterval(timer);
  }, 450);
  return requestId;
}

export function subscribeUpdateProgress(requestId: string, listener: (progress: UpdateProgress) => void): Unsubscribe {
  const group = listeners.get(requestId) ?? new Set();
  group.add(listener);
  listeners.set(requestId, group);
  const current = progressByRequest.get(requestId);
  if (current) queueMicrotask(() => listener(current));
  return () => {
    group.delete(listener);
    if (group.size === 0) listeners.delete(requestId);
  };
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
