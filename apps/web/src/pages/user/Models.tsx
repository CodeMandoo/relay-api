import { Fragment, useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { Bot, CheckCircle2, Copy, Loader2, RotateCcw, Search, Zap } from 'lucide-react';
import { toast } from 'sonner';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Input,
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
  Tabs,
  TabsList,
  TabsTrigger,
  cn,
} from '@relay-api/ui';
import { MODEL_PROVIDERS, copyToClipboard } from '@relay-api/lib';
import type { ModelAccessGroup, ModelFormat, UserModel } from '@relay-api/lib';
import { getErrorMessage, userApi } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { StatusDot } from '@/components/common/StatusDot';

type LatencyState = number | 'loading' | 'success';
type InvokeState = 'loading' | 'success' | 'error';
type ProviderFilter = 'all' | UserModel['provider'];
type FormatFilter = 'all' | UserModel['formats'][number];
type GroupFilter = 'all' | string;

type DisplayModel = UserModel & {
  modelGroupId: string;
  modelGroupName: string;
  sourceName: string;
  sourceLabel?: string;
};

type UserModelGroup = {
  id: string;
  name: string;
  description: string;
};

const DEFAULT_PLATFORM_GROUP_ID = 'platform_default';

const PLATFORM_GROUP: UserModelGroup = {
  id: DEFAULT_PLATFORM_GROUP_ID,
  name: '默认分组',
  description: '平台模型默认分组',
};

const providerOptions: { value: ProviderFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  ...MODEL_PROVIDERS.map((provider) => ({ value: provider, label: provider })),
];

