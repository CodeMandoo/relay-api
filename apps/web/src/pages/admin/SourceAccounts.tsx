import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  ArrowLeft,
  Hash,
  KeyRound,
  Loader2,
  LogIn,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
  UserRound,
  WalletCards,
} from 'lucide-react';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
  Badge,
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  Label,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Progress,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Checkbox,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@relay-api/ui';
import {
  formatNumberFull,
  formatRelative,
  formatTimeRemaining,
  type AccountProvider,
  type AccountStatus,
  type SourceAccount,
  type SourceAccountTokenUsage,
  type UpstreamSource,
} from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { EmptyState } from '@/components/common/EmptyState';

const providers: AccountProvider[] = ['ChatGPT', 'Claude', 'Gemini', 'Grok'];
const manualTokenProviders = new Set<AccountProvider>(['ChatGPT', 'Claude']);

const supportsAccountPool = (source?: UpstreamSource | null): boolean => source?.type === 'CLIProxyAPI';
const supportsManualTokenLogin = (provider: AccountProvider): boolean => manualTokenProviders.has(provider);

const sourceTypeLabel = (source?: UpstreamSource | null): string =>
  source?.type === 'CLIProxyAPI' ? 'CLIProxyAPI' : '第三方提供商';

const accountProviderToModelProvider = (provider: AccountProvider): 'OpenAI' | 'Anthropic' | 'Google' | 'xAI' => {
  if (provider === 'ChatGPT') return 'OpenAI';
  if (provider === 'Claude') return 'Anthropic';
  if (provider === 'Gemini') return 'Google';
  return 'xAI';
};

const statusTone = (status: AccountStatus): 'success' | 'warning' | 'destructive' => {
  if (status === 'valid') return 'success';
  if (status === 'cooldown') return 'warning';
  return 'destructive';
};

const statusLabel = (status: AccountStatus): string => {
  if (status === 'valid') return '有效';
  if (status === 'cooldown') return '冷却中';
  return '已过期';
};

const hasQuotaData = (account: SourceAccount): boolean =>
  account.limit5h > 0 || account.limit7d > 0 || account.used5h > 0 || account.used7d > 0;

const quotaRemaining = (used: number, limit: number): number => (limit > 0 ? Math.max(0, limit - used) : 0);

const quotaRemainingPercent = (used: number, limit: number): number =>
  limit > 0 ? Math.min(100, Math.max(0, (quotaRemaining(used, limit) / limit) * 100)) : 0;

const quotaRemainingText = (used: number, limit: number): string => {
  if (limit <= 0) return '--';
  const remaining = quotaRemaining(used, limit);
  if (limit === 100) return `${Math.round(remaining)}%`;
  return `${remaining} / ${limit}`;
};

const planLabel = (plan?: string): string => {
  const normalized = plan?.trim().toLowerCase().replace(/[\s-]+/g, '_');
  if (!normalized) return '待同步';
  if (normalized.includes('pro') && (normalized.includes('20x') || normalized.includes('20_x'))) return 'ChatGPT Pro 20x';
  if (normalized.includes('pro') && (normalized.includes('5x') || normalized.includes('5_x'))) return 'ChatGPT Pro 5x';
  const labels: Record<string, string> = {
    free: 'ChatGPT Free',
    chatgpt_free: 'ChatGPT Free',
    go: 'ChatGPT Go',
    chatgpt_go: 'ChatGPT Go',
    plus: 'ChatGPT Plus',
    chatgpt_plus: 'ChatGPT Plus',
    pro: 'ChatGPT Pro',
    chatgpt_pro: 'ChatGPT Pro',
    pro_5x: 'ChatGPT Pro 5x',
    pro_20x: 'ChatGPT Pro 20x',
    team: 'ChatGPT Team',
    chatgpt_team: 'ChatGPT Team',
    business: 'ChatGPT Business',
    chatgpt_business: 'ChatGPT Business',
    enterprise: 'ChatGPT Enterprise',
    chatgpt_enterprise: 'ChatGPT Enterprise',
    edu: 'ChatGPT Edu',
    chatgpt_edu: 'ChatGPT Edu',
    education: 'ChatGPT Edu',
  };
  return labels[normalized] ?? plan!;
};

