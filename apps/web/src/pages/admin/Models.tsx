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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
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
  Info,
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

type RouteCandidate = NonNullable<PlatformModel['routingCandidates']>[number];
type ModelBindingDraft = {
  clientId: string;
  id?: string;
  sourceId: string;
  sourceKeyId: string;
  routingWeight: number;
};
type ModelGroup = {
  key: string;
  name: string;
  provider: Provider;
  formats: ModelFormat[];
  enabled: boolean;
  ids: string[];
  models: PlatformModel[];
  candidates: RouteCandidate[];
  currentCandidate?: RouteCandidate;
};

const publicIdNumber = (id: string) => Number(id.split('_').pop() ?? 0) || 0;

const routeCandidateWeight = (candidate: RouteCandidate) => Math.max(1, candidate.routingWeight || 1);

const isRouteCandidateSchedulable = (candidate: RouteCandidate) =>
  candidate.modelEnabled && candidate.sourceStatus === 'online' && !candidate.coolingDown;

const routeCandidateOrder = (left: RouteCandidate, right: RouteCandidate) => {
  const leftSchedulable = isRouteCandidateSchedulable(left);
  const rightSchedulable = isRouteCandidateSchedulable(right);
  if (leftSchedulable !== rightSchedulable) return leftSchedulable ? -1 : 1;
  if (left.sourcePriority !== right.sourcePriority) return left.sourcePriority - right.sourcePriority;
  const weightDiff = routeCandidateWeight(right) - routeCandidateWeight(left);
  if (weightDiff !== 0) return weightDiff;
  return publicIdNumber(left.id) - publicIdNumber(right.id);
};

const sortedRouteCandidates = (model: PlatformModel) => [...(model.routingCandidates ?? [])].sort(routeCandidateOrder);

const currentRouteCandidate = (model: PlatformModel) =>
  sortedRouteCandidates(model).find(isRouteCandidateSchedulable);

const routeCandidateTone = (candidate: NonNullable<PlatformModel['routingCandidates']>[number]): string => {
  if (!candidate.modelEnabled) return 'text-muted-foreground';
  if (candidate.coolingDown || candidate.sourceStatus === 'offline') return 'text-amber-600 dark:text-amber-400';
  if (candidate.sourceStatus === 'disabled') return 'text-destructive';
  return 'text-emerald-600 dark:text-emerald-400';
};

function RoutingRuleHint() {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="inline-flex h-5 w-5 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground">
          <Info className="h-3.5 w-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" align="start" className="max-w-sm text-xs leading-relaxed">
        启用状态即参与调度。先过滤未启用、上游非在线和冷却中候选；再按源优先级 P 从小到大，同 P 按权重 W 从大到小；失败、超时、429 或 5xx 时切到下一个候选。
      </TooltipContent>
    </Tooltip>
  );
}

const makeBindingDraft = (sourceId = '', overrides: Partial<ModelBindingDraft> = {}): ModelBindingDraft => ({
  clientId: `binding_${Date.now()}_${Math.random().toString(36).slice(2)}`,
  sourceId,
  sourceKeyId: 'default',
  routingWeight: 1,
  ...overrides,
});

const buildModelGroups = (models: PlatformModel[]): ModelGroup[] => {
  const grouped = new Map<string, PlatformModel[]>();
  for (const model of models) {
    grouped.set(model.name, [...(grouped.get(model.name) ?? []), model]);
  }
  return Array.from(grouped.entries())
    .map(([name, rows]) => {
      const candidates = sortedRouteCandidates(rows[0]);
      const currentCandidate = currentRouteCandidate(rows[0]);
      const currentModel = rows.find((row) => row.id === currentCandidate?.id) ?? rows[0];
      return {
        key: name,
        name,
        provider: currentModel.provider,
        formats: modelFormats(currentModel),
        enabled: rows.some((row) => row.enabled),
        ids: rows.map((row) => row.id),
        models: rows,
        candidates,
        currentCandidate,
      };
    })
    .sort((left, right) => left.name.localeCompare(right.name));
};

