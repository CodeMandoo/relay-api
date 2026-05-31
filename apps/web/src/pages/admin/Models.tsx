import { useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Button,
  Card,
  CardContent,
  Input,
  Label,
  Switch,
  Checkbox,
  Badge,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  cn,
} from '@relay-api/ui';
import {
  Search,
  RefreshCw,
  Plus,
  MoreHorizontal,
  Edit,
  Copy,
  Trash2,
  Layers,
  Power,
  PowerOff,
  Cpu,
  Sparkles,
  Network,
} from 'lucide-react';
import { MODEL_PROVIDERS, copyToClipboard } from '@relay-api/lib';
import type { ModelFormat, PlatformModel, SourceKey, UpstreamSource } from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { StatusBadge } from '@/components/common/StatusBadge';

type Provider = PlatformModel['provider'];
type Filter = 'all' | Provider | 'disabled';

const MODEL_FORMAT_OPTIONS: { value: ModelFormat; label: string }[] = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
];

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: '全部' },
  ...MODEL_PROVIDERS.map((provider) => ({ key: provider, label: provider })),
  { key: 'disabled', label: '已禁用' },
];

const defaultFormatsForProvider = (provider: Provider): ModelFormat[] => (provider === 'Anthropic' ? ['anthropic'] : ['openai']);

const modelFormats = (model: PlatformModel): ModelFormat[] =>
  model.formats?.length ? model.formats : defaultFormatsForProvider(model.provider);

const routeCandidateTone = (candidate: NonNullable<PlatformModel['routingCandidates']>[number]): string => {
  if (!candidate.modelEnabled || !candidate.routingEnabled) return 'text-muted-foreground';
  if (candidate.coolingDown || candidate.sourceStatus === 'offline') return 'text-amber-600 dark:text-amber-400';
  if (candidate.sourceStatus === 'disabled') return 'text-destructive';
  return 'text-emerald-600 dark:text-emerald-400';
};

