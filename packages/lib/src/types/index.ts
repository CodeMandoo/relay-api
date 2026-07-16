export type Role = 'admin' | 'user';

export interface AuthUser {
  id: string;
  email: string;
  name: string;
  role: Role;
  avatarText: string;
}

export type UserStatus = 'normal' | 'disabled';

export interface PlatformUser {
  id: string;
  email: string;
  name: string;
  role: Role;
  status: UserStatus;
  inviteCode?: string;
  registeredAt: string;
  monthlyQuota: number;
  weeklyQuota: number;
  usedThisMonth: number;
  balance: number;
}

export type SourceType = 'CLIProxyAPI' | 'Third-party Provider';
export type SourceStatus = 'online' | 'offline' | 'disabled';
export type ModelFormat = 'openai' | 'anthropic';

export interface UpstreamSource {
  id: string;
  name: string;
  type: SourceType;
  apiBase: string;
  openaiBaseUrl?: string;
  anthropicBaseUrl?: string;
  apiKey?: string;
  maskedKey?: string;
  managementKey?: string;
  hasManagementKey?: boolean;
  accountCount: number;
  priority: number;
  status: SourceStatus;
  load: number;
  latencyMs: number;
  failureCount?: number;
  successCount?: number;
  lastFailureAt?: string;
  lastSuccessAt?: string;
  cooldownUntil?: string;
  coolingDown?: boolean;
  createdAt?: string;
}

export type AccountProvider = 'ChatGPT' | 'Claude' | 'Gemini' | 'Grok';
export type AccountStatus = 'valid' | 'expired' | 'cooldown';

export interface SourceAccount {
  id: string;
  sourceId: string;
  identifier: string;
  provider: AccountProvider;
  authIndex?: string;
  planType?: string;
  subscriptionPlan?: string;
  hasSubscription?: boolean;
  subscriptionExpiresAt?: string;
  subscriptionRenewsAt?: string;
  status: AccountStatus;
  balance: number;
  balanceLimit: number;
  used5h: number;
  limit5h: number;
  used7d: number;
  limit7d: number;
  successCount?: number;
  failedCount?: number;
  recentRequests?: number;
  nextRefresh5h?: string;
  nextRefresh7d?: string;
  lastRefreshed: string;
}

export interface SourceAccountTokenUsage {
  accountId: string;
  dayTokens: number;
  weekTokens: number;
  monthTokens: number;
  totalTokens: number;
  syncedCount: number;
  syncError?: string;
}

export type SourceKeyStatus = 'valid' | 'disabled';

export interface SourceKey {
  id: string;
  sourceId: string;
  alias: string;
  key?: string;
  masked: string;
  status: SourceKeyStatus;
  lastUsedAt?: string;
  createdAt: string;
}

export const MODEL_PROVIDERS = [
  'OpenAI',
  'Anthropic',
  'Google',
  'xAI',
  'DeepSeek',
  'Qwen',
  'Kimi',
  'GLM',
  'MiMo',
  'MiniMax',
  'Doubao',
  'Hunyuan',
  'ERNIE',
  'Baichuan',
  'Yi',
  'StepFun',
  'Mistral',
  'Meta',
  'Cohere',
  'Perplexity',
  'NVIDIA',
] as const;

export type BuiltInModelProvider = (typeof MODEL_PROVIDERS)[number];
export type ModelProvider = BuiltInModelProvider | (string & {});

export interface PlatformModel {
  id: string;
  name: string;
  modelGroupId?: string;
  modelGroupName?: string;
  sourceId: string;
  sourceName: string;
  sourceKeyId?: string;
  sourceKeyAlias?: string;
  provider: ModelProvider;
  formats: ModelFormat[];
  enabled: boolean;
  routingWeight: number;
  routingEnabled: boolean;
  candidateCount?: number;
  routingCandidates?: ModelRouteCandidate[];
  bindings?: ModelBindingInput[];
}

export interface ModelBindingInput {
  id?: string;
  sourceId: string;
  sourceKeyId?: string;
  routingWeight: number;
}

export interface ModelRouteCandidate {
  id: string;
  sourceId: string;
  sourceName: string;
  sourceStatus: SourceStatus;
  sourcePriority: number;
  sourceKeyId?: string;
  sourceKeyAlias?: string;
  routingWeight: number;
  routingEnabled: boolean;
  modelEnabled: boolean;
  coolingDown: boolean;
  cooldownUntil?: string;
  schedulerState?: 'closed' | 'open' | 'half_open' | 'recovering';
}

