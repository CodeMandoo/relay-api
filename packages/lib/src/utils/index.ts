export const sleep = (ms: number) => new Promise<void>((res) => setTimeout(res, ms));

export const formatNumber = (n: number): string => {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toString();
};

export const formatNumberFull = (n: number): string => n.toLocaleString('en-US');

export const formatCurrency = (n: number): string =>
  n.toLocaleString('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });

export const formatPercent = (n: number, digits = 0): string => `${n.toFixed(digits)}%`;

export const formatDateTime = (iso: string): string => {
  const d = new Date(iso);
  return d.toLocaleString('zh-CN', { hour12: false });
};

export const formatDate = (iso: string): string => {
  const d = new Date(iso);
  return d.toLocaleDateString('zh-CN');
};

export const formatRelative = (iso: string): string => {
  const d = new Date(iso).getTime();
  const now = Date.now();
  const diffSec = Math.floor((now - d) / 1000);
  if (diffSec < 60) return `${diffSec} 秒前`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} 小时前`;
  if (diffSec < 604800) return `${Math.floor(diffSec / 86400)} 天前`;
  return formatDate(iso);
};

export const formatTimeRemaining = (iso?: string): string => {
  if (!iso) return '--';
  const d = new Date(iso).getTime();
  const now = Date.now();
  const diffSec = Math.floor((d - now) / 1000);
  if (diffSec <= 0) return '即将刷新';
  if (diffSec < 60) return `${diffSec}秒`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}分`;
  if (diffSec < 86400) {
    const h = Math.floor(diffSec / 3600);
    const m = Math.floor((diffSec % 3600) / 60);
    return `${h}时${m}分`;
  }
  const d_days = Math.floor(diffSec / 86400);
  const d_hours = Math.floor((diffSec % 86400) / 3600);
  return `${d_days}天${d_hours}时`;
};

export const maskKey = (key: string): string => {
  if (key.length <= 12) return key;
  return `${key.slice(0, 7)}${'•'.repeat(20)}${key.slice(-4)}`;
};

export const randomId = (): string => Math.random().toString(36).slice(2, 10);

export const generateApiKey = (): string => {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let out = 'sk-';
  for (let i = 0; i < 48; i++) out += chars[Math.floor(Math.random() * chars.length)];
  return out;
};

export const generateInviteCode = (): string => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  let out = '';
  for (let i = 0; i < 10; i++) {
    if (i === 4 || i === 7) out += '-';
    out += chars[Math.floor(Math.random() * chars.length)];
  }
  return out;
};

export const copyToClipboard = async (text: string): Promise<boolean> => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
    const el = document.createElement('textarea');
    el.value = text;
    el.style.position = 'fixed';
    el.style.opacity = '0';
    document.body.appendChild(el);
    el.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(el);
    return ok;
  } catch {
    return false;
  }
};