const subscriptionTagLabel = (provider: AccountProvider, plan?: string): string => {
  const label = planLabel(plan).trim();
  if (!label || label === '待同步') return '待同步';
  const providerPrefixes: Record<AccountProvider, RegExp> = {
    ChatGPT: /^chatgpt\s+/i,
    Claude: /^claude\s+/i,
    Gemini: /^gemini\s+/i,
    Grok: /^(grok|xai)\s+/i,
  };
  const withoutProvider = label.replace(providerPrefixes[provider], '').trim();
  return withoutProvider || label;
};

const compactTokenNumber = (value: number): string => {
  const normalized = Number.isFinite(value) ? value : 0;
  const abs = Math.abs(normalized);
  const trim = (next: number) => {
    const precision = next >= 100 ? 0 : next >= 10 ? 1 : 2;
    return next.toFixed(precision).replace(/\.0+$/, '').replace(/(\.\d)0$/, '$1');
  };
  if (abs >= 1_000_000) return `${trim(normalized / 1_000_000)}M`;
  if (abs >= 1_000) return `${trim(normalized / 1_000)}K`;
  return formatNumberFull(Math.round(normalized));
};

const tokenUsageItems = (usage?: SourceAccountTokenUsage) => [
  { label: '今日', value: usage?.dayTokens ?? 0 },
  { label: '本周', value: usage?.weekTokens ?? 0 },
  { label: '本月', value: usage?.monthTokens ?? 0 },
  { label: '总计', value: usage?.totalTokens ?? 0 },
];

const ACCOUNT_AUTO_REFRESH_MS = 5 * 60 * 1000;

const isAccountRefreshStale = (account: SourceAccount, now = Date.now()): boolean => {
  const lastRefreshedAt = Date.parse(account.lastRefreshed);
  return !Number.isFinite(lastRefreshedAt) || now - lastRefreshedAt > ACCOUNT_AUTO_REFRESH_MS;
};

