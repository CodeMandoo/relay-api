import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Activity,
  Edit,
  KeyRound,
  Link2,
  Plus,
  Power,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  Trash2,
  WifiOff,
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
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  Label,
  Progress,
  Switch,
  cn,
} from '@relay-api/ui';
import {
  formatNumberFull,
  type SourceKey,
  type SourceStatus,
  type SourceType,
  type UpstreamSource,
} from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { StatusDot } from '@/components/common/StatusDot';
import { EmptyState } from '@/components/common/EmptyState';

const sourceSupportsAccountPool = (source: UpstreamSource): boolean => source.type === 'CLIProxyAPI';
const sourceSupportsKeyPool = (source: UpstreamSource): boolean => !sourceSupportsAccountPool(source);

const sourceTypeLabel = (value?: string): string =>
  value === 'CLIProxyAPI' ? '内置 CLIProxyAPI' : value === 'Third-party Provider' ? '第三方提供商' : (value ?? '未知类型');

const normalizedBaseInput = (value: string): string => value.trim().replace(/\/+$/, '');

const defaultOpenAIBaseUrl = (apiBase: string): string => {
  const base = normalizedBaseInput(apiBase);
  if (!base) return 'https://api.example.com/v1';
  return base.toLowerCase().endsWith('/v1') ? base : `${base}/v1`;
};

const defaultAnthropicBaseUrl = (apiBase: string): string => normalizedBaseInput(apiBase) || 'https://api.example.com';

const statusTone = (status: SourceStatus): 'success' | 'destructive' | 'neutral' => {
  if (status === 'online') return 'success';
  if (status === 'offline') return 'destructive';
  return 'neutral';
};

const statusLabel = (status: SourceStatus): string => {
  if (status === 'online') return '可访问';
  if (status === 'offline') return '不可访问';
  return '已禁用';
};

const statusDot = (status: SourceStatus): 'online' | 'offline' | 'idle' => {
  if (status === 'online') return 'online';
  if (status === 'offline') return 'offline';
  return 'idle';
};

const isCoolingSource = (source: UpstreamSource): boolean =>
  Boolean(source.coolingDown || (source.cooldownUntil && new Date(source.cooldownUntil).getTime() > Date.now()));

const shortDateTime = (value?: string): string => (value ? new Date(value).toLocaleString() : '无');