export default function Page() {
  const [models, setModels] = useState<PlatformModel[]>([]);
  const [sources, setSources] = useState<UpstreamSource[]>([]);
  const [sourceKeys, setSourceKeys] = useState<SourceKey[]>([]);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [syncing, setSyncing] = useState(false);
  const [sourceKeyLoading, setSourceKeyLoading] = useState(false);
  
  const [addOpen, setAddOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<PlatformModel | null>(null);

  // Form state
  const [newName, setNewName] = useState('');
  const [newSource, setNewSource] = useState('');
  const [newSourceKey, setNewSourceKey] = useState('default');
  const [newProvider, setNewProvider] = useState<Provider>('OpenAI');
  const [newFormats, setNewFormats] = useState<ModelFormat[]>(['openai']);
  const [newRoutingWeight, setNewRoutingWeight] = useState(1);
  const [newRoutingEnabled, setNewRoutingEnabled] = useState(true);

  const reloadModels = () => {
    setSyncing(true);
    Promise.all([adminApi.models(), adminApi.sources()])
      .then(([modelResponse, sourceResponse]) => {
        setModels(modelResponse.data);
        setSources(sourceResponse.data);
        if (!newSource && sourceResponse.data[0]) {
          setNewSource(sourceResponse.data[0].id);
        }
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载模型配置失败')))
      .finally(() => setSyncing(false));
  };

  useEffect(() => {
    reloadModels();
  }, []);

  useEffect(() => {
    if (!addOpen && !editOpen) return;
    const source = sources.find((item) => item.id === newSource);
    if (!source || source.type === 'CLIProxyAPI') {
      setSourceKeys([]);
      setSourceKeyLoading(false);
      setNewSourceKey('default');
      return;
    }
    let ignore = false;
    setSourceKeyLoading(true);
    adminApi
      .sourceKeys(source.id)
      .then((response) => {
        if (ignore) return;
        setSourceKeys(response.data);
        setNewSourceKey((current) =>
          current !== 'default' && !response.data.some((key) => key.id === current) ? 'default' : current,
        );
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载上游 Key 失败')))
      .finally(() => {
        if (!ignore) setSourceKeyLoading(false);
      });
    return () => {
      ignore = true;
    };
  }, [addOpen, editOpen, newSource, sources]);

  const filtered = useMemo(() => {
    return models.filter((m) => {
      if (search && !m.name.toLowerCase().includes(search.toLowerCase())) return false;
      if (filter === 'all') return true;
      if (filter === 'disabled') return !m.enabled;
      return m.provider === filter;
    });
  }, [models, search, filter]);

  const stats = useMemo(() => {
    const enabled = models.filter((m) => m.enabled).length;
    const disabled = models.length - enabled;
    const sources = new Set(models.map((m) => m.sourceName)).size;
    return { total: models.length, enabled, disabled, sources };
  }, [models]);

  const allChecked = filtered.length > 0 && filtered.every((m) => selected.has(m.id));
  const someChecked = filtered.some((m) => selected.has(m.id)) && !allChecked;
  const hasSelection = selected.size > 0;

  const toggleAll = () => {
    if (allChecked) {
      const next = new Set(selected);
      filtered.forEach((m) => next.delete(m.id));
      setSelected(next);
    } else {
      const next = new Set(selected);
      filtered.forEach((m) => next.add(m.id));
      setSelected(next);
    }
  };

  const toggleOne = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  const toggleModelFormat = (format: ModelFormat) => {
    setNewFormats((current) => {
      if (current.includes(format)) {
        return current.length === 1 ? current : current.filter((item) => item !== format);
      }
      return [...current, format];
    });
  };

  const runBatch = async (action: 'enable' | 'disable' | 'delete') => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    try {
      await adminApi.batchModels(ids, action);
      if (action === 'delete') {
        setModels((prev) => prev.filter((model) => !selected.has(model.id)));
      } else {
        setModels((prev) =>
          prev.map((model) => (selected.has(model.id) ? { ...model, enabled: action === 'enable' } : model)),
        );
      }
      setSelected(new Set());
      toast.success(`已处理 ${ids.length} 个模型`);
    } catch (error) {
      toast.error(getErrorMessage(error, '批量操作失败'));
    }
  };

  const handleSync = async () => {
    await toast.promise(
      Promise.all([adminApi.models(), adminApi.sources()]).then(([modelResponse, sourceResponse]) => {
        setModels(modelResponse.data);
        setSources(sourceResponse.data);
        return modelResponse.data.length;
      }),
      {
        loading: '正在同步模型配置...',
        success: (count) => `成功同步 ${count} 个模型配置`,
        error: (error) => getErrorMessage(error, '同步失败'),
      },
    );
  };

  const [syncingPricing, setSyncingPricing] = useState(false);
  const handleSyncPricing = async () => {
    setSyncingPricing(true);
    try {
      const response = await adminApi.syncPricing();
      const result = response.result;
      const parts = [`已同步 ${result.synced} 个模型定价`];
      if (result.skipped > 0) parts.push(`${result.skipped} 个未匹配`);
      if (result.errors && result.errors.length > 0) parts.push(`${result.errors.length} 个错误`);
      toast.success(parts.join('，'));
      // Refresh model list to show updated pricing
      const modelResponse = await adminApi.models();
      setModels(modelResponse.data);
    } catch (error) {
      toast.error(getErrorMessage(error, '同步定价失败'));
    } finally {
      setSyncingPricing(false);
    }
  };

  const toggleEnabled = async (id: string) => {
    const model = models.find((item) => item.id === id);
    if (!model) return;
    try {
      const response = await adminApi.updateModel(id, { enabled: !model.enabled });
      setModels((prev) => prev.map((m) => (m.id === id ? response.data : m)));
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型状态失败'));
    }
  };

  const toggleRouting = async (model: PlatformModel) => {
    try {
      const response = await adminApi.updateModel(model.id, { routingEnabled: !model.routingEnabled });
      setModels((prev) => prev.map((m) => (m.id === model.id ? response.data : m)));
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型调度状态失败'));
    }
  };

  const deleteModel = async (id: string) => {
    try {
      await adminApi.deleteModel(id);
      setModels((prev) => prev.filter((m) => m.id !== id));
      setSelected((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
      });
      toast.success('模型已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除模型失败'));
    }
  };

  const handleEdit = (m: PlatformModel) => {
    setEditingModel(m);
    setNewSource(m.sourceId);
    setNewSourceKey(m.sourceKeyId ?? 'default');
    setNewFormats(modelFormats(m));
    setNewRoutingWeight(m.routingWeight || 1);
    setNewRoutingEnabled(m.routingEnabled);
    setEditOpen(true);
  };

  const saveEdit = async () => {
    if (!editingModel) return;
    try {
      const response = await adminApi.updateModel(editingModel.id, {
        sourceId: newSource,
        sourceKeyId: newSourceKey === 'default' ? 'default' : newSourceKey,
        formats: newFormats,
        routingWeight: newRoutingWeight,
        routingEnabled: newRoutingEnabled,
      });
      setModels((prev) => prev.map((m) => (m.id === editingModel.id ? response.data : m)));
      toast.success('模型配置已更新');
      setEditOpen(false);
      setEditingModel(null);
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型配置失败'));
    }
  };

  const handleAdd = async () => {
    if (!newName.trim()) {
      toast.error('请输入模型名称');
      return;
    }
    const sourceId = newSource || sources[0]?.id;
    if (!sourceId) {
      toast.error('请先创建上游源');
      return;
    }
    try {
      const response = await adminApi.createModel({
        name: newName.trim(),
        sourceId,
        sourceKeyId: newSourceKey === 'default' ? undefined : newSourceKey,
        provider: newProvider,
        formats: newFormats,
        enabled: true,
        routingWeight: newRoutingWeight,
        routingEnabled: newRoutingEnabled,
      });
      setModels((prev) => [response.data, ...prev]);
      toast.success('自定义模型已添加', { description: response.data.name });
      setAddOpen(false);
      setNewName('');
      setNewSourceKey('default');
      setNewFormats(defaultFormatsForProvider(newProvider));
      setNewRoutingWeight(1);
      setNewRoutingEnabled(true);
    } catch (error) {
      toast.error(getErrorMessage(error, '添加模型失败'));
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="模型治理"
        title="模型配置"
        description="管理可被代理调用的模型清单、上游绑定与启用状态。"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索模型名称..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 w-64 pl-9"
              />
            </div>
            <Button variant="outline" onClick={handleSync} disabled={syncing}>
              <RefreshCw className={cn('mr-2 h-4 w-4', syncing && 'animate-spin')} />
              同步上游模型
            </Button>
            <Button variant="outline" onClick={handleSyncPricing} disabled={syncingPricing}>
              <RefreshCw className={cn('mr-2 h-4 w-4', syncingPricing && 'animate-spin')} />
              从 LiteLLM 同步定价
            </Button>
            <Dialog open={addOpen} onOpenChange={setAddOpen}>
              <DialogTrigger asChild>
                <Button className="shadow-sm">
                  <Plus className="mr-2 h-4 w-4" />
                  添加自定义模型
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>添加自定义模型</DialogTitle>
                  <DialogDescription>注册一个不在上游列表中的模型，便于内部路由。</DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                  <div className="grid gap-2">
                    <Label htmlFor="m-name">模型名称</Label>
                    <Input id="m-name" placeholder="e.g. my-llama-3-finetune" value={newName} onChange={(e) => setNewName(e.target.value)} />
                  </div>
                  <div className="grid gap-2">
                    <Label>上游源</Label>
                    <Select value={newSource} onValueChange={(value) => { setNewSource(value); setNewSourceKey('default'); }}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {sources.map((source) => (
                          <SelectItem key={source.id} value={source.id}>
                            {source.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label>API Key 绑定</Label>
                    <Select value={newSourceKey} onValueChange={setNewSourceKey} disabled={sourceKeyLoading || sourceKeys.length === 0}>
                      <SelectTrigger><SelectValue placeholder="默认上游 Key" /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="default">默认上游 Key</SelectItem>
                        {sourceKeys.map((key) => (
                          <SelectItem key={key.id} value={key.id} disabled={key.status !== 'valid'}>
                            {key.alias}{key.status !== 'valid' ? '（已禁用）' : ''}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      {sourceKeyLoading ? '正在加载 Key...' : sourceKeys.length === 0 ? '该上游源暂无可绑定 Key，将使用默认 API Key。' : '绑定后该模型优先使用所选 Key 转发。'}
                    </p>
                  </div>
                  <div className="grid gap-2">
                    <Label>模型提供商</Label>
                    <Select
                      value={newProvider}
                      onValueChange={(v) => {
                        const provider = v as Provider;
                        setNewProvider(provider);
                        setNewFormats(defaultFormatsForProvider(provider));
                      }}
                    >
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {MODEL_PROVIDERS.map((provider) => (
                          <SelectItem key={provider} value={provider}>
                            {provider}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label>支持格式</Label>
                    <div className="grid gap-2 rounded-lg border bg-muted/20 p-3">
                      {MODEL_FORMAT_OPTIONS.map((option) => (
                        <label key={option.value} className="flex cursor-pointer items-center gap-3 text-sm font-medium">
                          <Checkbox checked={newFormats.includes(option.value)} onCheckedChange={() => toggleModelFormat(option.value)} />
                          <span>{option.label}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                  <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
                    <div className="grid gap-2">
                      <Label htmlFor="m-routing-weight">调度权重</Label>
                      <Input
                        id="m-routing-weight"
                        type="number"
                        min={1}
                        value={newRoutingWeight}
                        onChange={(e) => setNewRoutingWeight(Math.max(1, Number(e.target.value) || 1))}
                      />
                    </div>
                    <label className="flex items-center justify-between gap-3 text-sm font-medium">
                      <span>参与自动调度</span>
                      <Switch checked={newRoutingEnabled} onCheckedChange={setNewRoutingEnabled} />
                    </label>
                  </div>
                </div>
                <DialogFooter>
                  <DialogClose asChild>
                    <Button variant="ghost">取消</Button>
                  </DialogClose>
                  <Button onClick={handleAdd}>保存模型</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
        }
      />

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑模型配置</DialogTitle>
            <DialogDescription>
              调整模型 <span className="font-mono font-bold text-foreground">{editingModel?.name}</span> 的上游绑定。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label>上游源</Label>
              <Select value={newSource} onValueChange={(value) => { setNewSource(value); setNewSourceKey('default'); }}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {sources.map((source) => (
                    <SelectItem key={source.id} value={source.id}>
                      {source.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label>API Key 绑定</Label>
              <Select value={newSourceKey} onValueChange={setNewSourceKey} disabled={sourceKeyLoading || sourceKeys.length === 0}>
                <SelectTrigger><SelectValue placeholder="默认上游 Key" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">默认上游 Key</SelectItem>
                  {sourceKeys.map((key) => (
                    <SelectItem key={key.id} value={key.id} disabled={key.status !== 'valid'}>
                      {key.alias}{key.status !== 'valid' ? '（已禁用）' : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label>支持格式</Label>
              <div className="grid gap-2 rounded-lg border bg-muted/20 p-3">
                {MODEL_FORMAT_OPTIONS.map((option) => (
                  <label key={option.value} className="flex cursor-pointer items-center gap-3 text-sm font-medium">
                    <Checkbox checked={newFormats.includes(option.value)} onCheckedChange={() => toggleModelFormat(option.value)} />
                    <span>{option.label}</span>
                  </label>
                ))}
              </div>
            </div>
            <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
              <div className="grid gap-2">
                <Label htmlFor="edit-routing-weight">调度权重</Label>
                <Input
                  id="edit-routing-weight"
                  type="number"
                  min={1}
                  value={newRoutingWeight}
                  onChange={(e) => setNewRoutingWeight(Math.max(1, Number(e.target.value) || 1))}
                />
              </div>
              <label className="flex items-center justify-between gap-3 text-sm font-medium">
                <span>参与自动调度</span>
                <Switch checked={newRoutingEnabled} onCheckedChange={setNewRoutingEnabled} />
              </label>
            </div>
          </div>
          <DialogFooter>
             <Button variant="ghost" onClick={() => setEditOpen(false)}>取消</Button>
             <Button onClick={saveEdit}>确认修改</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="模型总数" value={stats.total} icon={Layers} tone="primary" delay={0.02} hint="可被路由调度的模型" />
        <StatCard label="已启用" value={stats.enabled} icon={Power} tone="success" delay={0.06} hint="对终端用户可见" />
        <StatCard label="已禁用" value={stats.disabled} icon={PowerOff} tone="warning" delay={0.1} hint="临时下架或测试中" />
        <StatCard label="上游覆盖" value={stats.sources} icon={Sparkles} tone="neutral" delay={0.14} hint="独立上游源数量" />
      </div>

      <div className="flex items-center gap-3">
        <Select value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <SelectTrigger className="h-10 w-56 rounded-xl bg-muted/40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FILTERS.map((f) => (
              <SelectItem key={f.key} value={f.key}>
                {f.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <Card className="overflow-hidden">
        <div className="flex min-h-[64px] flex-wrap items-center justify-between gap-3 border-b bg-card px-4 py-3">
          <div className="text-sm font-semibold">
            已选择 <span className="font-mono font-bold">{selected.size}</span> 个模型
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => runBatch('enable')} disabled={!hasSelection}>
              <Power className="mr-1.5 h-3.5 w-3.5" />
              批量启用
            </Button>
            <Button variant="outline" size="sm" onClick={() => runBatch('disable')} disabled={!hasSelection}>
              <PowerOff className="mr-1.5 h-3.5 w-3.5" />
              批量禁用
            </Button>
            <Button variant="destructive" size="sm" onClick={() => runBatch('delete')} disabled={!hasSelection}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              删除
            </Button>
          </div>
        </div>
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/40 hover:bg-muted/40">
              <TableHead className="w-10">
                <Checkbox
                  checked={someChecked ? 'indeterminate' : allChecked}
                  onCheckedChange={toggleAll}
                />
              </TableHead>
              <TableHead>模型名称</TableHead>
              <TableHead>上游源</TableHead>
              <TableHead>路由候选</TableHead>
              <TableHead>支持格式</TableHead>
              <TableHead className="text-center">状态</TableHead>
              <TableHead className="w-20 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((m, i) => (
              <motion.tr
                key={m.id}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.02, ease: [0.22, 1, 0.36, 1] }}
                className={cn(
                  'group border-b transition-colors hover:bg-muted/40',
                  !m.enabled && 'opacity-60',
                )}
              >
                <TableCell>
                  <Checkbox checked={selected.has(m.id)} onCheckedChange={() => toggleOne(m.id)} />
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2.5">
                    <ProviderIcon provider={m.provider} />
                    <div>
                      <div className="font-mono text-sm font-medium">{m.name}</div>
                      <div className="text-xs text-muted-foreground">{m.provider}</div>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex min-w-0 flex-col items-start gap-1">
                    <Badge variant="secondary" className="max-w-[220px] truncate font-mono text-[11px]">{m.sourceName}</Badge>
                    {m.sourceKeyAlias && (
                      <span className="max-w-[220px] truncate rounded-md bg-muted px-2 py-0.5 font-mono text-[10px] text-muted-foreground">
                        Key: {m.sourceKeyAlias}
                      </span>
                    )}
                    <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-[10px] text-muted-foreground">
                      <Network className="h-3 w-3" />
                      权重 {m.routingWeight || 1} · {m.routingEnabled ? '参与调度' : '不参与调度'}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex max-w-[320px] flex-col gap-1.5">
                    <Badge variant={m.candidateCount && m.candidateCount > 1 ? 'default' : 'secondary'} className="w-fit text-[10px]">
                      候选 {m.candidateCount ?? 1}
                    </Badge>
                    {(m.routingCandidates ?? []).slice(0, 3).map((candidate) => (
                      <div key={candidate.id} className="flex min-w-0 items-center gap-1.5 text-[11px]">
                        <span className={cn('h-1.5 w-1.5 rounded-full bg-current', routeCandidateTone(candidate))} />
                        <span className="truncate">{candidate.sourceName || candidate.sourceId}</span>
                        <span className="shrink-0 font-mono text-muted-foreground">
                          P{candidate.sourcePriority} W{candidate.routingWeight}
                        </span>
                      </div>
                    ))}
                    {(m.routingCandidates?.length ?? 0) > 3 && (
                      <div className="text-[11px] text-muted-foreground">+{(m.routingCandidates?.length ?? 0) - 3} 个候选</div>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {modelFormats(m).map((format) => (
                      <Badge
                        key={format}
                        variant="outline"
                        className={cn(
                          'h-6 rounded-md px-2 py-0 font-mono text-[10px] font-bold uppercase tracking-wider shadow-none',
                          format === 'openai'
                            ? 'border-sky-500/20 bg-sky-500/5 text-sky-600'
                            : 'border-orange-500/20 bg-orange-500/5 text-orange-600',
                        )}
                      >
                        {format}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className="text-center">
                  <div className="inline-flex items-center gap-2">
                    <Switch checked={m.enabled} onCheckedChange={() => toggleEnabled(m.id)} />
                    <StatusBadge tone={m.enabled ? 'success' : 'neutral'} label={m.enabled ? '启用' : '已禁用'} />
                  </div>
                </TableCell>
                <TableCell className="text-right">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" className="h-8 w-8">
                        <MoreHorizontal className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => handleEdit(m)}>
                        <Edit className="mr-2 h-4 w-4" />编辑配置
                      </DropdownMenuItem>
                      <DropdownMenuItem onClick={() => toggleRouting(m)}>
                        {m.routingEnabled ? <PowerOff className="mr-2 h-4 w-4" /> : <Power className="mr-2 h-4 w-4" />}
                        {m.routingEnabled ? '暂停调度' : '参与调度'}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={async () => {
                          await copyToClipboard(m.name);
                          toast.success('已复制', { description: m.name });
                        }}
                      >
                        <Copy className="mr-2 h-4 w-4" />复制名称
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <AlertDialog>
                        <AlertDialogTrigger asChild>
                          <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={(e) => e.preventDefault()}>
                            <Trash2 className="mr-2 h-4 w-4" />删除
                          </DropdownMenuItem>
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                          <AlertDialogHeader>
                            <AlertDialogTitle>删除模型 {m.name}?</AlertDialogTitle>
                            <AlertDialogDescription>
                              此操作不可撤销。该模型将从路由列表移除，所有引用此模型的 API 请求将返回 404。
                            </AlertDialogDescription>
                          </AlertDialogHeader>
                          <AlertDialogFooter>
                            <AlertDialogCancel>取消</AlertDialogCancel>
                            <AlertDialogAction onClick={() => deleteModel(m.id)} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                              确认删除
                            </AlertDialogAction>
                          </AlertDialogFooter>
                        </AlertDialogContent>
                      </AlertDialog>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </motion.tr>
            ))}
            {filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={7}>
                  <div className="py-12 text-center text-sm text-muted-foreground">
                    <Cpu className="mx-auto mb-2 h-8 w-8 opacity-40" />
                    没有匹配的模型
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <CardContent className="flex items-center justify-between border-t bg-muted/20 py-3 text-xs text-muted-foreground">
          <div>显示 {filtered.length} 条 · 共 {models.length} 个模型</div>
          <div className="text-[11px] uppercase tracking-wider">同步周期 · 每 6 小时</div>
        </CardContent>
      </Card>
    </div>
  );
}
