import type {
  ApiKey,
  AuthUser,
  InviteCode,
  PlatformModel,
  PlatformSettings,
  PlatformUser,
  RequestAttemptLog,
  RequestLog,
  RequestLogPagination,
  SourceStatus,
  SourceAccount,
  SourceAccountTokenUsage,
  SourceKey,
  UpstreamSource,
  UsageDetailRow,
  UsageStats,
  UserModel,
  UserModelInvokeTestResult,
  UserQuota,
} from '@relay-api/lib';

const API_BASE = import.meta.env.VITE_API_BASE ?? '/api';
const TOKEN_STORAGE_KEY = 'relay.auth.tokens';

export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
}

export interface AuthResponse extends AuthTokens {
  user: AuthUser;
}

export interface EmailCodeResponse {
  email: string;
  expiresIn: number;
  cooldownSeconds: number;
  sent: boolean;
  devCode?: string;
}

export interface DashboardMetrics {
  todayRequests: number;
  todayRequestsChangePct: number;
  activeUsers: number;
  activeUsersChange: number;
  upstreamOnline: number;
  upstreamTotal: number;
  monthlySpend: number;
  monthlySpendPct: number;
  trendChangePct: number;
  trend7d: { day: string; value: number; isToday?: boolean }[];
  upstreamStatuses: { name: string; status: SourceStatus; load: number; latencyMs: number; pulsing?: boolean }[];
}

export interface UserDashboardResponse {
  quota: UserQuota;
  usage: UsageStats;
}

export interface UserUsageResponse {
  stats: UsageStats;
  rows: UsageDetailRow[];
}

type ApiEnvelope<T> = { data: T };
type PaginatedApiEnvelope<T> = ApiEnvelope<T> & { pagination: RequestLogPagination };

export function saveAuthTokens(tokens: AuthTokens) {
  localStorage.setItem(TOKEN_STORAGE_KEY, JSON.stringify(tokens));
}

export function readAuthTokens(): AuthTokens | null {
  const raw = localStorage.getItem(TOKEN_STORAGE_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as AuthTokens;
  } catch {
    return null;
  }
}

export function clearAuthTokens() {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
}

export function getErrorMessage(error: unknown, fallback = '请求失败') {
  return error instanceof Error && error.message ? error.message : fallback;
}

export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json');
  }
  const token = readAuthTokens()?.accessToken;
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  const response = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `Request failed: ${response.status}`);
  }
  return response.json() as Promise<T>;
}

const jsonBody = (value: unknown) => JSON.stringify(value);

export const authApi = {
  settings: () =>
    apiRequest<
      ApiEnvelope<
        Pick<
          PlatformSettings,
          'platformName' | 'supportEmail' | 'openRegistration' | 'requireInviteCode' | 'requireEmailVerification'
        >
      >
    >('/auth/settings'),
  async login(payload: { email: string; password: string; role?: 'admin' | 'user' }) {
    const response = await apiRequest<AuthResponse>('/auth/login', {
      method: 'POST',
      body: jsonBody(payload),
    });
    saveAuthTokens(response);
    return response;
  },
  sendRegisterEmailCode: (payload: { email: string }) =>
    apiRequest<ApiEnvelope<EmailCodeResponse>>('/auth/register/email-code', {
      method: 'POST',
      body: jsonBody(payload),
    }),
  async register(payload: { email: string; password: string; name?: string; inviteCode: string; emailCode?: string }) {
    const response = await apiRequest<AuthResponse>('/auth/register', {
      method: 'POST',
      body: jsonBody(payload),
    });
    saveAuthTokens(response);
    return response;
  },
  me: () => apiRequest<{ user: AuthUser }>('/auth/me'),
  async refresh(refreshToken: string) {
    const response = await apiRequest<AuthResponse>('/auth/refresh', {
      method: 'POST',
      body: jsonBody({ refreshToken }),
    });
    saveAuthTokens(response);
    return response;
  },
};

