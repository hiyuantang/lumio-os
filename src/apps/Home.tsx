// SPDX-License-Identifier: AGPL-3.0-only
import { useEffect, useState } from 'react';
import { getOverview, uptimeSeconds, type SystemOverview } from '../mock/system';
import { useNow } from '../shell/ShellContext';
import '../styles/apps.css';
import '../styles/home.css';

function formatUptime(totalSeconds: number): string {
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${days}d ${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
}

function Sparkline({ values }: { values: number[] }) {
  const w = 220;
  const h = 36;
  const max = Math.max(100, ...values);
  const points = values
    .map((v, i) => `${((i / Math.max(1, values.length - 1)) * w).toFixed(1)},${(h - (v / max) * (h - 3)).toFixed(1)}`)
    .join(' ');
  return (
    <svg className="home-sparkline" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <polyline points={points} fill="none" stroke="var(--accent)" strokeWidth="1.6" strokeLinejoin="round" />
    </svg>
  );
}

export function Home() {
  const [overview, setOverview] = useState<SystemOverview>(() => getOverview());
  const now = useNow(1000);
  void now;

  useEffect(() => {
    const id = window.setInterval(() => setOverview(getOverview()), 2000);
    return () => window.clearInterval(id);
  }, []);

  const memPct = Math.round((overview.memoryUsedMb / overview.memoryTotalMb) * 100);
  const diskPct = Math.round((overview.storageUsedGb / overview.storageTotalGb) * 100);

  return (
    <div className="app home" data-testid="app-home">
      <div className="home-grid">
        <section className="home-card home-identity" aria-label="System">
          <h2>{overview.hostname}</h2>
          <dl>
            <div>
              <dt>OS</dt>
              <dd>{overview.os}</dd>
            </div>
            <div>
              <dt>Kernel</dt>
              <dd className="mono">{overview.kernel}</dd>
            </div>
            <div>
              <dt>Uptime</dt>
              <dd className="mono">{formatUptime(uptimeSeconds())}</dd>
            </div>
          </dl>
        </section>

        <section className="home-card" aria-label="CPU">
          <h3>CPU</h3>
          <p className="home-big">{overview.cpuPercent}%</p>
          <Sparkline values={overview.cpuHistory} />
        </section>

        <section className="home-card" aria-label="Memory">
          <h3>Memory</h3>
          <p className="home-big">{memPct}%</p>
          <div className="meter" role="meter" aria-valuenow={memPct} aria-valuemin={0} aria-valuemax={100} aria-label="Memory usage">
            <div className="meter-fill" style={{ width: `${memPct}%` }} />
          </div>
          <p className="home-muted">
            {(overview.memoryUsedMb / 1024).toFixed(1)} of {(overview.memoryTotalMb / 1024).toFixed(0)} GB
          </p>
        </section>

        <section className="home-card" aria-label="Storage">
          <h3>Storage</h3>
          <p className="home-big">{diskPct}%</p>
          <div className="meter" role="meter" aria-valuenow={diskPct} aria-valuemin={0} aria-valuemax={100} aria-label="Storage usage">
            <div className="meter-fill" style={{ width: `${diskPct}%` }} />
          </div>
          <p className="home-muted">
            {overview.storageUsedGb} of {overview.storageTotalGb} GB
          </p>
        </section>

        <section className="home-card" aria-label="Updates">
          <h3>Updates</h3>
          <p className="home-big">{overview.pendingUpdates}</p>
          <p className="home-muted">{overview.securityUpdates} security</p>
        </section>

        <section className="home-card home-alerts" aria-label="Alerts">
          <h3>Alerts</h3>
          <ul>
            {overview.alerts.map((alert) => (
              <li key={alert.id} className={`home-alert level-${alert.level}`}>
                <span className="home-alert-dot" aria-hidden="true" />
                {alert.text}
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  );
}