export default function Page() {
  const [sources, setSources] = useState<UpstreamSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const [checkingSources, setCheckingSources] = useState<Set<string>>(new Set());
  
  const [open, setOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editingSource, setEditingSource] = useState<UpstreamSource | null>(null);
  const [keyOpen, setKeyOpen] = useState(false);
  const [keySource, setKeySource] = useState<UpstreamSource | null>(null);
  const [sourceKeys, setSourceKeys] = useState<SourceKey[]>([]);
  const [editingKey, setEditingKey] = useState<SourceKey | null>(null);
  const [keyAlias, setKeyAlias] = useState('');
  const [keySecret, setKeySecret] = useState('');
  const [keyLoading, setKeyLoading] = useState(false);

  const [name, setName] = useState('');
  const [type, setType] = useState<SourceType>('Third-party Provider');
  const [apiBase, setApiBase] = useState('');
  const [openaiBaseUrl, setOpenaiBaseUrl] = useState('');
  const [anthropicBaseUrl, setAnthropicBaseUrl] = useState('');
  const [apiKey, setApiKey] = useState('');

  const reloadSources = () => {
    setLoading(true);
    adminApi
      .sources()
      .then((response) => {
        setSources(response.data);
        response.data
          .filter((source) => source.status !== 'disabled')
          .forEach((source) => {
            void checkSource(source.id, true);
          });
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载上游源失败')))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    reloadSources();
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sources;
    return sources.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        sourceTypeLabel(s.type).toLowerCase().includes(q) ||
        s.apiBase.toLowerCase().includes(q),
    );
  }, [sources, query]);

  const stats = useMemo(() => {
    const online = sources.filter((s) => s.status === 'online').length;
    const accountCount = sources
      .filter(sourceSupportsAccountPool)
      .reduce((sum, s) => sum + s.accountCount, 0);
    const onlineLatency = sources.filter((s) => s.status === 'online').map((s) => s.latencyMs);
    const avgLatency =
      onlineLatency.length === 0 ? 0 : Math.round(onlineLatency.reduce((sum, n) => sum + n, 0) / onlineLatency.length);
    const alerts = sources.filter((s) => s.status === 'offline' || isCoolingSource(s)).length;
    return { online, accountCount, avgLatency, alerts };
  }, [sources]);

  const addSource = async () => {
    if (!name.trim() || !apiBase.trim()) {
      toast.error('请填写上游名称和 API 地址');
      return;
    }
    try {
      const response = await adminApi.createSource({
        name: name.trim(),
        type: 'Third-party Provider',
        apiBase: apiBase.trim(),
        openaiBaseUrl: openaiBaseUrl.trim() || undefined,
        anthropicBaseUrl: anthropicBaseUrl.trim() || undefined,
        apiKey: apiKey.trim() || undefined,
      });
      setSources((prev) => [response.data, ...prev]);
      setOpen(false);
      resetForm();
      toast.success('上游源已添加', { description: response.data.name });
      void checkSource(response.data.id, true);
    } catch (error) {
      toast.error(getErrorMessage(error, '添加上游源失败'));
    }
  };

  const handleEdit = (s: UpstreamSource) => {
    setEditingSource(s);
    setName(s.name);
    setType(s.type);
    setApiBase(s.apiBase);
    setOpenaiBaseUrl(s.openaiBaseUrl ?? '');
    setAnthropicBaseUrl(s.anthropicBaseUrl ?? '');
    setApiKey(s.apiKey ?? '');
    setEditOpen(true);
  };

  const saveEdit = async () => {
    if (!editingSource) return;
    try {
      const payload: Partial<UpstreamSource> = sourceSupportsAccountPool(editingSource)
        ? { name: name.trim() }
        : {
            name: name.trim(),
            apiKey: apiKey.trim() || undefined,
            type: 'Third-party Provider',
            apiBase: apiBase.trim(),
            openaiBaseUrl: openaiBaseUrl.trim(),
            anthropicBaseUrl: anthropicBaseUrl.trim(),
          };
      const response = await adminApi.updateSource(editingSource.id, payload);
      setSources((prev) => prev.map((s) => (s.id === editingSource.id ? response.data : s)));
      setEditOpen(false);
      resetForm();
      toast.success('上游源配置已更新');
    } catch (error) {
      toast.error(getErrorMessage(error, '更新上游源失败'));
    }
  };

  const resetForm = () => {
    setName('');
    setApiBase('');
    setOpenaiBaseUrl('');
    setAnthropicBaseUrl('');
    setApiKey('');
    setType('Third-party Provider');
    setEditingSource(null);
  };

  const toggleSource = async (id: string) => {
    const source = sources.find((item) => item.id === id);
    if (!source) return;
    if (source.status === 'disabled') {
      await checkSource(id);
      return;
    }
    try {
      const response = await adminApi.updateSource(id, { status: 'disabled', load: 0 });
      setSources((prev) => prev.map((s) => (s.id === id ? response.data : s)));
      toast.success('上游源已禁用');
    } catch (error) {
      toast.error(getErrorMessage(error, '更新上游源状态失败'));
    }
  };

  async function checkSource(id: string, silent = false) {
    setCheckingSources((prev) => new Set(prev).add(id));
    try {
      const response = await adminApi.checkSource(id);
      setSources((prev) => prev.map((s) => (s.id === id ? response.data : s)));
      if (!silent) {
        if (response.data.status === 'online') {
          toast.success('上游源可访问', { description: response.data.name });
        } else {
          toast.error(response.error ?? '上游源不可访问');
        }
      }
      return response.data;
    } catch (error) {
      if (!silent) toast.error(getErrorMessage(error, '检测上游源失败'));
      return null;
    } finally {
      setCheckingSources((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }

  const recoverSource = async (id: string) => {
    try {
      const response = await adminApi.recoverSource(id);
      setSources((prev) => prev.map((s) => (s.id === id ? response.data : s)));
      toast.success('已恢复上游调度');
    } catch (error) {
      toast.error(getErrorMessage(error, '恢复上游失败'));
    }
  };

  const removeSource = async (id: string) => {
    try {
      await adminApi.deleteSource(id);
      setSources((prev) => prev.filter((s) => s.id !== id));
      toast.success('上游源已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除上游源失败'));
    }
  };

  const loadSourceKeys = async (sourceId: string) => {
    setKeyLoading(true);
    try {
      const response = await adminApi.sourceKeys(sourceId);
      setSourceKeys(response.data);
    } catch (error) {
      toast.error(getErrorMessage(error, '加载 API Key 失败'));
    } finally {
      setKeyLoading(false);
    }
  };

  const openKeyDialog = (source: UpstreamSource) => {
    setKeySource(source);
    setEditingKey(null);
    setKeyAlias('');
    setKeySecret('');
    setKeyOpen(true);
    void loadSourceKeys(source.id);
  };

  const saveSourceKey = async () => {
    if (!keySource) return;
    if (!keyAlias.trim() || (!editingKey && !keySecret.trim())) {
      toast.error('请填写 Key 别名和密钥');
      return;
    }
    try {
      if (editingKey) {
        const response = await adminApi.updateSourceKey(editingKey.id, {
          alias: keyAlias.trim(),
          ...(keySecret.trim() ? { key: keySecret.trim() } : {}),
        });
        setSourceKeys((prev) => prev.map((item) => (item.id === editingKey.id ? response.data : item)));
        setEditingKey(null);
        setKeyAlias('');
        setKeySecret('');
        toast.success('API Key 已更新');
        return;
      }
      const response = await adminApi.createSourceKey(keySource.id, {
        alias: keyAlias.trim(),
        key: keySecret.trim(),
        status: 'valid',
      });
      setSourceKeys((prev) => [...prev, response.data]);
      setKeyAlias('');
      setKeySecret('');
      toast.success('API Key 已添加');
    } catch (error) {
      toast.error(getErrorMessage(error, editingKey ? '更新 API Key 失败' : '添加 API Key 失败'));
    }
  };

  const editSourceKey = (key: SourceKey) => {
    setEditingKey(key);
    setKeyAlias(key.alias);
    setKeySecret('');
  };

  const cancelEditSourceKey = () => {
    setEditingKey(null);
    setKeyAlias('');
    setKeySecret('');
  };

  const toggleSourceKey = async (key: SourceKey) => {
    const nextStatus: SourceKey['status'] = key.status === 'valid' ? 'disabled' : 'valid';
    try {
      const response = await adminApi.updateSourceKey(key.id, { status: nextStatus });
      setSourceKeys((prev) => prev.map((item) => (item.id === key.id ? response.data : item)));
    } catch (error) {
      toast.error(getErrorMessage(error, '更新 API Key 状态失败'));
    }
  };

  const removeSourceKey = async (key: SourceKey) => {
    try {
      await adminApi.deleteSourceKey(key.id);
      setSourceKeys((prev) => prev.filter((item) => item.id !== key.id));
      toast.success('API Key 已删除，相关模型已恢复默认 Key');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除 API Key 失败'));
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="上游池"
        title="上游源管理"
        description="内置 CLIProxyAPI 由环境变量配置；新增上游只用于接入第三方供应商。"
        actions={
          <>
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                className="h-9 w-64 pl-9"
                placeholder="搜索名称、类型或地址..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button className="shadow-sm">
                  <Plus className="mr-2 h-4 w-4" />
                  添加第三方供应商
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>添加第三方供应商</DialogTitle>
                  <DialogDescription>配置一个新的第三方 API 转发目标，保存后会出现在调度池中。</DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                  <div className="grid gap-2">
                    <Label htmlFor="source-name">名称</Label>
                    <Input id="source-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="OpenRouter_Prod" />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="source-base">API 地址</Label>
                    <Input
                      id="source-base"
                      value={apiBase}
                      onChange={(e) => setApiBase(e.target.value)}
                      placeholder="https://api.example.com"
                    />
                  </div>
                  <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
                    <div className="grid gap-2">
                      <Label htmlFor="source-openai-base">OpenAI 请求基础路径</Label>
                      <Input
                        id="source-openai-base"
                        value={openaiBaseUrl}
                        onChange={(e) => setOpenaiBaseUrl(e.target.value)}
                        placeholder={defaultOpenAIBaseUrl(apiBase)}
                      />
                    </div>
                    <div className="grid gap-2">
                      <Label htmlFor="source-anthropic-base">Anthropic 请求基础路径</Label>
                      <Input
                        id="source-anthropic-base"
                        value={anthropicBaseUrl}
                        onChange={(e) => setAnthropicBaseUrl(e.target.value)}
                        placeholder={defaultAnthropicBaseUrl(apiBase)}
                      />
                    </div>
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="source-key">默认 API Key</Label>
                    <Input
                      id="source-key"
                      value={apiKey}
                      onChange={(e) => setApiKey(e.target.value)}
                      placeholder="可选；未绑定模型 Key 时使用"
                    />
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="ghost" onClick={() => setOpen(false)}>
                    取消
                  </Button>
                  <Button onClick={addSource}>保存上游源</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        }
      />

      {/* Edit Dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑上游源</DialogTitle>
            <DialogDescription>
              {type === 'CLIProxyAPI' ? '内置 CLIProxyAPI 只允许修改显示名称，连接信息由环境变量配置。' : '修改上游连接配置。这些更改将立即影响后续调度。'}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="grid gap-2">
              <Label htmlFor="edit-name">名称</Label>
              <Input id="edit-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label>类型</Label>
              <div className="flex h-10 items-center rounded-md border bg-muted/20 px-3 text-sm font-medium">
                {sourceTypeLabel(type)}
              </div>
            </div>
            {type !== 'CLIProxyAPI' && (
              <>
                <div className="grid gap-2">
                  <Label htmlFor="edit-base">API 地址</Label>
                  <Input id="edit-base" value={apiBase} onChange={(e) => setApiBase(e.target.value)} />
                </div>
                <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
                  <div className="grid gap-2">
                    <Label htmlFor="edit-openai-base">OpenAI 请求基础路径</Label>
                    <Input
                      id="edit-openai-base"
                      value={openaiBaseUrl}
                      onChange={(e) => setOpenaiBaseUrl(e.target.value)}
                      placeholder={defaultOpenAIBaseUrl(apiBase)}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="edit-anthropic-base">Anthropic 请求基础路径</Label>
                    <Input
                      id="edit-anthropic-base"
                      value={anthropicBaseUrl}
                      onChange={(e) => setAnthropicBaseUrl(e.target.value)}
                      placeholder={defaultAnthropicBaseUrl(apiBase)}
                    />
                  </div>
                </div>
              </>
            )}
            {type === 'CLIProxyAPI' && (
              <div className="grid gap-2 rounded-lg border bg-muted/20 p-3">
                <Label>API 地址</Label>
                <div className="truncate font-mono text-xs text-muted-foreground">{apiBase}</div>
                <div className="text-xs text-muted-foreground">
                  通过环境变量 RELAY_CLIPROXYAPI_BASE_URL 配置，界面不支持修改。
                </div>
              </div>
            )}
            {type !== 'CLIProxyAPI' && (
              <div className="grid gap-2">
                <Label htmlFor="edit-key">默认 API Key</Label>
                <Input
                  id="edit-key"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder="留空则不修改"
                />
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setEditOpen(false)}>取消</Button>
            <Button onClick={saveEdit}>保存更改</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={keyOpen} onOpenChange={setKeyOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>管理 API Key</DialogTitle>
            <DialogDescription>
              为 <span className="font-mono font-semibold text-foreground">{keySource?.name}</span> 配置多个可绑定 Key。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-5 py-2">
            <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
              <div className="grid gap-3 md:grid-cols-[1fr_1.5fr_auto]">
                <div className="grid gap-2">
                  <Label htmlFor="source-key-alias">别名</Label>
                  <Input id="source-key-alias" value={keyAlias} onChange={(e) => setKeyAlias(e.target.value)} placeholder="team-a / group-prod" />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="source-key-secret">API Key</Label>
                  <Input
                    id="source-key-secret"
                    type="password"
                    value={keySecret}
                    onChange={(e) => setKeySecret(e.target.value)}
                    placeholder={editingKey ? '留空则不修改' : 'sk-...'}
                  />
                </div>
                <div className="flex items-end">
                  <Button onClick={saveSourceKey} className="w-full">
                    <Plus className="mr-2 h-4 w-4" />
                    {editingKey ? '保存' : '添加'}
                  </Button>
                </div>
              </div>
              {editingKey && (
                <div className="flex items-center justify-between rounded-md bg-background px-3 py-2 text-xs text-muted-foreground">
                  <span>正在编辑 {editingKey.alias}</span>
                  <Button variant="ghost" size="sm" className="h-7 px-2" onClick={cancelEditSourceKey}>
                    取消编辑
                  </Button>
                </div>
              )}
            </div>

            <div className="overflow-hidden rounded-lg border">
              <div className="grid grid-cols-[1fr_1.4fr_88px_112px] gap-3 border-b bg-muted/35 px-3 py-2 text-xs font-semibold text-muted-foreground">
                <span>别名</span>
                <span>Key</span>
                <span>状态</span>
                <span className="text-right">操作</span>
              </div>
              {keyLoading ? (
                <div className="px-3 py-8 text-center text-sm text-muted-foreground">正在加载 API Key...</div>
              ) : sourceKeys.length === 0 ? (
                <div className="px-3 py-8 text-center text-sm text-muted-foreground">暂无 API Key，模型将使用上游源默认 Key。</div>
              ) : (
                sourceKeys.map((item) => (
                  <div key={item.id} className="grid grid-cols-[1fr_1.4fr_88px_112px] items-center gap-3 border-b px-3 py-2 last:border-b-0">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-semibold">{item.alias}</div>
                      <div className="text-[11px] text-muted-foreground">
                        {item.lastUsedAt ? `最近使用 ${new Date(item.lastUsedAt).toLocaleString()}` : '尚未使用'}
                      </div>
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground">{item.masked}</div>
                    <Switch checked={item.status === 'valid'} onCheckedChange={() => toggleSourceKey(item)} />
                    <div className="ml-auto flex items-center gap-1">
                      <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground" onClick={() => editSourceKey(item)}>
                        <Edit className="h-4 w-4" />
                      </Button>
                      <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-destructive" onClick={() => removeSourceKey(item)}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setKeyOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="可访问源" value={`${stats.online}/${sources.length}`} icon={Server} tone="success" delay={0} hint="URL 健康检测通过" />
        <StatCard label="账号池账号" value={formatNumberFull(stats.accountCount)} icon={KeyRound} tone="primary" delay={0.05} hint="仅统计 CLIProxyAPI OAuth 账号池" />
        <StatCard label="平均延迟" value={`${stats.avgLatency}ms`} icon={Activity} tone="neutral" delay={0.1} hint="仅统计在线源" />
        <StatCard label="异常告警" value={stats.alerts} icon={WifiOff} tone={stats.alerts > 0 ? 'destructive' : 'neutral'} delay={0.15} hint="离线或隔离节点" />
      </div>

      {loading ? (
        <div className="rounded-xl border bg-card p-10 text-center text-sm text-muted-foreground">正在加载上游源...</div>
      ) : filtered.length === 0 ? (
        <EmptyState icon={Server} title="没有匹配的上游源" description="调整搜索条件，或添加一个新的上游源。" />
      ) : (
        <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
          {filtered.map((source, index) => (
            <motion.div
              key={source.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: index * 0.04, ease: [0.22, 1, 0.36, 1] }}
            >
              <Card
                className={cn(
                  'group relative h-full overflow-hidden card-hover',
                  source.status === 'disabled' && 'opacity-65 grayscale-[0.35]',
                )}
              >
                <CardContent className="flex h-full flex-col gap-5 p-5">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-start gap-3">
                      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary shadow-soft transition-transform duration-500 group-hover:rotate-3 group-hover:scale-110">
                        <Server className="h-5 w-5" />
                      </span>
                      <div className="min-w-0">
                        {sourceSupportsAccountPool(source) ? (
                          <Link
                            to={`/admin/sources/${source.id}/accounts`}
                            className="inline-flex max-w-full items-center gap-1.5 truncate text-base font-semibold hover:text-primary transition-colors"
                          >
                            <span className="truncate">{source.name}</span>
                            <Link2 className="h-3.5 w-3.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
                          </Link>
                        ) : (
                          <span className="block max-w-full truncate text-base font-semibold">{source.name}</span>
                        )}
                        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                          <StatusDot
                            tone={checkingSources.has(source.id) ? 'idle' : statusDot(source.status)}
                            pulsing={checkingSources.has(source.id) || source.status === 'online'}
                          />
                          <span>{sourceTypeLabel(source.type)}</span>
                        </div>
                      </div>
                    </div>
                    <StatusBadge
                      tone={checkingSources.has(source.id) || isCoolingSource(source) ? 'neutral' : statusTone(source.status)}
                      label={checkingSources.has(source.id) ? '检测中' : isCoolingSource(source) ? '冷却中' : statusLabel(source.status)}
                    />
                  </div>

                  <div className="rounded-lg border bg-muted/25 p-3 group-hover:bg-muted/40 transition-colors">
                    <div className="truncate font-mono text-[11px] text-muted-foreground">{source.apiBase}</div>
                  </div>

                  <div className="grid gap-3 text-sm">
                    <div className="flex items-center justify-between">
                      {sourceSupportsAccountPool(source) ? (
                        <>
                          <span className="inline-flex items-center gap-1.5 text-muted-foreground text-xs font-bold uppercase tracking-wider">
                            <KeyRound className="h-3.5 w-3.5" />
                            账号数
                          </span>
                          <span className="font-mono font-bold">{source.accountCount}</span>
                        </>
                      ) : (
                        <>
                          <span className="inline-flex items-center gap-1.5 text-muted-foreground text-xs font-bold uppercase tracking-wider">
                            <KeyRound className="h-3.5 w-3.5" />
                            认证方式
                          </span>
                          <span className="font-bold text-muted-foreground">API Key</span>
                        </>
                      )}
                    </div>
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
                        <span>当前负载</span>
                        <span className="font-mono">{source.status === 'online' ? `${source.load}%` : '--'}</span>
                      </div>
                      <Progress value={source.status === 'online' ? source.load : 0} className="h-1 bg-muted/40" />
                    </div>
                    <div className="flex items-center justify-between text-xs font-bold">
                      <span className="text-muted-foreground uppercase tracking-wider">响应延迟</span>
                      <span className={cn(
                        "font-mono",
                        source.status === 'online' && source.latencyMs < 200 ? "text-emerald-500" : 
                        source.status === 'online' && source.latencyMs < 500 ? "text-amber-500" : "text-muted-foreground"
                      )}>
                        {source.status === 'online' ? `${source.latencyMs}ms` : '--'}
                      </span>
                    </div>
                    <div className="grid gap-2 rounded-lg border bg-background/60 p-2 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="text-muted-foreground">连续失败</span>
                        <span className={cn('font-mono font-bold', (source.failureCount ?? 0) > 0 && 'text-destructive')}>
                          {source.failureCount ?? 0}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-muted-foreground">最近成功</span>
                        <span className="truncate text-right">{shortDateTime(source.lastSuccessAt)}</span>
                      </div>
                      {source.cooldownUntil && (
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-muted-foreground">冷却到</span>
                          <span className="truncate text-right text-amber-600">{shortDateTime(source.cooldownUntil)}</span>
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="mt-auto flex items-center justify-between border-t pt-4">
                    {sourceSupportsAccountPool(source) ? (
                      <Button asChild variant="outline" size="sm" className="h-8 rounded-lg font-bold">
                        <Link to={`/admin/sources/${source.id}/accounts`}>
                          管理账号
                          <KeyRound className="ml-1.5 h-3.5 w-3.5" />
                        </Link>
                      </Button>
                    ) : sourceSupportsKeyPool(source) ? (
                      <Button variant="outline" size="sm" className="h-8 rounded-lg font-bold" onClick={() => openKeyDialog(source)}>
                        管理 Key
                        <KeyRound className="ml-1.5 h-3.5 w-3.5" />
                      </Button>
                    ) : (
                      <span className="rounded-lg border bg-muted/30 px-3 py-1.5 text-xs font-bold text-muted-foreground">
                        API Key 配置
                      </span>
                    )}
                    <div className="flex gap-1">
                      {source.status !== 'disabled' && (isCoolingSource(source) || (source.failureCount ?? 0) > 0) && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-emerald-600"
                          onClick={() => recoverSource(source.id)}
                        >
                          <ShieldCheck className="h-4 w-4" />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-muted-foreground hover:text-foreground"
                        disabled={source.status === 'disabled' || checkingSources.has(source.id)}
                        onClick={() => checkSource(source.id)}
                      >
                        <RefreshCw className={cn('h-4 w-4', checkingSources.has(source.id) && 'animate-spin')} />
                      </Button>
                      <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground" onClick={() => handleEdit(source)}>
                        <Edit className="h-4 w-4" />
                      </Button>
                      <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-primary" onClick={() => toggleSource(source.id)}>
                        <Power className="h-4 w-4" />
                      </Button>
                      {sourceSupportsKeyPool(source) && (
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-destructive transition-colors">
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>删除上游源 {source.name}?</AlertDialogTitle>
                              <AlertDialogDescription>
                                删除后该源下模型路由都将不可用。该操作会立即提交到后端。
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>取消</AlertDialogCancel>
                              <AlertDialogAction
                                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                onClick={() => removeSource(source.id)}
                              >
                                确认删除
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}