const formatTabs: { value: FormatFilter; label: string }[] = [
  { value: 'all', label: '全部格式' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
];

const FALLBACK_PLATFORM_MODELS: UserModel[] = [
  {
    id: 'm_demo_gpt41',
    name: 'gpt-4.1',
    provider: 'OpenAI',
    formats: ['openai'],
    status: 'online',
    latencyMs: 128,
    sourceId: 's_platform_openai',
    sourceName: '平台',
    sourceType: 'Third-party Provider',
    sourceStatus: 'online',
    routingCandidates: 3,
  },
  {
    id: 'm_demo_claude',
    name: 'claude-sonnet-4',
    provider: 'Anthropic',
    formats: ['anthropic'],
    status: 'online',
    latencyMs: 186,
    sourceId: 's_platform_claude',
    sourceName: '平台',
    sourceType: 'CLIProxyAPI',
    sourceStatus: 'online',
    routingCandidates: 2,
  },
  {
    id: 'm_demo_deepseek',
    name: 'deepseek-chat',
    provider: 'DeepSeek',
    formats: ['openai'],
    status: 'offline',
    latencyMs: 0,
    sourceId: 's_platform_deepseek',
    sourceName: '平台',
    sourceType: 'Third-party Provider',
    sourceStatus: 'offline',
    routingCandidates: 1,
  },
];


const latencyClass = (ms: number) => {
  if (ms === 0) return 'text-muted-foreground';
  if (ms < 150) return 'font-bold text-emerald-500';
  if (ms < 300) return 'font-bold text-amber-500';
  return 'font-bold text-destructive';
};

const compactTokens = (tokens: number) => {
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`;
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(1)}K`;
  return `${tokens}`;
};

const modelFormats = (model: UserModel): ModelFormat[] => {
  if (model.formats?.length) return model.formats;
  return model.provider === 'Anthropic' ? ['anthropic'] : ['openai'];
};

const modelGroupFromDTO = (group: ModelAccessGroup): UserModelGroup => {
  return {
    id: group.id,
    name: group.name,
    description: group.description ?? '平台模型分组',
  };
};

const normalizeBackendModel = (model: UserModel, groupMap: Map<string, UserModelGroup>): DisplayModel => {
  const fallbackGroupId = DEFAULT_PLATFORM_GROUP_ID;
  const groupId = model.modelGroupId ?? fallbackGroupId;
  const group = groupMap.get(groupId);
  const groupName = model.modelGroupName ?? group?.name ?? '默认分组';
  return {
    ...model,
    formats: modelFormats(model),
    sourceName: model.sourceName || '平台上游',
    sourceLabel: model.sourceName || '平台上游',
    modelGroupId: groupId,
    modelGroupName: groupName,
  };
};

const ensureGroupsForModels = (groups: UserModelGroup[], models: DisplayModel[]) => {
  const map = new Map(groups.map((group) => [group.id, group]));
  models.forEach((model) => {
    if (map.has(model.modelGroupId)) return;
    map.set(model.modelGroupId, {
      id: model.modelGroupId,
      name: model.modelGroupName,
      description: '平台模型分组',
    });
  });
  return Array.from(map.values());
};

const dedupeById = (rows: DisplayModel[]) => {
  const seen = new Set<string>();
  return rows.filter((model) => {
    if (seen.has(model.id)) return false;
    seen.add(model.id);
    return true;
  });
};

export default function Page() {
  const [provider, setProvider] = useState<ProviderFilter>('all');
  const [format, setFormat] = useState<FormatFilter>('all');
  const [groupFilter, setGroupFilter] = useState<GroupFilter>('all');
  const [query, setQuery] = useState('');
  const [models, setModels] = useState<DisplayModel[]>([]);
  const [modelGroups, setModelGroups] = useState<UserModelGroup[]>([]);
  const [latencyMap, setLatencyMap] = useState<Record<string, LatencyState>>({});
  const [invokeMap, setInvokeMap] = useState<Record<string, InvokeState>>({});
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    Promise.all([userApi.modelGroups(), userApi.models()])
      .then(([groupResponse, modelResponse]) => {
        const groups = groupResponse.data.map(modelGroupFromDTO);
        const groupMap = new Map(groups.map((group) => [group.id, group]));
        const nextModels = dedupeById(modelResponse.data.map((model) => normalizeBackendModel(model, groupMap)));
        setModels(nextModels);
        setModelGroups(ensureGroupsForModels(groups, nextModels));
      })
      .catch((error) => {
        setModels([]);
        setModelGroups([]);
        toast.error(getErrorMessage(error, '加载可用模型失败'));
      });
  }, []);

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return models.filter((model) => {
      if (provider !== 'all' && model.provider !== provider) return false;
      if (format !== 'all' && !modelFormats(model).includes(format)) return false;
      if (groupFilter !== 'all' && model.modelGroupId !== groupFilter) return false;
      if (!keyword) return true;
      return (
        model.name.toLowerCase().includes(keyword) ||
        model.provider.toLowerCase().includes(keyword) ||
        model.modelGroupName.toLowerCase().includes(keyword) ||
        model.sourceName.toLowerCase().includes(keyword) ||
        modelFormats(model).some((item) => item.includes(keyword))
      );
    });
  }, [models, provider, format, groupFilter, query]);

  const displaySections = useMemo(() => {
    const visibleGroups = groupFilter === 'all' ? modelGroups : modelGroups.filter((group) => group.id === groupFilter);
    return visibleGroups
      .map((group) => ({
        group,
        models: filtered.filter((model) => model.modelGroupId === group.id),
      }))
      .filter((section) => section.models.length > 0);
  }, [filtered, groupFilter, modelGroups]);

  const displayCount = displaySections.reduce((total, section) => total + section.models.length, 0);

  const testOne = async (model: DisplayModel) => {
    setLatencyMap((prev) => ({ ...prev, [model.id]: 'loading' }));
    try {
      const response = await userApi.testModel(model.id);
      const newLatency = response.data.latencyMs;
      setLatencyMap((prev) => ({ ...prev, [model.id]: newLatency }));
      setModels((prev) => prev.map((item) => (item.id === model.id ? { ...item, latencyMs: newLatency, status: 'online' } : item)));
      toast.success(`${model.name} 延迟测试完成`, { description: `当前转发延迟: ${newLatency}ms` });
    } catch (error) {
      setLatencyMap((prev) => ({ ...prev, [model.id]: model.latencyMs }));
      toast.error(getErrorMessage(error, `${model.name} 测速失败`));
    }
  };

  const invokeOne = async (model: DisplayModel) => {
    setInvokeMap((prev) => ({ ...prev, [model.id]: 'loading' }));
    try {
      const response = await userApi.invokeTestModel(model.id);
      const { latencyMs, totalTokens } = response.data;
      setInvokeMap((prev) => ({ ...prev, [model.id]: 'success' }));
      setLatencyMap((prev) => ({ ...prev, [model.id]: latencyMs }));
      setModels((prev) => prev.map((item) => (item.id === model.id ? { ...item, latencyMs, status: 'online' } : item)));
      toast.success(`${model.name} 调用测试通过`, {
        description: `真实模型响应: ${latencyMs}ms · 消耗 ${compactTokens(totalTokens)} tokens`,
      });
    } catch (error) {
      setInvokeMap((prev) => ({ ...prev, [model.id]: 'error' }));
      toast.error(getErrorMessage(error, `${model.name} 调用测试失败`));
    }
  };

  const refreshAll = async () => {
    setRefreshing(true);
    const onlineModels = dedupeById(filtered.filter((model) => model.status === 'online'));

    setLatencyMap((prev) => {
      const next = { ...prev };
      for (const model of onlineModels) next[model.id] = 'loading';
      return next;
    });

    try {
      const results = await Promise.allSettled(onlineModels.map((model) => userApi.testModel(model.id)));
      setLatencyMap((prev) => {
        const next = { ...prev };
        results.forEach((result, index) => {
          const model = onlineModels[index];
          next[model.id] = result.status === 'fulfilled' ? result.value.data.latencyMs : model.latencyMs;
        });
        return next;
      });
      setModels((prev) =>
        prev.map((model) => {
          const index = onlineModels.findIndex((item) => item.id === model.id);
          if (index < 0) return model;
          const result = results[index];
          return result.status === 'fulfilled' ? { ...model, latencyMs: result.value.data.latencyMs, status: 'online' } : model;
        }),
      );
      toast.success('已刷新当前页面所有在线模型延迟');
    } catch (error) {
      toast.error(getErrorMessage(error, '批量测速失败'));
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div className="space-y-8">
      <PageHeader
        eyebrow="可用模型"
        title="可用模型"
        description="查看当前 Key 可调用的平台模型和模型分组。"
        actions={
          <div className="flex items-center gap-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索模型或分组..."
                className="h-10 w-72 rounded-xl pl-9"
              />
            </div>
            <Button size="lg" className="rounded-xl font-bold" onClick={refreshAll} disabled={refreshing}>
              {refreshing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RotateCcw className="mr-2 h-4 w-4" />}
              一键测速
            </Button>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-3">
        <Select value={provider} onValueChange={(value) => setProvider(value as ProviderFilter)}>
          <SelectTrigger className="h-10 w-56 rounded-xl bg-muted/40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {providerOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={groupFilter} onValueChange={setGroupFilter}>
          <SelectTrigger className="h-10 w-56 rounded-xl bg-muted/40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部分组</SelectItem>
            {modelGroups.map((group) => (
              <SelectItem key={group.id} value={group.id}>
                {group.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Tabs value={format} onValueChange={(value) => setFormat(value as FormatFilter)}>
          <TabsList className="h-11 rounded-xl bg-muted/40 p-1">
            {formatTabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} className="rounded-lg px-5 text-xs font-bold">
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <Card className="overflow-hidden border-border/40 shadow-sm">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/20 hover:bg-muted/20">
              <TableHead className="w-[30%] py-4">模型节点</TableHead>
              <TableHead>协议格式</TableHead>
              <TableHead>实时状态</TableHead>
              <TableHead>转发延迟</TableHead>
              <TableHead className="w-[180px] pr-6 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {displaySections.map((section, sectionIndex) => (
              <Fragment key={section.group.id}>
                <TableRow className="border-b bg-muted/25 hover:bg-muted/25">
                  <TableCell colSpan={5} className="px-4 py-2">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                        <span className="font-semibold text-foreground">{section.group.name}</span>
                        <span>· {section.models.length} 个模型</span>
                        <span className="min-w-0 truncate">{section.group.description}</span>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
                {section.models.map((model, index) => {
                  const state = latencyMap[model.id] ?? model.latencyMs;
                  const isOffline = model.status === 'offline';
                  const isLoading = state === 'loading';
                  const isInvoking = invokeMap[model.id] === 'loading';

                  return (
                    <motion.tr
                      key={`${section.group.id}:${model.id}`}
                      initial={{ opacity: 0, y: 6 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: (sectionIndex + index) * 0.02, ease: [0.22, 1, 0.36, 1] }}
                      className="group border-b border-border/40 transition-colors hover:bg-muted/30"
                    >
                      <TableCell className="py-4">
                        <div className="flex items-center gap-3">
                          <div className="relative">
                            <ProviderIcon provider={model.provider} size="md" />
                            <div className="absolute -bottom-1 -right-1 flex h-3 w-3 items-center justify-center rounded-full border-2 border-background bg-background">
                              <StatusDot tone={isOffline ? 'offline' : 'online'} />
                            </div>
                          </div>
                          <div className="min-w-0">
                            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                              <span className="truncate font-bold text-foreground">{model.name}</span>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon"
                                aria-label={`复制模型名称 ${model.name}`}
                                title="复制模型名称"
                                className="h-6 w-6 shrink-0 rounded-md text-muted-foreground opacity-70 transition-opacity hover:text-foreground group-hover:opacity-100"
                                onClick={async () => {
                                  await copyToClipboard(model.name);
                                  toast.success('已复制模型名称', { description: model.name });
                                }}
                              >
                                <Copy className="h-3.5 w-3.5" />
                              </Button>
                            </div>
                            <div className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground/60">{model.id}</div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {modelFormats(model).map((item) => (
                            <Badge
                              key={item}
                              variant="outline"
                              className={cn(
                                'h-6 rounded-md px-2 py-0 font-mono text-[10px] font-bold uppercase tracking-wider shadow-none',
                                item === 'openai'
                                  ? 'border-sky-500/20 bg-sky-500/5 text-sky-600'
                                  : 'border-orange-500/20 bg-orange-500/5 text-orange-600',
                              )}
                            >
                              {item}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        <span
                          className={cn(
                            'rounded-full border px-2 py-0.5 text-xs font-bold uppercase tracking-widest',
                            isOffline
                              ? 'border-destructive/20 bg-destructive/5 text-destructive'
                              : 'border-emerald-500/20 bg-emerald-500/5 text-emerald-500',
                          )}
                        >
                          {isOffline ? 'Offline' : 'Online'}
                        </span>
                      </TableCell>
                      <TableCell>
                        <div className="min-w-[100px]">
                          {isLoading ? (
                            <div className="flex animate-pulse items-center gap-2 text-xs font-bold text-primary">
                              <div className="h-1.5 w-1.5 rounded-full bg-primary" />
                              PINGING...
                            </div>
                          ) : isOffline ? (
                            <span className="font-mono text-xs text-muted-foreground opacity-40">TIMEOUT</span>
                          ) : (
                            <div className="flex items-center gap-2">
                              <span className={cn('font-mono text-sm', latencyClass(state as number))}>{state}ms</span>
                              {(state as number) < 100 && <CheckCircle2 className="h-3 w-3 text-emerald-500" />}
                            </div>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="pr-6 text-right">
                        <div className="flex min-w-[160px] items-center justify-end gap-2">
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={isOffline || isLoading || isInvoking}
                            onClick={() => testOne(model)}
                            className="h-8 min-w-[66px] rounded-lg text-xs font-bold transition-colors hover:bg-primary/10 hover:text-primary"
                          >
                            {isLoading ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Zap className="mr-1.5 h-3.5 w-3.5" />}
                            测速
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={isOffline || isLoading || isInvoking}
                            onClick={() => invokeOne(model)}
                            className="h-8 min-w-[78px] rounded-lg border-border/70 bg-background text-xs font-bold transition-colors hover:bg-primary hover:text-primary-foreground"
                          >
                            {isInvoking ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Bot className="mr-1.5 h-3.5 w-3.5" />}
                            调用
                          </Button>
                        </div>
                      </TableCell>
                    </motion.tr>
                  );
                })}
              </Fragment>
            ))}
            {displaySections.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="h-32 text-center text-muted-foreground">
                  未匹配到相关模型节点
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <CardContent className="flex items-center justify-between border-t bg-muted/20 py-3 text-xs text-muted-foreground">
          <div>显示 {displayCount} 条 · 共 {models.length} 个模型节点</div>
        </CardContent>
      </Card>
    </div>
  );
}