export default function Page() {
  const [models, setModels] = useState<PlatformModel[]>([]);
  const [sources, setSources] = useState<UpstreamSource[]>([]);
  const [sourceKeysBySource, setSourceKeysBySource] = useState<Record<string, SourceKey[]>>({});
  const [sourceKeyLoadingBySource, setSourceKeyLoadingBySource] = useState<Record<string, boolean>>({});
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [syncing, setSyncing] = useState(false);
  
  const [addOpen, setAddOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<ModelGroup | null>(null);

  // Form state
  const [newName, setNewName] = useState('');
  const [newProvider, setNewProvider] = useState<Provider>('OpenAI');
  const [newFormats, setNewFormats] = useState<ModelFormat[]>(['openai']);
  const [newBindings, setNewBindings] = useState<ModelBindingDraft[]>([]);

  const reloadModels = () => {
    setSyncing(true);
    return Promise.all([adminApi.models(), adminApi.sources()])
      .then(([modelResponse, sourceResponse]) => {
        setModels(modelResponse.data);
        setSources(sourceResponse.data);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载模型配置失败')))
      .finally(() => setSyncing(false));
  };

  useEffect(() => {
    reloadModels();
  }, []);

  const ensureSourceKeys = (sourceId: string) => {
    if (!sourceId || sourceKeysBySource[sourceId] || sourceKeyLoadingBySource[sourceId]) return;
    const source = sources.find((item) => item.id === sourceId);
    if (!source || source.type === 'CLIProxyAPI') {
      setSourceKeysBySource((current) => ({ ...current, [sourceId]: [] }));
      return;
    }
    setSourceKeyLoadingBySource((current) => ({ ...current, [sourceId]: true }));
    adminApi
      .sourceKeys(sourceId)
      .then((response) => setSourceKeysBySource((current) => ({ ...current, [sourceId]: response.data })))
      .catch((error) => toast.error(getErrorMessage(error, '加载上游 Key 失败')))
      .finally(() => setSourceKeyLoadingBySource((current) => ({ ...current, [sourceId]: false })));
  };

  useEffect(() => {
    if (!addOpen && !editOpen) return;
    newBindings.forEach((binding) => ensureSourceKeys(binding.sourceId));
  }, [addOpen, editOpen, newBindings, sources, sourceKeysBySource, sourceKeyLoadingBySource]);

  const modelGroups = useMemo(() => buildModelGroups(models), [models]);

  const filtered = useMemo(() => {
    return modelGroups
      .filter((m) => {
        if (search && !m.name.toLowerCase().includes(search.toLowerCase())) return false;
        if (filter === 'all') return true;
        if (filter === 'disabled') return !m.enabled;
        return m.provider === filter;
      });
  }, [modelGroups, search, filter]);

  const stats = useMemo(() => {
    const enabled = modelGroups.filter((m) => m.enabled).length;
    const disabled = modelGroups.length - enabled;
    const sources = new Set(models.map((m) => m.sourceName)).size;
    return { total: modelGroups.length, enabled, disabled, sources };
  }, [models, modelGroups]);

  const allChecked = filtered.length > 0 && filtered.every((m) => selected.has(m.key));
  const someChecked = filtered.some((m) => selected.has(m.key)) && !allChecked;
  const hasSelection = selected.size > 0;

  const toggleAll = () => {
    if (allChecked) {
      const next = new Set(selected);
      filtered.forEach((m) => next.delete(m.key));
      setSelected(next);
    } else {
      const next = new Set(selected);
      filtered.forEach((m) => next.add(m.key));
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

  const resetAddForm = () => {
    const firstSource = sources[0]?.id ?? '';
    setNewName('');
    setNewProvider('OpenAI');
    setNewFormats(['openai']);
    setNewBindings([makeBindingDraft(firstSource)]);
  };

  const updateBinding = (clientId: string, patch: Partial<ModelBindingDraft>) => {
    setNewBindings((current) =>
      current.map((binding) =>
        binding.clientId === clientId
          ? { ...binding, ...patch, sourceKeyId: patch.sourceId && patch.sourceId !== binding.sourceId ? 'default' : patch.sourceKeyId ?? binding.sourceKeyId }
          : binding,
      ),
    );
  };

  const addBinding = () => {
    const used = new Set(newBindings.map((binding) => binding.sourceId));
    const sourceId = sources.find((source) => !used.has(source.id))?.id ?? sources[0]?.id ?? '';
    setNewBindings((current) => [...current, makeBindingDraft(sourceId)]);
  };

  const removeBinding = (clientId: string) => {
    setNewBindings((current) => (current.length > 1 ? current.filter((binding) => binding.clientId !== clientId) : current));
  };

  const validateBindings = () => {
    const validBindings = newBindings.filter((binding) => binding.sourceId);
    if (validBindings.length === 0) {
      toast.error('请至少选择一个上游源');
      return [];
    }
    const seen = new Set<string>();
    for (const binding of validBindings) {
      if (seen.has(binding.sourceId)) {
        toast.error('同一个模型下不能重复选择同一个上游源');
        return [];
      }
      seen.add(binding.sourceId);
    }
    return validBindings;
  };

  const selectedModelIds = () =>
    modelGroups.filter((group) => selected.has(group.key)).flatMap((group) => group.ids);

  const runBatch = async (action: 'enable' | 'disable' | 'delete') => {
    const ids = selectedModelIds();
    if (ids.length === 0) return;
    try {
      await adminApi.batchModels(ids, action);
      if (action === 'delete') {
        setModels((prev) => prev.filter((model) => !ids.includes(model.id)));
      } else {
        setModels((prev) =>
          prev.map((model) => (ids.includes(model.id) ? { ...model, enabled: action === 'enable' } : model)),
        );
      }
      setSelected(new Set());
      toast.success(`已处理 ${selected.size} 个模型`);
    } catch (error) {
      toast.error(getErrorMessage(error, '批量操作失败'));
    }
  };

  const handleSync = async () => {
    await toast.promise(
      Promise.all([adminApi.models(), adminApi.sources()]).then(([modelResponse, sourceResponse]) => {
        setModels(modelResponse.data);
        setSources(sourceResponse.data);
        return buildModelGroups(modelResponse.data).length;
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

  const toggleEnabled = async (group: ModelGroup) => {
    const action = group.enabled ? 'disable' : 'enable';
    try {
      await adminApi.batchModels(group.ids, action);
      setModels((prev) => prev.map((m) => (group.ids.includes(m.id) ? { ...m, enabled: action === 'enable' } : m)));
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型状态失败'));
    }
  };

  const deleteModelGroup = async (group: ModelGroup) => {
    try {
      await adminApi.batchModels(group.ids, 'delete');
      setModels((prev) => prev.filter((m) => !group.ids.includes(m.id)));
      setSelected((prev) => {
          const next = new Set(prev);
          next.delete(group.key);
          return next;
      });
      toast.success('模型已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除模型失败'));
    }
  };

  const handleEdit = (group: ModelGroup) => {
    setEditingGroup(group);
    setNewProvider(group.provider);
    setNewFormats(group.formats);
    setNewBindings(
      group.candidates.map((candidate) =>
        makeBindingDraft(candidate.sourceId, {
          id: candidate.id,
          sourceKeyId: candidate.sourceKeyId ?? 'default',
          routingWeight: routeCandidateWeight(candidate),
        }),
      ),
    );
    setEditOpen(true);
  };

  const saveEdit = async () => {
    if (!editingGroup) return;
    const bindings = validateBindings();
    if (bindings.length === 0) return;
    try {
      await adminApi.updateModel(editingGroup.ids[0], {
        provider: newProvider,
        formats: newFormats,
        bindings: bindings.map((binding) => ({
          id: binding.id,
          sourceId: binding.sourceId,
          sourceKeyId: binding.sourceKeyId === 'default' ? 'default' : binding.sourceKeyId,
          routingWeight: binding.routingWeight,
        })),
      });
      await reloadModels();
      toast.success('模型配置已更新');
      setEditOpen(false);
      setEditingGroup(null);
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型配置失败'));
    }
  };

  const handleAdd = async () => {
    if (!newName.trim()) {
      toast.error('请输入模型名称');
      return;
    }
    const bindings = validateBindings();
    if (bindings.length === 0) return;
    try {
      await adminApi.createModel({
        name: newName.trim(),
        provider: newProvider,
        formats: newFormats,
        enabled: true,
        bindings: bindings.map((binding) => ({
          sourceId: binding.sourceId,
          sourceKeyId: binding.sourceKeyId === 'default' ? undefined : binding.sourceKeyId,
          routingWeight: binding.routingWeight,
        })),
      });
      await reloadModels();
      toast.success('自定义模型已添加', { description: newName.trim() });
      setAddOpen(false);
      resetAddForm();
    } catch (error) {
      toast.error(getErrorMessage(error, '添加模型失败'));
    }
  };

  const renderBindingFields = () => (
    <div className="grid gap-3">
      {newBindings.map((binding, index) => {
        const sourceKeys = sourceKeysBySource[binding.sourceId] ?? [];
        const sourceKeyLoading = sourceKeyLoadingBySource[binding.sourceId];
        const selectedSourceIds = new Set(newBindings.filter((item) => item.clientId !== binding.clientId).map((item) => item.sourceId));
        return (
          <div key={binding.clientId} className="grid gap-3 rounded-lg border bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-2">
              <div className="text-xs font-semibold text-muted-foreground">上游绑定 {index + 1}</div>
              <Button type="button" variant="ghost" size="sm" onClick={() => removeBinding(binding.clientId)} disabled={newBindings.length === 1}>
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                移除
              </Button>
            </div>
            <div className="grid min-w-0 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_112px]">
              <div className="grid min-w-0 gap-2">
                <Label>上游源</Label>
                <Select value={binding.sourceId} onValueChange={(value) => updateBinding(binding.clientId, { sourceId: value })}>
                  <SelectTrigger className="min-w-0 [&>span]:min-w-0 [&>span]:truncate"><SelectValue placeholder="选择上游源" /></SelectTrigger>
                  <SelectContent className="max-w-[var(--radix-select-trigger-width)]">
                    {sources.map((source) => (
                      <SelectItem key={source.id} value={source.id} disabled={selectedSourceIds.has(source.id)}>
                        <span className="block max-w-full truncate">{source.name}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid min-w-0 gap-2">
                <Label>API Key 绑定</Label>
                <Select
                  value={binding.sourceKeyId}
                  onValueChange={(value) => updateBinding(binding.clientId, { sourceKeyId: value })}
                  disabled={sourceKeyLoading || sourceKeys.length === 0}
                >
                  <SelectTrigger className="min-w-0 [&>span]:min-w-0 [&>span]:truncate"><SelectValue placeholder="默认上游 Key" /></SelectTrigger>
                  <SelectContent className="max-w-[var(--radix-select-trigger-width)]">
                    <SelectItem value="default">默认上游 Key</SelectItem>
                    {sourceKeys.map((key) => (
                      <SelectItem key={key.id} value={key.id} disabled={key.status !== 'valid'}>
                        <span className="block max-w-full truncate">{key.alias}{key.status !== 'valid' ? '（已禁用）' : ''}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid min-w-0 gap-2">
                <Label className="inline-flex items-center gap-1.5">
                  调度权重
                  <RoutingRuleHint />
                </Label>
                <Input
                  type="number"
                  min={1}
                  value={binding.routingWeight}
                  onChange={(e) => updateBinding(binding.clientId, { routingWeight: Math.max(1, Number(e.target.value) || 1) })}
                />
              </div>
            </div>
          </div>
        );
      })}
      <Button type="button" variant="outline" onClick={addBinding} disabled={sources.length === 0 || newBindings.length >= sources.length}>
        <Plus className="mr-2 h-4 w-4" />
        添加上游源
      </Button>
    </div>
  );

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
            <Dialog
              open={addOpen}
              onOpenChange={(open) => {
                setAddOpen(open);
                if (open) resetAddForm();
              }}
            >
              <DialogTrigger asChild>
                <Button className="shadow-sm">
                  <Plus className="mr-2 h-4 w-4" />
                  添加自定义模型
                </Button>
              </DialogTrigger>
              <DialogContent className="grid max-h-[calc(100vh-64px)] w-[min(92vw,640px)] max-w-[640px] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden">
                <DialogHeader>
                  <DialogTitle>添加自定义模型</DialogTitle>
                  <DialogDescription>注册一个不在上游列表中的模型，便于内部路由。</DialogDescription>
                </DialogHeader>
                <div className="grid min-h-0 gap-4 overflow-y-auto py-2 pr-1">
                  <div className="grid gap-2">
                    <Label htmlFor="m-name">模型名称</Label>
                    <Input id="m-name" placeholder="e.g. my-llama-3-finetune" value={newName} onChange={(e) => setNewName(e.target.value)} />
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
                  {renderBindingFields()}
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
        <DialogContent className="grid max-h-[calc(100vh-64px)] w-[min(92vw,640px)] max-w-[640px] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden">
          <DialogHeader>
            <DialogTitle>编辑模型配置</DialogTitle>
            <DialogDescription>
              调整模型 <span className="font-mono font-bold text-foreground">{editingGroup?.name}</span> 的上游绑定。
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 gap-4 overflow-y-auto py-4 pr-1">
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
            {renderBindingFields()}
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
              <TableHead>支持格式</TableHead>
              <TableHead className="text-center">状态</TableHead>
              <TableHead className="w-20 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((group, i) => {
              const currentCandidateId = group.currentCandidate?.id;
              return (
                <motion.tr
                  key={group.key}
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: i * 0.02, ease: [0.22, 1, 0.36, 1] }}
                  className={cn(
                    'group border-b transition-colors hover:bg-muted/40',
                    !group.enabled && 'opacity-60',
                  )}
                >
                  <TableCell>
                    <Checkbox checked={selected.has(group.key)} onCheckedChange={() => toggleOne(group.key)} />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2.5">
                      <ProviderIcon provider={group.provider} />
                      <div>
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-mono text-sm font-medium">{group.name}</span>
                        </div>
                        <div className="text-xs text-muted-foreground">{group.provider}</div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex min-w-0 flex-col items-start gap-1.5">
                      {group.candidates.map((candidate) => {
                        const isCurrent = candidate.id === currentCandidateId;
                        const muted = group.candidates.length > 1 && !isCurrent;
                        return (
                          <div
                            key={candidate.id}
                            className={cn(
                              'flex max-w-[320px] flex-wrap items-center gap-1.5 text-[11px]',
                              muted && 'text-muted-foreground',
                            )}
                          >
                            <span className={cn('h-1.5 w-1.5 rounded-full bg-current', muted ? 'text-muted-foreground' : routeCandidateTone(candidate))} />
                            <span className={cn('truncate font-mono', !muted && 'font-semibold text-foreground')}>
                              {candidate.sourceName}
                            </span>
                            {candidate.sourceKeyAlias && (
                              <span className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[10px]">
                                Key: {candidate.sourceKeyAlias}
                              </span>
                            )}
                            <span className="font-mono">W{routeCandidateWeight(candidate)}</span>
                          </div>
                        );
                      })}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {group.formats.map((format) => (
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
                      <Switch checked={group.enabled} onCheckedChange={() => toggleEnabled(group)} />
                      <StatusBadge tone={group.enabled ? 'success' : 'neutral'} label={group.enabled ? '启用' : '已禁用'} />
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
                        <DropdownMenuItem onClick={() => handleEdit(group)}>
                          <Edit className="mr-2 h-4 w-4" />编辑配置
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={async () => {
                            await copyToClipboard(group.name);
                            toast.success('已复制', { description: group.name });
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
                              <AlertDialogTitle>删除模型 {group.name}?</AlertDialogTitle>
                              <AlertDialogDescription>
                                此操作不可撤销。该模型下的所有上游绑定将从路由列表移除，所有引用此模型的 API 请求将返回 404。
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>取消</AlertDialogCancel>
                              <AlertDialogAction onClick={() => deleteModelGroup(group)} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                                确认删除
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </motion.tr>
              );
            })}
            {filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={6}>
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
          <div>显示 {filtered.length} 条 · 共 {modelGroups.length} 个模型</div>
          <div className="text-[11px] uppercase tracking-wider">同步周期 · 每 6 小时</div>
        </CardContent>
      </Card>
    </div>
  );
}