export default function Page() {
  const { sourceId } = useParams();
  const [sources, setSources] = useState<UpstreamSource[]>([]);
  const [accounts, setAccounts] = useState<SourceAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [refreshingAll, setRefreshingAll] = useState(false);
  const [refreshingId, setRefreshingId] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [tokenUsage, setTokenUsage] = useState<Record<string, SourceAccountTokenUsage>>({});
  const [tokenUsageLoadingId, setTokenUsageLoadingId] = useState<string | null>(null);
  const [tokenUsageErrors, setTokenUsageErrors] = useState<Record<string, string>>({});
  const [provider, setProvider] = useState<AccountProvider>('ChatGPT');
  const [identifier, setIdentifier] = useState('');
  const [loginMode, setLoginMode] = useState('oauth');
  const [startingOAuth, setStartingOAuth] = useState(false);
  const [submittingCallback, setSubmittingCallback] = useState(false);
  const [callbackUrl, setCallbackUrl] = useState('');
  const [manualToken, setManualToken] = useState('');
  const [oauthSession, setOauthSession] = useState<{ authUrl?: string; sessionId?: string; statusUrl?: string } | null>(null);
  const autoRefreshedSourcesRef = useRef<Set<string>>(new Set());
  const source = useMemo(() => sources.find((item) => item.id === sourceId) ?? sources[0], [sourceId, sources]);
  const accountPoolSupported = supportsAccountPool(source);

  const reloadAccounts = () => {
    if (!sourceId) return;
    setLoading(true);
    adminApi
      .sources()
      .then(async (sourceResponse) => {
        setSources(sourceResponse.data);
        const currentSource = sourceResponse.data.find((item) => item.id === sourceId);
        if (!supportsAccountPool(currentSource)) {
          setAccounts([]);
          return;
        }
        const accountResponse = await adminApi.sourceAccounts(sourceId);
        const loadedAccounts = accountResponse.data;
        setAccounts(loadedAccounts);

        const shouldAutoRefresh =
          currentSource?.hasManagementKey &&
          loadedAccounts.some((account) => isAccountRefreshStale(account)) &&
          !autoRefreshedSourcesRef.current.has(sourceId);

        if (shouldAutoRefresh) {
          autoRefreshedSourcesRef.current.add(sourceId);
          setRefreshingAll(true);
          try {
            const syncResponse = await adminApi.syncSourceAccounts(sourceId);
            setAccounts(syncResponse.data);
          } catch (error) {
            toast.error(getErrorMessage(error, '自动刷新账号失败'));
          } finally {
            setRefreshingAll(false);
          }
        }
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载账号池失败')))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    reloadAccounts();
  }, [sourceId]);

  const sourceAccounts = useMemo(() => {
    const q = query.trim().toLowerCase();
    return accounts
      .filter((a) => !source || a.sourceId === source.id)
      .filter((a) => {
        const plan = `${a.planType ?? ''} ${a.subscriptionPlan ?? ''}`.toLowerCase();
        return !q || a.identifier.toLowerCase().includes(q) || a.provider.toLowerCase().includes(q) || plan.includes(q);
      });
  }, [accounts, query, source]);

  const stats = useMemo(() => {
    const valid = sourceAccounts.filter((a) => a.status === 'valid').length;
    const cooldown = sourceAccounts.filter((a) => a.status === 'cooldown').length;
    const totalBalance = sourceAccounts.reduce((sum, a) => sum + a.balance, 0);
    return { total: sourceAccounts.length, valid, cooldown, totalBalance };
  }, [sourceAccounts]);

  const allChecked = sourceAccounts.length > 0 && sourceAccounts.every((a) => selected.has(a.id));

  const refreshOne = async (id: string) => {
    if (!source?.hasManagementKey) {
      toast.error('请在后端配置 RELAY_CLIPROXYAPI_MANAGEMENT_KEY', {
        description: '账号刷新需要从 CLIProxyAPI Management API 同步。',
      });
      return;
    }
    setRefreshingId(id);
    try {
      const response = await adminApi.refreshSourceAccount(id);
      setAccounts((prev) =>
        prev.map((a) => (a.id === id ? response.data : a)),
      );
      toast.success('账号状态已同步');
    } catch (error) {
      toast.error(getErrorMessage(error, '刷新账号失败'));
    } finally {
      setRefreshingId(null);
    }
  };

  const refreshAll = async () => {
    if (!source || !accountPoolSupported) return;
    if (!source.hasManagementKey) {
      toast.error('请在后端配置 RELAY_CLIPROXYAPI_MANAGEMENT_KEY', {
        description: '需要与 CLIProxyAPI config.yaml 的 remote-management.secret-key 一致。',
      });
      return;
    }
    setRefreshingAll(true);
    try {
      const response = await adminApi.syncSourceAccounts(source.id);
      setAccounts(response.data);
      toast.success(`已同步 ${response.data.length} 个账号`);
    } catch (error) {
      toast.error(getErrorMessage(error, '同步账号失败'));
    } finally {
      setRefreshingAll(false);
    }
  };

  const addAccount = async () => {
    if (!source) return;
    if (!accountPoolSupported) {
      toast.error('该上游源不支持账号池');
      return;
    }
    if (loginMode === 'token' && !identifier.trim()) {
      toast.error('请填写账号标识');
      return;
    }
    if (loginMode === 'token' && !manualToken.trim()) {
      toast.error('请填写 refresh_token');
      return;
    }
    if (loginMode === 'token' && !supportsManualTokenLogin(provider)) {
      toast.error('当前仅支持 ChatGPT / Claude 手动 Token 登录', {
        description: 'Gemini 和 Grok 请继续使用浏览器 OAuth。',
      });
      return;
    }
    try {
      if (loginMode === 'oauth') {
        if (!source.hasManagementKey) {
          toast.error('请在后端配置 RELAY_CLIPROXYAPI_MANAGEMENT_KEY', {
            description: '该环境变量需要对应 config.yaml 的 remote-management.secret-key。',
          });
          return;
        }
        setStartingOAuth(true);
        const session = await adminApi.startSourceOAuth(source.id, provider);
        setOauthSession(session);
        setCallbackUrl('');
        if (session.authUrl) {
          window.open(session.authUrl, '_blank', 'noopener,noreferrer');
        }
        toast.success('OAuth 授权已创建', { description: session.authUrl ? '请在新窗口完成授权。' : '请按上游返回信息继续。' });
        return;
      } else {
        setStartingOAuth(true);
        const response = await adminApi.submitSourceAccountToken(source.id, {
          identifier: identifier.trim(),
          provider,
          refreshToken: manualToken.trim(),
        });
        setAccounts(response.data);
        toast.success('Token 登录完成', { description: '账号已同步到 CLIProxyAPI。' });
      }
      setIdentifier('');
      setProvider('ChatGPT');
      setLoginMode('oauth');
      setOauthSession(null);
      setCallbackUrl('');
      setManualToken('');
      setOpen(false);
    } catch (error) {
      toast.error(getErrorMessage(error, '添加账号失败'));
    } finally {
      setStartingOAuth(false);
    }
  };

  const submitOAuthCallback = async () => {
    if (!source) return;
    const redirectUrl = callbackUrl.trim();
    if (!redirectUrl) {
      toast.error('请粘贴浏览器返回的完整回调链接');
      return;
    }
    setSubmittingCallback(true);
    try {
      const response = await adminApi.submitSourceOAuthCallback(source.id, provider, redirectUrl);
      if (response.data?.length) {
        setAccounts(response.data);
      }
      if (response.pending) {
        toast.success('回调已提交', { description: 'CLIProxyAPI 仍在换取 Token，稍后刷新账号列表。' });
        window.setTimeout(reloadAccounts, 3000);
      } else {
        toast.success('授权完成，账号已同步');
      }
      setCallbackUrl('');
      setOauthSession(null);
      setOpen(false);
    } catch (error) {
      toast.error(getErrorMessage(error, '提交回调失败'));
    } finally {
      setSubmittingCallback(false);
    }
  };

  const removeAccount = async (id: string) => {
    try {
      await adminApi.deleteSourceAccount(id);
      setAccounts((prev) => prev.filter((a) => a.id !== id));
      setSelected((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      toast.success('账号已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除账号失败'));
    }
  };

  const reauthorizeAccount = async (account: SourceAccount) => {
    try {
      if (!source?.hasManagementKey) {
        toast.error('请在后端配置 RELAY_CLIPROXYAPI_MANAGEMENT_KEY', {
          description: '重新授权需要对应 config.yaml 的 remote-management.secret-key。',
        });
        return;
      }
      const session = await adminApi.startSourceOAuth(account.sourceId, account.provider);
      if (session.authUrl) {
        window.open(session.authUrl, '_blank', 'noopener,noreferrer');
      }
      toast.success('OAuth 授权已创建', { description: session.authUrl ? '请在新窗口完成授权。' : account.provider });
    } catch (error) {
      toast.error(getErrorMessage(error, '创建授权会话失败'));
    }
  };

  const loadTokenUsage = async (accountId: string) => {
    setTokenUsageLoadingId(accountId);
    setTokenUsageErrors((prev) => {
      const next = { ...prev };
      delete next[accountId];
      return next;
    });
    try {
      const response = await adminApi.sourceAccountTokenUsage(accountId);
      setTokenUsage((prev) => ({ ...prev, [accountId]: response.data }));
    } catch (error) {
      const message = getErrorMessage(error, '加载 Token 消耗失败');
      setTokenUsageErrors((prev) => ({ ...prev, [accountId]: message }));
      toast.error(message);
    } finally {
      setTokenUsageLoadingId((current) => (current === accountId ? null : current));
    }
  };

  if (loading && !source) {
    return <div className="p-10 text-center text-sm text-muted-foreground">正在加载账号池...</div>;
  }

  if (!source) {
    return <EmptyState icon={KeyRound} title="上游源不存在" description="请返回上游源列表重新选择。" />;
  }

  if (!accountPoolSupported) {
    return (
      <div className="space-y-6">
        <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <Button asChild variant="ghost" size="sm" className="-ml-2">
            <Link to="/admin/sources">
              <ArrowLeft className="mr-1.5 h-4 w-4" />
              返回上游列表
            </Link>
          </Button>
          <span>/</span>
          <span>{source.name}</span>
        </div>
        <EmptyState
          icon={KeyRound}
          title="该上游源不支持账号池"
          description={`${sourceTypeLabel(source)} 使用上游源 API Key 直接认证，请在上游源配置中维护 Key。`}
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <Button asChild variant="ghost" size="sm" className="-ml-2">
          <Link to="/admin/sources">
            <ArrowLeft className="mr-1.5 h-4 w-4" />
            返回上游列表
          </Link>
        </Button>
        <span>/</span>
        <span>{source.name}</span>
      </div>

      <PageHeader
        eyebrow="账号池"
        title={`${source.name} · 账号管理`}
        description="管理该上游下的 OAuth 账号、余额与登录状态。"
        actions={
          <>
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                className="h-9 w-64 pl-9"
                placeholder="搜索账号标识..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
            <Button variant="outline" onClick={refreshAll} disabled={refreshingAll}>
              <RefreshCw className={cn('mr-2 h-4 w-4', refreshingAll && 'animate-spin')} />
              刷新全部额度
            </Button>
            <Dialog
              open={open}
              onOpenChange={(nextOpen) => {
                setOpen(nextOpen);
                if (!nextOpen) {
                  setOauthSession(null);
                  setCallbackUrl('');
                  setManualToken('');
                }
              }}
            >
              <DialogTrigger asChild>
                <Button className="shadow-sm">
                  <Plus className="mr-2 h-4 w-4" />
                  添加账号
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>添加账号</DialogTitle>
                  <DialogDescription>
                    选择登录平台，使用浏览器 OAuth 或手动 refresh_token 登录。
                  </DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                  <div className="grid gap-2">
                    <Label>平台</Label>
                    <Select
                      value={provider}
                      onValueChange={(value) => {
                        setProvider(value as AccountProvider);
                        setOauthSession(null);
                        setCallbackUrl('');
                        setManualToken('');
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {providers.map((item) => (
                          <SelectItem key={item} value={item}>
                            {item}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="identifier">账号标识</Label>
                    <Input
                      id="identifier"
                      value={identifier}
                      onChange={(e) => setIdentifier(e.target.value)}
                      placeholder="user@example.com"
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label>登录方式</Label>
                    <Select
                      value={loginMode}
                      onValueChange={(value) => {
                        setLoginMode(value);
                        setOauthSession(null);
                        setCallbackUrl('');
                        setManualToken('');
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="oauth">浏览器 OAuth</SelectItem>
                        <SelectItem value="token">手动 Token</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  {loginMode === 'oauth' && oauthSession ? (
                    <div className="grid gap-3 rounded-md border bg-muted/30 p-3">
                      <div className="space-y-1">
                        <Label htmlFor="oauth-callback-url">授权回调链接</Label>
                        <p className="text-xs leading-relaxed text-muted-foreground">
                          登录后浏览器会跳到 localhost，把地址栏完整链接粘贴到这里，不要修改 code 或 state。
                        </p>
                      </div>
                      <textarea
                        id="oauth-callback-url"
                        value={callbackUrl}
                        onChange={(event) => setCallbackUrl(event.target.value)}
                        placeholder="http://localhost:1455/auth/callback?code=...&state=..."
                        className={cn(
                          'min-h-24 w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none transition-colors',
                          'placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20',
                        )}
                      />
                      {oauthSession.authUrl ? (
                        <Button
                          type="button"
                          variant="outline"
                          className="w-fit"
                          onClick={() => window.open(oauthSession.authUrl, '_blank', 'noopener,noreferrer')}
                        >
                          <LogIn className="mr-2 h-4 w-4" />
                          重新打开授权页
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                  {loginMode === 'token' ? (
                    <div className="grid gap-3 rounded-md border bg-muted/30 p-3">
                      <div className="space-y-1">
                        <Label htmlFor="manual-refresh-token">Refresh Token</Label>
                        <p className="text-xs leading-relaxed text-muted-foreground">
                          填写 refresh_token，不是 access_token 或 id_token。Relay 只会转交给 CLIProxyAPI 保存为 auth file，不写入 Relay 数据库。
                        </p>
                      </div>
                      {!supportsManualTokenLogin(provider) ? (
                        <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                          当前仅支持 ChatGPT / Claude 手动 Token 登录；{provider} 请使用浏览器 OAuth。
                        </div>
                      ) : null}
                      <textarea
                        id="manual-refresh-token"
                        value={manualToken}
                        onChange={(event) => setManualToken(event.target.value)}
                        placeholder="refresh_token"
                        disabled={!supportsManualTokenLogin(provider)}
                        className={cn(
                          'min-h-28 w-full resize-y rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm outline-none transition-colors',
                          'placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20',
                          !supportsManualTokenLogin(provider) && 'cursor-not-allowed opacity-60',
                        )}
                      />
                    </div>
                  ) : null}
                </div>
                <DialogFooter>
                  <Button variant="ghost" onClick={() => setOpen(false)}>
                    取消
                  </Button>
                  {loginMode === 'oauth' && oauthSession ? (
                    <Button onClick={submitOAuthCallback} disabled={submittingCallback || !callbackUrl.trim()}>
                      {submittingCallback ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                      提交回调链接
                    </Button>
                  ) : (
                    <Button onClick={addAccount} disabled={startingOAuth || (loginMode === 'token' && (!manualToken.trim() || !supportsManualTokenLogin(provider)))}>
                      {startingOAuth ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                      {loginMode === 'oauth' ? '继续授权' : 'Token 登录'}
                    </Button>
                  )}
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        }
      />

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="总账号数" value={stats.total} icon={KeyRound} tone="primary" delay={0} hint="当前源下账号" />
        <StatCard label="有效账号" value={stats.valid} icon={ShieldCheck} tone="success" delay={0.05} hint="来自 CLIProxyAPI 账号状态" />
        <StatCard label="冷却中" value={stats.cooldown} icon={RefreshCw} tone="warning" delay={0.1} hint="达到频率限制" />
        <StatCard label="总剩余额度" value={formatNumberFull(stats.totalBalance)} icon={WalletCards} tone="neutral" delay={0.15} hint="按上游额度汇总" />
      </div>

      {sourceAccounts.length === 0 ? (
        <EmptyState icon={KeyRound} title="没有账号" description="为该上游源添加第一个 OAuth 账号。" action={<Button onClick={() => setOpen(true)}>添加账号</Button>} />
      ) : (
        <Card className="overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-10">
                  <Checkbox
                    checked={allChecked}
                    onCheckedChange={() => {
                      if (allChecked) setSelected(new Set());
                      else setSelected(new Set(sourceAccounts.map((a) => a.id)));
                    }}
                  />
                </TableHead>
                <TableHead>账号标识</TableHead>
                <TableHead>平台</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="min-w-[280px]">剩余额度 (5h / 7d)</TableHead>
                <TableHead>最后刷新</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sourceAccounts.map((account, index) => {
                const quotaVisible = hasQuotaData(account);
                const fiveHourRemainingPercent = quotaRemainingPercent(account.used5h, account.limit5h);
                const sevenDayRemainingPercent = quotaRemainingPercent(account.used7d, account.limit7d);
                const accountTokenUsage = tokenUsage[account.id];
                const accountTokenUsageError = tokenUsageErrors[account.id] || accountTokenUsage?.syncError;
                const accountTokenUsageLoading = tokenUsageLoadingId === account.id;
                return (
                  <motion.tr
                    key={account.id}
                    initial={{ opacity: 0, y: 6 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: index * 0.03, ease: [0.22, 1, 0.36, 1] }}
                    className={cn(
                      'border-b transition-colors hover:bg-muted/40',
                      account.status === 'expired' && 'bg-destructive/[0.04]',
                    )}
                  >
                    <TableCell>
                      <Checkbox
                        checked={selected.has(account.id)}
                        onCheckedChange={() =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (next.has(account.id)) next.delete(account.id);
                            else next.add(account.id);
                            return next;
                          })
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <span className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10 text-primary">
                          <UserRound className="h-4 w-4" />
                        </span>
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium">{account.identifier}</div>
                          <div className="text-xs text-muted-foreground">{account.id}</div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex h-6 items-center gap-2">
                        <ProviderIcon provider={accountProviderToModelProvider(account.provider)} />
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className="truncate text-sm font-medium leading-6">{account.provider}</span>
                          <Badge
                            variant="secondary"
                            className={cn(
                              'inline-flex h-5 shrink-0 items-center justify-center rounded-md border px-1.5 py-0 text-[10px] font-semibold leading-none shadow-none',
                              (account.subscriptionPlan ?? account.planType)
                                ? 'border-primary/15 bg-primary/5 text-primary'
                                : 'border-border/70 bg-muted/50 text-muted-foreground',
                            )}
                          >
                            {subscriptionTagLabel(account.provider, account.subscriptionPlan ?? account.planType)}
                          </Badge>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-2">
                        <StatusBadge tone={statusTone(account.status)} label={statusLabel(account.status)} />
                        <div className="flex items-center gap-1.5 text-[10px] font-mono font-bold text-muted-foreground/60">
                          <WalletCards className="h-3 w-3" />
                          {account.balance}/{account.balanceLimit}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="space-y-3 py-1">
                        {quotaVisible ? (
                          <>
                            <div className="space-y-1">
                              <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-wider text-muted-foreground/70">
                                <div className="flex items-center gap-1.5">
                                  <span>5小时剩余</span>
                                  {account.nextRefresh5h && (
                                    <span className="inline-flex items-center gap-0.5 rounded bg-muted px-1 py-0.5 text-[9px] font-medium normal-case tracking-normal">
                                      {formatTimeRemaining(account.nextRefresh5h)} 后刷新
                                    </span>
                                  )}
                                </div>
                                <span>{quotaRemainingText(account.used5h, account.limit5h)}</span>
                              </div>
                              <Progress
                                value={fiveHourRemainingPercent}
                                className={cn(
                                  'h-1 bg-muted/40',
                                  fiveHourRemainingPercent <= 10 && '[&>div]:bg-destructive',
                                  fiveHourRemainingPercent > 10 && fiveHourRemainingPercent <= 30 && '[&>div]:bg-amber-500',
                                )}
                              />
                            </div>
                            <div className="space-y-1">
                              <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-wider text-muted-foreground/70">
                                <div className="flex items-center gap-1.5">
                                  <span>一周剩余</span>
                                  {account.nextRefresh7d && (
                                    <span className="inline-flex items-center gap-0.5 rounded bg-muted px-1 py-0.5 text-[9px] font-medium normal-case tracking-normal">
                                      {formatTimeRemaining(account.nextRefresh7d)} 后刷新
                                    </span>
                                  )}
                                </div>
                                <span>{quotaRemainingText(account.used7d, account.limit7d)}</span>
                              </div>
                              <Progress
                                value={sevenDayRemainingPercent}
                                className={cn(
                                  'h-1 bg-muted/40',
                                  sevenDayRemainingPercent <= 10 && '[&>div]:bg-destructive',
                                  sevenDayRemainingPercent > 10 && sevenDayRemainingPercent <= 30 && '[&>div]:bg-amber-500',
                                )}
                              />
                            </div>
                          </>
                        ) : (
                          <div className="rounded-md border border-dashed border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                            暂未获取到 5小时/一周额度，点击刷新同步。
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{formatRelative(account.lastRefreshed)}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Popover onOpenChange={(nextOpen) => nextOpen && void loadTokenUsage(account.id)}>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <PopoverTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="查看 Token 消耗">
                                  {accountTokenUsageLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Hash className="h-4 w-4" />}
                                </Button>
                              </PopoverTrigger>
                            </TooltipTrigger>
                            <TooltipContent>查看 Token 消耗</TooltipContent>
                          </Tooltip>
                          <PopoverContent align="end" className="w-72 p-3">
                            <div className="space-y-3">
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                  <div className="text-sm font-semibold">Token 消耗</div>
                                  <div className="mt-0.5 truncate text-xs text-muted-foreground">{account.identifier}</div>
                                </div>
                                {accountTokenUsageLoading && accountTokenUsage ? (
                                  <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
                                ) : accountTokenUsage?.syncedCount ? (
                                  <Badge variant="outline" className="h-5 shrink-0 rounded-md px-1.5 py-0 text-[10px]">
                                    +{accountTokenUsage.syncedCount}
                                  </Badge>
                                ) : null}
                              </div>
                              {accountTokenUsageLoading && !accountTokenUsage ? (
                                <div className="flex h-24 items-center justify-center text-xs text-muted-foreground">
                                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                  正在读取...
                                </div>
                              ) : (
                                <>
                                  <div className="grid grid-cols-2 gap-2">
                                    {tokenUsageItems(accountTokenUsage).map((item) => (
                                      <div key={item.label} className="rounded-md border bg-muted/20 px-3 py-2">
                                        <div className="text-[10px] font-medium text-muted-foreground">{item.label}</div>
                                        <div className="mt-1 font-mono text-lg font-semibold leading-none">
                                          {compactTokenNumber(item.value)}
                                        </div>
                                      </div>
                                    ))}
                                  </div>
                                  {accountTokenUsageError && (
                                    <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11px] leading-5 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300">
                                      {accountTokenUsageError}
                                    </div>
                                  )}
                                </>
                              )}
                            </div>
                          </PopoverContent>
                        </Popover>
                        {account.status === 'expired' ? (
                          <Button size="sm" onClick={() => refreshOne(account.id)} disabled={refreshingId === account.id}>
                            {refreshingId === account.id ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <LogIn className="mr-1.5 h-3.5 w-3.5" />}
                            重新授权
                          </Button>
                        ) : (
                          <>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => refreshOne(account.id)} disabled={refreshingId === account.id} aria-label="同步账号状态">
                                  <RefreshCw className={cn('h-4 w-4', refreshingId === account.id && 'animate-spin')} />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>同步账号状态</TooltipContent>
                            </Tooltip>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => reauthorizeAccount(account)} aria-label="重新授权 OAuth">
                                  <LogIn className="h-4 w-4" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>重新授权 OAuth</TooltipContent>
                            </Tooltip>
                          </>
                        )}
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive hover:text-destructive">
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>删除该账号?</AlertDialogTitle>
                              <AlertDialogDescription>
                                账号 {account.identifier} 将从该上游源账号池移除。
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>取消</AlertDialogCancel>
                              <AlertDialogAction
                                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                onClick={() => removeAccount(account.id)}
                              >
                                确认删除
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      </div>
                    </TableCell>
                  </motion.tr>
                );
              })}
            </TableBody>
          </Table>
        </Card>
      )}
    </div>
  );
}
