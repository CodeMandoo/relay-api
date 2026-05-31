import { useEffect, useMemo, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Bot, Loader2, RotateCcw, Search, Zap, CheckCircle2 } from 'lucide-react';
import { toast } from 'sonner';
import {
  Badge,
  Button,
  Card,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsList,
  TabsTrigger,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  cn,
} from '@relay-api/ui';
import { MODEL_PROVIDERS } from '@relay-api/lib';
import type { UserModel } from '@relay-api/lib';
import { getErrorMessage, userApi } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { StatusBadge } from '@/components/common/StatusBadge';
import { StatusDot } from '@/components/common/StatusDot';

type LatencyState = number | 'loading' | 'success';
type InvokeState = 'loading' | 'success' | 'error';
type ProviderFilter = 'all' | UserModel['provider'];
type FormatFilter = 'all' | UserModel['formats'][number];

const providerOptions: { value: ProviderFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  ...MODEL_PROVIDERS.map((provider) => ({ value: provider, label: provider })),
];

const formatTabs: { value: FormatFilter; label: string }[] = [
  { value: 'all', label: '全部格式' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
];

const latencyClass = (ms: number) => {
  if (ms === 0) return 'text-muted-foreground';
  if (ms < 150) return 'text-emerald-500 font-bold';
  if (ms < 300) return 'text-amber-500 font-bold';
  return 'text-destructive font-bold';
};

const compactTokens = (tokens: number) => {
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`;
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(1)}K`;
  return `${tokens}`;
};

const modelFormats = (model: UserModel): UserModel['formats'] => {
  if (model.formats?.length) return model.formats;
  return model.provider === 'Anthropic' ? ['anthropic'] : ['openai'];
};

export default function Page() {
  const [provider, setProvider] = useState<ProviderFilter>('all');
  const [format, setFormat] = useState<FormatFilter>('all');
  const [query, setQuery] = useState('');
  const [models, setModels] = useState<UserModel[]>([]);
  const [latencyMap, setLatencyMap] = useState<Record<string, LatencyState>>({});
  const [invokeMap, setInvokeMap] = useState<Record<string, InvokeState>>({});
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    userApi
      .models()
      .then((response) => setModels(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载可用模型失败')));
  }, []);

  const filtered = useMemo(() => {
    return models.filter(
      (m) =>
        (provider === 'all' || m.provider === provider) &&
        (format === 'all' || modelFormats(m).includes(format)) &&
        (
          m.name.toLowerCase().includes(query.toLowerCase()) ||
          m.provider.toLowerCase().includes(query.toLowerCase()) ||
          modelFormats(m).some((item) => item.includes(query.toLowerCase())) ||
          (m.sourceName ?? '').toLowerCase().includes(query.toLowerCase())
        ),
    );
  }, [models, provider, format, query]);

  const testOne = async (m: UserModel) => {
    setLatencyMap((prev) => ({ ...prev, [m.id]: 'loading' }));
    try {
      const response = await userApi.testModel(m.id);
      const newLatency = response.data.latencyMs;
      setLatencyMap((prev) => ({ ...prev, [m.id]: newLatency }));
      setModels((prev) => prev.map((item) => (item.id === m.id ? { ...item, latencyMs: newLatency, status: 'online' } : item)));
      toast.success(`${m.name} 延迟测试完成`, { description: `当前转发延迟: ${newLatency}ms` });
    } catch (error) {
      setLatencyMap((prev) => ({ ...prev, [m.id]: m.latencyMs }));
      toast.error(getErrorMessage(error, `${m.name} 测速失败`));
    }
  };

  const invokeOne = async (m: UserModel) => {
    setInvokeMap((prev) => ({ ...prev, [m.id]: 'loading' }));
    try {
      const response = await userApi.invokeTestModel(m.id);
      const { latencyMs, totalTokens } = response.data;
      setInvokeMap((prev) => ({ ...prev, [m.id]: 'success' }));
      setLatencyMap((prev) => ({ ...prev, [m.id]: latencyMs }));
      setModels((prev) => prev.map((item) => (item.id === m.id ? { ...item, latencyMs, status: 'online' } : item)));
      toast.success(`${m.name} 调用测试通过`, {
        description: `真实模型响应: ${latencyMs}ms · 消耗 ${compactTokens(totalTokens)} tokens`,
      });
    } catch (error) {
      setInvokeMap((prev) => ({ ...prev, [m.id]: 'error' }));
      toast.error(getErrorMessage(error, `${m.name} 调用测试失败`));
    }
  };

  const refreshAll = async () => {
    setRefreshing(true);
    const onlineModels = filtered.filter((m) => m.status === 'online');
    
    setLatencyMap((prev) => {
      const next = { ...prev };
      for (const m of onlineModels) next[m.id] = 'loading';
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
      toast.success('已刷新当前页面所有模型延迟');
    } catch (error) {
      toast.error(getErrorMessage(error, '批量测速失败'));
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div className="space-y-10">
      <PageHeader
        eyebrow="模型市场"
        title="可用模型"
        description="实时监控平台支持的各模型提供商转发延迟，保障您的应用响应速度。"
        actions={
          <div className="flex items-center gap-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="搜索模型..."
                className="w-64 pl-9 h-10 rounded-xl"
              />
            </div>
            <Button size="lg" className="rounded-xl font-bold" onClick={refreshAll} disabled={refreshing}>
              {refreshing ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <RotateCcw className="mr-2 h-4 w-4" />
              )}
              一键测速
            </Button>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-3">
        <Select value={provider} onValueChange={(v) => setProvider(v as ProviderFilter)}>
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

        <Tabs value={format} onValueChange={(v) => setFormat(v as FormatFilter)}>
          <TabsList className="bg-muted/40 p-1 rounded-xl h-11">
            {formatTabs.map((t) => (
              <TabsTrigger key={t.value} value={t.value} className="px-5 rounded-lg text-xs font-bold">
                {t.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <Card className="overflow-hidden border-border/40 shadow-sm">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/20 hover:bg-muted/20">
              <TableHead className="w-[35%] py-4">模型节点</TableHead>
              <TableHead>模型厂商</TableHead>
              <TableHead>协议格式</TableHead>
              <TableHead>上游 / 路由</TableHead>
              <TableHead>实时状态</TableHead>
              <TableHead>转发延迟 (Ping)</TableHead>
              <TableHead className="w-[180px] text-right pr-6">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="h-32 text-center text-muted-foreground">
                  未匹配到相关模型节点
                </TableCell>
              </TableRow>
            )}
            {filtered.map((m, i) => {
              const state = latencyMap[m.id] ?? m.latencyMs;
              const isOffline = m.status === 'offline';
              const isLoading = state === 'loading';
              const isInvoking = invokeMap[m.id] === 'loading';
              
              return (
                <motion.tr 
                  key={m.id}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: i * 0.02 }}
                  className="group border-b border-border/40 transition-colors hover:bg-muted/30"
                >
                  <TableCell className="py-4">
                    <div className="flex items-center gap-3">
                      <div className="relative">
                        <ProviderIcon provider={m.provider} size="md" />
                        <div className="absolute -right-1 -bottom-1 h-3 w-3 rounded-full border-2 border-background bg-background flex items-center justify-center">
                            <StatusDot tone={isOffline ? 'offline' : 'online'} />
                        </div>
                      </div>
                      <div className="min-w-0">
                        <div className="font-bold text-foreground truncate">{m.name}</div>
                        <div className="text-[10px] font-mono text-muted-foreground/60 uppercase tracking-wider">
                          {m.id}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge tone="neutral" label={m.provider} className="font-bold text-[10px] tracking-wider" />
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {modelFormats(m).map((item) => (
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
                    <div className="space-y-1">
                      <div className="max-w-[180px] truncate text-sm font-medium">
                        {m.sourceName ?? '平台中转源'}
                      </div>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Badge variant="secondary" className="h-5 rounded-md px-1.5 py-0 text-[10px] font-bold">
                          {m.sourceStatus === 'online' ? '上游在线' : '上游异常'}
                        </Badge>
                        {(m.routingCandidates ?? 1) > 1 && (
                          <Badge variant="outline" className="h-5 rounded-md px-1.5 py-0 text-[10px] font-bold">
                            多上游 {m.routingCandidates}
                          </Badge>
                        )}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span
                        className={cn(
                          'text-xs font-bold uppercase tracking-widest px-2 py-0.5 rounded-full border',
                          isOffline 
                            ? 'text-destructive border-destructive/20 bg-destructive/5' 
                            : 'text-emerald-500 border-emerald-500/20 bg-emerald-500/5',
                        )}
                      >
                        {isOffline ? 'Offline' : 'Online'}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="min-w-[100px]">
                        {isLoading ? (
                          <div className="flex items-center gap-2 text-xs font-bold text-primary animate-pulse">
                            <div className="h-1.5 w-1.5 rounded-full bg-primary" />
                            PINGING...
                          </div>
                        ) : isOffline ? (
                          <span className="font-mono text-xs text-muted-foreground opacity-40">TIMEOUT</span>
                        ) : (
                          <div className="flex items-center gap-2">
                             <span className={cn('font-mono text-sm', latencyClass(state as number))}>
                                {state}ms
                             </span>
                             {(state as number) < 100 && <CheckCircle2 className="h-3 w-3 text-emerald-500" />}
                          </div>
                        )}
                    </div>
                  </TableCell>
                  <TableCell className="text-right pr-6">
                    <div className="flex min-w-[160px] items-center justify-end gap-2">
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={isOffline || isLoading || isInvoking}
                        onClick={() => testOne(m)}
                        className="h-8 min-w-[66px] rounded-lg font-bold text-xs transition-colors hover:bg-primary/10 hover:text-primary"
                      >
                        {isLoading ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Zap className="mr-1.5 h-3.5 w-3.5" />}
                        测速
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={isOffline || isLoading || isInvoking}
                        onClick={() => invokeOne(m)}
                        className="h-8 min-w-[78px] rounded-lg border-border/70 bg-background font-bold text-xs transition-colors hover:bg-primary hover:text-primary-foreground"
                      >
                        {isInvoking ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Bot className="mr-1.5 h-3.5 w-3.5" />}
                        调用
                      </Button>
                    </div>
                  </TableCell>
                </motion.tr>
              );
            })}
          </TableBody>
        </Table>
      </Card>

      <div className="flex items-center justify-center gap-4 text-[10px] font-bold text-muted-foreground/40 uppercase tracking-[0.2em]">
         <div className="flex items-center gap-1.5">
            <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
            <span>Optimal (&lt;150ms)</span>
         </div>
         <div className="flex items-center gap-1.5">
            <div className="h-1.5 w-1.5 rounded-full bg-amber-500" />
            <span>Standard (150-300ms)</span>
         </div>
         <div className="flex items-center gap-1.5">
            <div className="h-1.5 w-1.5 rounded-full bg-destructive" />
            <span>High Latency (&gt;300ms)</span>
         </div>
      </div>
    </div>
  );
}