export type InviteStatus = 'valid' | 'expired' | 'exhausted';

export interface InviteCode {
  id: string;
  code: string;
  createdAt: string;
  expiresAt: string;
  usedCount: number;
  limit: number;
  remark: string;
  status: InviteStatus;
}

export interface RequestLog {
  id: string;
  timestamp: string;
  requestId?: string;
  userEmail: string;
  apiKeyId?: string;
  apiKeyName: string;
  sourceId?: string;
  sourceKeyId?: string;
  sourceKeyAlias?: string;
  protocol?: string;
  path?: string;
  stream?: boolean;
  model: string;
  upstreamName: string;
  tokensPrompt: number;
  tokensCompletion: number;
  tokensCacheRead?: number;
  tokensCacheWrite?: number;
  tokensReasoning?: number;
  tokensTotal: number;
  estimatedCost?: number;
  latencyMs: number;
  statusCode: number;
  statusText: 'success' | 'error';
  errorMessage?: string;
  requestHeaders?: Record<string, string>;
  responseHeaders?: Record<string, string>;
  requestPayload?: unknown;
  responsePayload?: unknown;
  attemptCount?: number;
  finalAttemptId?: string;
}

export interface RequestLogPagination {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
}

export interface RequestAttemptLog {
  id: string;
  usageLogId: string;
  requestId: string;
  attemptIndex: number;
  modelConfigId?: string;
  sourceId?: string;
  sourceKeyId?: string;
  model: string;
  upstreamName: string;
  protocol: string;
  path: string;
  statusCode: number;
  statusText: 'success' | 'error';
  errorMessage?: string;
  latencyMs: number;
  startedAt: string;
  endedAt: string;
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

export interface UsageStats {
  totalTokens: number;
  totalCost: number;
  totalRequests: number;
  trend: { date: string; label?: string; tokens: number; cost: number; requests?: number }[];
  byModel: { model: string; tokens: number; percentage: number; color: string }[];
  byUser: { userId: string; email: string; tokens: number; requests: number; cost: number; percentage: number }[];
  range?: 'day' | 'week' | 'month';
  granularity?: 'hour' | 'day';
}

export interface PlatformSettings {
  platformName: string;
  supportEmail: string;
  openRegistration: boolean;
  requireInviteCode: boolean;
  defaultUserBalance: number;
  maxRetries: number;
  defaultTimeout: number;
  streamingEnabled?: boolean;
  requireEmailVerification?: boolean;
  hideUpstreamNameFromUsers?: boolean;
}

export type ApiKeyStatus = 'valid' | 'disabled';

export interface ApiKey {
  id: string;
  name: string;
  key: string;
  masked: string;
  createdAt: string;
  lastUsedAt?: string;
  status: ApiKeyStatus;
  limit?: number;
  spent: number;
  modelGroupId?: string;
  modelGroupName?: string;
}

export interface ModelAccessGroup {
  id: string;
  name: string;
  description?: string;
  isDefault: boolean;
  keyCount?: number;
  modelCount?: number;
  bindings?: ModelBindingInput[];
  createdAt: string;
}

export interface UserQuota {
  used: number;
  total: number;
  remaining: number;
  percentageUsed: number;
  billingPeriodStart: string;
  billingPeriodEnd: string;
  todayRequests: number;
  todayTokens: number;
  monthRequests: number;
  monthTokens: number;
}

export interface UserModel {
  id: string;
  name: string;
  provider: ModelProvider;
  formats: ModelFormat[];
  status: 'online' | 'offline';
  latencyMs: number;
  sourceId?: string;
  sourceName?: string;
  modelGroupId?: string;
  modelGroupName?: string;
  sourceType?: SourceType;
  sourceStatus?: SourceStatus;
  routingCandidates?: number;
}

export interface UserModelInvokeTestResult {
  ok: boolean;
  latencyMs: number;
  statusCode: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
}

export interface UsageDetailRow {
  date: string;
  requests: number;
  promptTokens: number;
  completionTokens: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  reasoningTokens?: number;
  totalTokens?: number;
  estimatedCost: number;
}
