// SPDX-License-Identifier: AGPL-3.0-only
import type { ServerFrame } from './protocol';
import { API_BASE, ApiError, csrfToken } from './transport';

export interface ChannelSubscription {
  capability: string;
  params: () => Record<string, unknown>;
  onEvent: (data: unknown, seq: number) => void;
  onError?: (err: ApiError) => void;
}

function socketUrl(): string {
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
  const csrf = csrfToken();
  const suffix = csrf ? `?csrf=${encodeURIComponent(csrf)}` : '';
  return `${scheme}://${window.location.host}${API_BASE}/ws${suffix}`;
}

export class LumioSocket {
  private ws: WebSocket | null = null;
  private open = false;
  private nextChannel = 1;
  private attempts = 0;
  private reconnectTimer: number | null = null;
  private channels = new Map<number, ChannelSubscription>();
  private lastSeq = new Map<number, number>();

  subscribe(subscription: ChannelSubscription): () => void {
    const channel = this.nextChannel++;
    this.channels.set(channel, subscription);
    if (this.open) {
      this.sendSubscribe(channel, subscription);
    } else {
      this.ensureConnected();
    }
    return () => {
      const known = this.channels.delete(channel);
      this.lastSeq.delete(channel);
      if (known && this.open) this.send({ type: 'unsubscribe', channel });
      if (this.channels.size === 0) this.closeSocket();
    };
  }

  private ensureConnected() {
    if (this.ws || this.channels.size === 0) return;
    const ws = new WebSocket(socketUrl());
    this.ws = ws;
    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.open = true;
      this.attempts = 0;
      for (const [channel, subscription] of this.channels) this.sendSubscribe(channel, subscription);
    };
    ws.onmessage = (event) => this.handleMessage(event.data as string);
    ws.onerror = () => {
      ws.close();
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.open = false;
      this.lastSeq.clear();
      if (this.channels.size > 0) this.scheduleReconnect();
    };
  }

  private closeSocket() {
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const ws = this.ws;
    this.ws = null;
    this.open = false;
    ws?.close();
  }

  private scheduleReconnect() {
    if (this.reconnectTimer !== null) return;
    const delayMs = Math.min(8000, 500 * 2 ** this.attempts);
    this.attempts += 1;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.ensureConnected();
    }, delayMs);
  }

  private sendSubscribe(channel: number, subscription: ChannelSubscription) {
    this.send({ type: 'subscribe', channel, capability: subscription.capability, params: subscription.params() });
  }

  private send(frame: Record<string, unknown>) {
    if (this.open && this.ws) this.ws.send(JSON.stringify(frame));
  }

  private handleMessage(raw: string) {
    let frame: ServerFrame;
    try {
      frame = JSON.parse(raw) as ServerFrame;
    } catch {
      return;
    }
    switch (frame.type) {
      case 'hello':
        return;
      case 'ping':
        this.send({ type: 'pong' });
        return;
      case 'subscribed':
        return;
      case 'event':
        this.handleEvent(frame.channel, frame.seq, frame.data);
        return;
      case 'error': {
        const subscription = this.channels.get(frame.channel);
        subscription?.onError?.(new ApiError(frame.error.code, frame.error.message, frame.error.details ?? {}));
        return;
      }
      case 'closed': {
        const subscription = this.channels.get(frame.channel);
        this.channels.delete(frame.channel);
        this.lastSeq.delete(frame.channel);
        if (frame.error) {
          subscription?.onError?.(new ApiError(frame.error.code, frame.error.message, frame.error.details ?? {}));
        }
        if (this.channels.size === 0) this.closeSocket();
        return;
      }
      default:
        return;
    }
  }

  private handleEvent(channel: number, seq: number, data: unknown) {
    const subscription = this.channels.get(channel);
    if (!subscription) return;
    const last = this.lastSeq.get(channel) ?? 0;
    this.lastSeq.set(channel, seq);
    if (last !== 0 && seq > last + 1) {
      this.lastSeq.delete(channel);
      this.send({ type: 'unsubscribe', channel });
      this.sendSubscribe(channel, subscription);
      return;
    }
    subscription.onEvent(data, seq);
  }
}