export const adminApi = {
  dashboard: () => apiRequest<ApiEnvelope<DashboardMetrics>>('/admin/dashboard'),
  users: () => apiRequest<ApiEnvelope<PlatformUser[]>>('/admin/users'),
  createUser: (payload: Partial<PlatformUser> & { password: string }) =>
    apiRequest<ApiEnvelope<PlatformUser>>('/admin/users', { method: 'POST', body: jsonBody(payload) }),
  updateUser: (id: string, payload: Partial<PlatformUser> & { password?: string }) =>
    apiRequest<ApiEnvelope<PlatformUser>>(`/admin/users/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  updateUserQuota: (id: string, payload: Pick<PlatformUser, 'monthlyQuota' | 'weeklyQuota' | 'balance'>) =>
    apiRequest<{ ok: boolean }>(`/admin/users/${id}/quota`, { method: 'PUT', body: jsonBody(payload) }),
  deleteUser: (id: string) => apiRequest<{ ok: boolean }>(`/admin/users/${id}`, { method: 'DELETE' }),
  sources: () => apiRequest<ApiEnvelope<UpstreamSource[]>>('/admin/sources'),
  createSource: (payload: Partial<UpstreamSource>) =>
    apiRequest<ApiEnvelope<UpstreamSource>>('/admin/sources', { method: 'POST', body: jsonBody(payload) }),
  updateSource: (id: string, payload: Partial<UpstreamSource>) =>
    apiRequest<ApiEnvelope<UpstreamSource>>(`/admin/sources/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteSource: (id: string) => apiRequest<{ ok: boolean }>(`/admin/sources/${id}`, { method: 'DELETE' }),
  checkSource: (id: string) => apiRequest<ApiEnvelope<UpstreamSource> & { error?: string }>(`/admin/sources/${id}/check`, { method: 'POST' }),
  recoverSource: (id: string) => apiRequest<ApiEnvelope<UpstreamSource>>(`/admin/sources/${id}/recover`, { method: 'POST' }),
  sourceAccounts: (sourceId: string) => apiRequest<ApiEnvelope<SourceAccount[]>>(`/admin/sources/${sourceId}/accounts`),
  createSourceAccount: (sourceId: string, payload: Partial<SourceAccount>) =>
    apiRequest<ApiEnvelope<SourceAccount>>(`/admin/sources/${sourceId}/accounts`, { method: 'POST', body: jsonBody(payload) }),
  syncSourceAccounts: (sourceId: string) =>
    apiRequest<ApiEnvelope<SourceAccount[]>>(`/admin/sources/${sourceId}/accounts/sync`, { method: 'POST' }),
  startSourceOAuth: (sourceId: string, provider: string) =>
    apiRequest<{ authUrl?: string; sessionId?: string; statusUrl?: string }>(`/admin/sources/${sourceId}/accounts/oauth`, {
      method: 'POST',
      body: jsonBody({ provider }),
    }),
  submitSourceOAuthCallback: (sourceId: string, provider: string, redirectUrl: string) =>
    apiRequest<ApiEnvelope<SourceAccount[]> & { ok: boolean; pending?: boolean }>(`/admin/sources/${sourceId}/accounts/oauth/callback`, {
      method: 'POST',
      body: jsonBody({ provider, redirectUrl }),
    }),
  updateSourceAccount: (id: string, payload: Partial<SourceAccount>) =>
    apiRequest<ApiEnvelope<SourceAccount>>(`/admin/source-accounts/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteSourceAccount: (id: string) => apiRequest<{ ok: boolean }>(`/admin/source-accounts/${id}`, { method: 'DELETE' }),
  refreshSourceAccount: (id: string) =>
    apiRequest<ApiEnvelope<SourceAccount>>(`/admin/source-accounts/${id}/refresh`, { method: 'POST' }),
  sourceAccountTokenUsage: (id: string) =>
    apiRequest<ApiEnvelope<SourceAccountTokenUsage>>(`/admin/source-accounts/${id}/token-usage`),
  sourceKeys: (sourceId: string) => apiRequest<ApiEnvelope<SourceKey[]>>(`/admin/sources/${sourceId}/keys`),
  createSourceKey: (sourceId: string, payload: Pick<SourceKey, 'alias'> & { key: string; status?: SourceKey['status'] }) =>
    apiRequest<ApiEnvelope<SourceKey>>(`/admin/sources/${sourceId}/keys`, { method: 'POST', body: jsonBody(payload) }),
  updateSourceKey: (id: string, payload: Partial<Pick<SourceKey, 'alias' | 'key' | 'status'>>) =>
    apiRequest<ApiEnvelope<SourceKey>>(`/admin/source-keys/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteSourceKey: (id: string) => apiRequest<{ ok: boolean }>(`/admin/source-keys/${id}`, { method: 'DELETE' }),
  models: () => apiRequest<ApiEnvelope<PlatformModel[]>>('/admin/models'),
  createModel: (payload: Partial<PlatformModel>) =>
    apiRequest<ApiEnvelope<PlatformModel>>('/admin/models', { method: 'POST', body: jsonBody(payload) }),
  updateModel: (id: string, payload: Partial<PlatformModel>) =>
    apiRequest<ApiEnvelope<PlatformModel>>(`/admin/models/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteModel: (id: string) => apiRequest<{ ok: boolean }>(`/admin/models/${id}`, { method: 'DELETE' }),
  batchModels: (ids: string[], action: 'enable' | 'disable' | 'delete') =>
    apiRequest<{ ok: boolean }>('/admin/models/batch', { method: 'POST', body: jsonBody({ ids, action }) }),
  syncPricing: () =>
    apiRequest<{ ok: boolean; result: { synced: number; skipped: number; errors?: string[] } }>('/admin/models/sync-pricing', { method: 'POST' }),
  pricingStatus: () =>
    apiRequest<{ cached: boolean; modelCount: number; lastSync: string; ttlSeconds: number }>('/admin/pricing/status'),
  invites: () => apiRequest<ApiEnvelope<InviteCode[]>>('/admin/invite-codes'),
  createInvite: (payload: { code?: string; limit: number; expiresAt?: string; remark?: string }) =>
    apiRequest<ApiEnvelope<InviteCode> & { link?: string }>('/admin/invite-codes', { method: 'POST', body: jsonBody(payload) }),
  updateInvite: (id: string, payload: Partial<InviteCode>) =>
    apiRequest<ApiEnvelope<InviteCode>>(`/admin/invite-codes/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteInvite: (id: string) => apiRequest<{ ok: boolean }>(`/admin/invite-codes/${id}`, { method: 'DELETE' }),
  logs: (params: { model?: string; status?: string; q?: string; page?: number; pageSize?: number; from?: string; to?: string } = {}) => {
    const search = new URLSearchParams();
    if (params.model && params.model !== 'all') search.set('model', params.model);
    if (params.status && params.status !== 'all') search.set('status', params.status);
    if (params.q?.trim()) search.set('q', params.q.trim());
    if (params.page) search.set('page', String(params.page));
    if (params.pageSize) search.set('pageSize', String(params.pageSize));
    if (params.from) search.set('from', params.from);
    if (params.to) search.set('to', params.to);
    const suffix = search.size ? `?${search.toString()}` : '';
    return apiRequest<PaginatedApiEnvelope<RequestLog[]>>(`/admin/logs${suffix}`);
  },
  logAttempts: (id: string) => apiRequest<ApiEnvelope<RequestAttemptLog[]>>(`/admin/logs/${id}/attempts`),
  clearLogs: () => apiRequest<{ ok: boolean }>('/admin/logs', { method: 'DELETE' }),
  usageStats: (range: 'day' | 'week' | 'month' = 'week') =>
    apiRequest<ApiEnvelope<UsageStats>>(`/admin/usage/stats?range=${range}`),
  resetUsage: () => apiRequest<{ ok: boolean }>('/admin/usage/reset', { method: 'POST' }),
  settings: () => apiRequest<ApiEnvelope<PlatformSettings>>('/admin/settings'),
  updateSettings: (payload: Partial<PlatformSettings>) =>
    apiRequest<ApiEnvelope<PlatformSettings>>('/admin/settings', { method: 'PUT', body: jsonBody(payload) }),
};

export const userApi = {
  dashboard: () => apiRequest<ApiEnvelope<UserDashboardResponse>>('/user/dashboard'),
  usage: (range: 'day' | 'week' | 'month', apiKeyId = 'all') =>
    apiRequest<ApiEnvelope<UserUsageResponse>>(`/user/usage?range=${range}&apiKeyId=${apiKeyId}`),
  models: () => apiRequest<ApiEnvelope<UserModel[]>>('/user/models'),
  testModel: (id: string) => apiRequest<ApiEnvelope<{ latencyMs: number }>>(`/user/models/${id}/test`, { method: 'POST' }),
  invokeTestModel: (id: string) =>
    apiRequest<ApiEnvelope<UserModelInvokeTestResult>>(`/user/models/${id}/invoke-test`, { method: 'POST' }),
  apiKeys: () => apiRequest<ApiEnvelope<ApiKey[]>>('/user/api-keys'),
  createApiKey: (payload: { name: string; limit?: number }) =>
    apiRequest<ApiEnvelope<ApiKey>>('/user/api-keys', { method: 'POST', body: jsonBody(payload) }),
  updateApiKey: (id: string, payload: Partial<ApiKey> & { enabled?: boolean }) =>
    apiRequest<ApiEnvelope<ApiKey>>(`/user/api-keys/${id}`, { method: 'PUT', body: jsonBody(payload) }),
  deleteApiKey: (id: string) => apiRequest<{ ok: boolean }>(`/user/api-keys/${id}`, { method: 'DELETE' }),
  revealApiKey: (id: string) =>
    apiRequest<ApiEnvelope<ApiKey>>(`/user/api-keys/${id}/reveal`, { method: 'POST' }),
};
