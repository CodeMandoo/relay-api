import { useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Clock,
  Coins,
  Copy,
  FilterX,
  Hash,
  KeyRound,
  Loader2,
  Search,
  ScrollText,
  TerminalSquare,
  type LucideIcon,
} from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  Input,
  ScrollArea,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  cn,
} from '@relay-api/ui';
import {
  copyToClipboard,
  formatCurrency,
  formatDateTime,
  formatNumberFull,
  type ApiKey,
  type RequestAttemptLog,
  type RequestLog,
} from '@relay-api/lib';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { EmptyState } from '@/components/common/EmptyState';
import { getErrorMessage, userApi } from '@/lib/api';

type StatusFilter = 'all' | 'success' | 'error';
type TimeRange = 'all' | 'day' | 'week' | 'month';

interface TokenUsage {
  prompt: number;
  completion: number;
  cacheRead: number;
  cacheWrite: number;
  reasoning: number;
  total: number;
}

const rangeOptions: { value: TimeRange; label: string }[] = [
  { value: 'all', label: '全部时间' },
  { value: 'day', label: '近 24 小时' },
  { value: 'week', label: '近 7 天' },
  { value: 'month', label: '近 30 天' },
];

const latencyTone = (latency: number) => {
  if (latency < 800) return 'text-emerald-600 dark:text-emerald-400';
  if (latency < 2000) return 'text-amber-600 dark:text-amber-400';
  return 'text-destructive';
};

const statusTone = (status: RequestLog['statusText']) => (status === 'success' ? 'success' : 'destructive');
const pretty = (value: unknown) => JSON.stringify(value ?? {}, null, 2);

const rangeStartISO = (range: TimeRange) => {
  if (range === 'all') return undefined;
  const hours = range === 'day' ? 24 : range === 'week' ? 24 * 7 : 24 * 30;
  return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
};

const getTokenUsage = (log: RequestLog): TokenUsage => ({
  prompt: log.tokensPrompt,
  completion: log.tokensCompletion,
  cacheRead: log.tokensCacheRead ?? 0,
  cacheWrite: log.tokensCacheWrite ?? 0,
  reasoning: log.tokensReasoning ?? 0,
  total: log.tokensTotal,
});

export default function Page() {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [model, setModel] = useState('all');
  const [apiKey, setApiKey] = useState('all');
  const [range, setRange] = useState<TimeRange>('week');
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [apiKeys, setApiKeys] = useState<ApiKey[]>([]);
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20, total: 0, totalPages: 1 });
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null);
  const [attemptsByLog, setAttemptsByLog] = useState<Record<string, RequestAttemptLog[]>>({});
  const [attemptsLoading, setAttemptsLoading] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    userApi
      .apiKeys()
      .then((response) => setApiKeys(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载 API Key 失败')));
  }, []);

  useEffect(() => {
    setLoading(true);
    userApi
      .logs({
        status,
        model,
        apiKeyId: apiKey,
        q: query,
        page,
        pageSize,
        from: rangeStartISO(range),
      })
      .then((response) => {
        setLogs(response.data);
        setPagination(response.pagination);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载请求日志失败')))
      .finally(() => setLoading(false));
  }, [apiKey, model, page, pageSize, query, range, status]);

  const models = useMemo(() => Array.from(new Set(logs.map((log) => log.model).filter(Boolean))).sort(), [logs]);

  const stats = useMemo(() => {
    const success = logs.filter((log) => log.statusText === 'success').length;
    const totalTokens = logs.reduce((sum, log) => sum + log.tokensTotal, 0);
    const cost = logs.reduce((sum, log) => sum + (log.estimatedCost ?? 0), 0);
    const avgLatency =
      logs.length === 0 ? 0 : Math.round(logs.reduce((sum, log) => sum + log.latencyMs, 0) / logs.length);
    return {
      total: logs.length,
      successRate: logs.length === 0 ? 0 : Math.round((success / logs.length) * 100),
      totalTokens,
      cost,
      avgLatency,
    };
  }, [logs]);

  const reset = () => {
    setQuery('');
    setStatus('all');
    setModel('all');
    setApiKey('all');
    setRange('week');
    setPage(1);
    setSelectedLog(null);
  };

  const updateFilter = (fn: () => void) => {
    fn();
    setPage(1);
    setSelectedLog(null);
  };

  const openLog = (log: RequestLog) => {
    setSelectedLog(log);
    if (attemptsByLog[log.id] || attemptsLoading[log.id]) {
      return;
    }
    setAttemptsLoading((current) => ({ ...current, [log.id]: true }));
    userApi
      .logAttempts(log.id)
      .then((response) => setAttemptsByLog((current) => ({ ...current, [log.id]: response.data })))
      .catch((error) => toast.error(getErrorMessage(error, '加载尝试链失败')))
      .finally(() => setAttemptsLoading((current) => ({ ...current, [log.id]: false })));
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="请求观测"
        title="请求日志"
        description="按请求查看模型调用、Token 明细、成本、延迟和结果状态。"
        actions={
          <Button variant="outline" onClick={reset}>
            <FilterX className="mr-2 h-4 w-4" />
            重置筛选
          </Button>
        }
      />

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="当前页请求" value={stats.total} icon={ScrollText} tone="primary" delay={0} hint="按当前筛选" />
        <StatCard label="成功率" value={`${stats.successRate}%`} icon={Hash} tone="success" delay={0.05} hint="当前页" />
        <StatCard label="总 Tokens" value={formatNumberFull(stats.totalTokens)} icon={TerminalSquare} tone="neutral" delay={0.1} hint="输入 + 输出" />
        <StatCard label="预估成本" value={formatCurrency(stats.cost)} icon={Coins} tone="warning" delay={0.15} hint="USD" />
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto border-b">
          <div className="grid min-w-[980px] grid-cols-[minmax(260px,1fr)_140px_170px_170px_170px] gap-3 p-4">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                className="pl-9"
                placeholder="搜索 Request ID、模型、路径或错误..."
                value={query}
                onChange={(event) => updateFilter(() => setQuery(event.target.value))}
              />
            </div>
            <Select value={status} onValueChange={(value) => updateFilter(() => setStatus(value as StatusFilter))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="success">成功</SelectItem>
                <SelectItem value="error">失败</SelectItem>
              </SelectContent>
            </Select>
            <Select value={model} onValueChange={(value) => updateFilter(() => setModel(value))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部模型</SelectItem>
                {models.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={apiKey} onValueChange={(value) => updateFilter(() => setApiKey(value))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部 API Key</SelectItem>
                {apiKeys.map((item) => (
                  <SelectItem key={item.id} value={item.id}>
                    {item.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={range} onValueChange={(value) => updateFilter(() => setRange(value as TimeRange))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {rangeOptions.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {loading ? (
          <div className="p-10 text-center text-sm text-muted-foreground">正在加载请求日志...</div>
        ) : (
          <Table className="min-w-[1040px]">
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead>时间 / 请求</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>API Key</TableHead>
                <TableHead className="text-right">Token</TableHead>
                <TableHead className="text-right">缓存</TableHead>
                <TableHead className="text-right">成本</TableHead>
                <TableHead>延迟</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={9} className="p-6">
                    <EmptyState icon={Search} title="没有匹配的请求" description="修改筛选条件后重新查询。" />
                  </TableCell>
                </TableRow>
              ) : (
                logs.map((log, index) => (
                  <LogRow key={log.id} log={log} index={index} onOpen={() => openLog(log)} />
                ))
              )}
            </TableBody>
          </Table>
        )}

        <CardContent className="flex flex-wrap items-center justify-between gap-2 border-t bg-muted/20 py-3 text-xs text-muted-foreground">
          <span>第 {pagination.page} / {pagination.totalPages} 页 · 共 {pagination.total} 条</span>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={pagination.page <= 1 || loading} onClick={() => setPage((current) => Math.max(1, current - 1))}>
              <ChevronLeft className="mr-1 h-3.5 w-3.5" />
              上一页
            </Button>
            <Button variant="outline" size="sm" disabled={pagination.page >= pagination.totalPages || loading} onClick={() => setPage((current) => current + 1)}>
              下一页
              <ChevronRight className="ml-1 h-3.5 w-3.5" />
            </Button>
          </div>
        </CardContent>
      </Card>

      <RequestLogSheet
        log={selectedLog}
        attempts={selectedLog ? attemptsByLog[selectedLog.id] ?? [] : []}
        attemptsLoading={selectedLog ? Boolean(attemptsLoading[selectedLog.id]) : false}
        onClose={() => setSelectedLog(null)}
      />
    </div>
  );
}

function LogRow({ log, index, onOpen }: { log: RequestLog; index: number; onOpen: () => void }) {
  const tokens = getTokenUsage(log);

  return (
    <motion.tr
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ delay: index * 0.02, ease: [0.22, 1, 0.36, 1] }}
      className="cursor-pointer border-b transition-colors hover:bg-muted/40"
      onClick={onOpen}
    >
      <TableCell>
        <div className="text-sm">{formatDateTime(log.timestamp)}</div>
        <div className="font-mono text-[11px] text-muted-foreground">{log.requestId ?? log.id}</div>
      </TableCell>
      <TableCell>
        <Badge variant="secondary" className="max-w-[220px] truncate font-mono text-[11px]">
          {log.model}
        </Badge>
        {log.path && <div className="mt-1 max-w-[240px] truncate font-mono text-[11px] text-muted-foreground">{log.path}</div>}
      </TableCell>
      <TableCell>
        <div className="font-medium">{log.apiKeyName}</div>
        {log.apiKeyId && <div className="mt-1 font-mono text-[11px] text-muted-foreground">{log.apiKeyId}</div>}
      </TableCell>
      <TableCell className="text-right">
        <div className="font-mono font-semibold">{formatNumberFull(tokens.total)}</div>
        <div className="mt-1 font-mono text-[11px] text-muted-foreground">
          {formatNumberFull(tokens.prompt)} / {formatNumberFull(tokens.completion)}
        </div>
      </TableCell>
      <TableCell className="text-right">
        <div className="font-mono text-sm">{formatNumberFull(tokens.cacheRead)}</div>
        <div className="mt-1 font-mono text-[11px] text-muted-foreground">写 {formatNumberFull(tokens.cacheWrite)}</div>
      </TableCell>
      <TableCell className="text-right font-mono font-semibold">{formatCurrency(log.estimatedCost ?? 0)}</TableCell>
      <TableCell>
        <span className={cn('font-mono font-semibold', latencyTone(log.latencyMs))}>{log.latencyMs}ms</span>
        {(log.attemptCount ?? 0) > 1 && <div className="mt-1 text-[11px] text-muted-foreground">{log.attemptCount} 次尝试</div>}
      </TableCell>
      <TableCell>
        <StatusBadge
          tone={statusTone(log.statusText)}
          label={log.statusText === 'success' ? `成功 ${log.statusCode}` : `失败 ${log.statusCode}`}
        />
      </TableCell>
      <TableCell className="text-right">
        <Button
          variant="outline"
          size="sm"
          onClick={(event) => {
            event.stopPropagation();
            onOpen();
          }}
        >
          查看详情
        </Button>
      </TableCell>
    </motion.tr>
  );
}

function RequestLogSheet({
  log,
  attempts,
  attemptsLoading,
  onClose,
}: {
  log: RequestLog | null;
  attempts: RequestAttemptLog[];
  attemptsLoading: boolean;
  onClose: () => void;
}) {
  const tokens = log ? getTokenUsage(log) : null;

  return (
    <Sheet open={Boolean(log)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-[min(100vw,920px)] overflow-hidden p-0 sm:max-w-[920px]">
        {log && tokens && (
          <div className="flex h-full flex-col">
            <SheetHeader className="border-b px-6 py-5">
              <div className="flex items-start justify-between gap-4 pr-8">
                <div className="min-w-0">
                  <SheetTitle className="flex flex-wrap items-center gap-2">
                    <span className="truncate font-mono">{log.model}</span>
                    <StatusBadge
                      tone={statusTone(log.statusText)}
                      label={log.statusText === 'success' ? `成功 ${log.statusCode}` : `失败 ${log.statusCode}`}
                    />
                  </SheetTitle>
                  <SheetDescription className="mt-2 font-mono">{log.requestId ?? log.id}</SheetDescription>
                </div>
                <div className="text-right">
                  <div className="text-xs text-muted-foreground">成本</div>
                  <div className="font-mono text-lg font-semibold">{formatCurrency(log.estimatedCost ?? 0)}</div>
                </div>
              </div>
            </SheetHeader>

            <ScrollArea className="min-h-0 flex-1">
              <div className="space-y-5 p-6">
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                  <InfoTile icon={Clock} label="延迟" value={`${log.latencyMs}ms`} tone={latencyTone(log.latencyMs)} />
                  <InfoTile icon={Hash} label="总 Tokens" value={formatNumberFull(tokens.total)} />
                  <InfoTile icon={KeyRound} label="API Key" value={log.apiKeyName} />
                  <InfoTile icon={ScrollText} label="尝试次数" value={String(log.attemptCount ?? attempts.length ?? 0)} />
                </div>

                <div className="grid gap-3 rounded-lg border bg-muted/15 p-4 text-xs md:grid-cols-2 xl:grid-cols-4">
                  <MetaItem label="协议" value={log.protocol ?? '-'} />
                  <MetaItem label="路径" value={log.path ?? '-'} mono />
                  <MetaItem label="流式" value={log.stream ? '是' : '否'} />
                  <MetaItem label="时间" value={formatDateTime(log.timestamp)} />
                </div>

                <Card className="border-border/50">
                  <CardContent className="p-4">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <div className="text-sm font-semibold">Token 明细</div>
                      {tokens.reasoning > 0 && (
                        <Badge variant="outline" className="border-sky-500/30 bg-sky-500/10 text-sky-600">
                          reasoning {formatNumberFull(tokens.reasoning)}
                        </Badge>
                      )}
                    </div>
                    <TokenBreakdown tokens={tokens} />
                  </CardContent>
                </Card>

                {log.errorMessage && (
                  <div className="rounded-lg border border-destructive/25 bg-destructive/[0.04] p-4">
                    <div className="flex items-center gap-2 text-sm font-semibold text-destructive">
                      <AlertTriangle className="h-4 w-4" />
                      {log.errorMessage}
                    </div>
                  </div>
                )}

                <Tabs defaultValue="payload" className="space-y-4">
                  <TabsList className="bg-muted/60">
                    <TabsTrigger value="payload">请求/响应</TabsTrigger>
                    <TabsTrigger value="attempts">尝试链</TabsTrigger>
                    <TabsTrigger value="headers">Headers</TabsTrigger>
                  </TabsList>

                  <TabsContent value="payload" className="space-y-4">
                    <PayloadPanel title="请求输入" value={pretty(log.requestPayload)} />
                    <PayloadPanel title="响应输出" value={pretty(log.responsePayload)} />
                  </TabsContent>

                  <TabsContent value="attempts">
                    <AttemptList attempts={attempts} loading={attemptsLoading} />
                  </TabsContent>

                  <TabsContent value="headers" className="space-y-4">
                    <PayloadPanel title="请求 Headers" value={pretty(log.requestHeaders)} />
                    <PayloadPanel title="响应 Headers" value={pretty(log.responseHeaders)} />
                  </TabsContent>
                </Tabs>
              </div>
            </ScrollArea>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function InfoTile({ icon: Icon, label, value, tone }: { icon: LucideIcon; label: string; value: string; tone?: string }) {
  return (
    <div className="rounded-lg border bg-muted/15 p-3">
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="h-3.5 w-3.5" />
        {label}
      </div>
      <div className={cn('truncate font-mono text-sm font-semibold', tone)}>{value}</div>
    </div>
  );
}

function MetaItem({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-muted-foreground">{label}</div>
      <div className={cn('mt-1 truncate text-foreground', mono && 'font-mono')}>{value}</div>
    </div>
  );
}

function AttemptList({ attempts, loading }: { attempts: RequestAttemptLog[]; loading: boolean }) {
  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border bg-background p-3 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        正在加载尝试链...
      </div>
    );
  }
  if (attempts.length === 0) {
    return <div className="rounded-lg border bg-background p-3 text-sm text-muted-foreground">此日志没有记录尝试链。</div>;
  }
  return (
    <div className="overflow-hidden rounded-lg border">
      {attempts.map((attempt) => (
        <div key={attempt.id} className="grid gap-3 border-b p-4 text-xs last:border-b-0 md:grid-cols-[80px_1fr_120px_110px]">
          <div className="font-mono text-sm text-muted-foreground">#{attempt.attemptIndex}</div>
          <div className="min-w-0">
            <div className="text-sm font-medium">{formatDateTime(attempt.startedAt)}</div>
            <div className="mt-1 truncate font-mono text-muted-foreground">{attempt.model}</div>
            {attempt.errorMessage && <div className="mt-1 truncate text-xs text-destructive">{attempt.errorMessage}</div>}
          </div>
          <StatusBadge
            tone={attempt.statusText === 'success' ? 'success' : 'destructive'}
            label={attempt.statusText === 'success' ? `成功 ${attempt.statusCode}` : `失败 ${attempt.statusCode}`}
          />
          <div className={cn('font-mono text-sm font-semibold', latencyTone(attempt.latencyMs))}>{attempt.latencyMs}ms</div>
        </div>
      ))}
    </div>
  );
}

function TokenBreakdown({ tokens }: { tokens: TokenUsage }) {
  const items = [
    { label: '输入', value: tokens.prompt, className: 'bg-sky-500' },
    { label: '输出', value: tokens.completion, className: 'bg-emerald-500' },
    { label: '缓存读', value: tokens.cacheRead, className: 'bg-violet-500' },
    { label: '缓存写', value: tokens.cacheWrite, className: 'bg-amber-500' },
    { label: '推理', value: tokens.reasoning, className: 'bg-rose-500' },
  ].filter((item) => item.value > 0);

  if (items.length === 0) {
    return <div className="rounded-md border bg-background p-3 text-sm text-muted-foreground">暂无 Token 明细。</div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex h-2 overflow-hidden rounded-full bg-muted">
        {items.map((item) => (
          <div
            key={item.label}
            className={item.className}
            style={{ width: `${Math.max(4, (item.value / Math.max(1, tokens.total)) * 100)}%` }}
          />
        ))}
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
        {items.map((item) => (
          <div key={item.label} className="rounded-md border bg-background p-3">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className={cn('h-2 w-2 rounded-full', item.className)} />
              {item.label}
            </div>
            <div className="mt-2 font-mono text-sm font-semibold">{formatNumberFull(item.value)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function PayloadPanel({ title, value }: { title: string; value: string }) {
  return (
    <div className="overflow-hidden rounded-lg border bg-background">
      <div className="flex items-center justify-between border-b bg-muted/20 px-4 py-2">
        <div className="text-sm font-semibold">{title}</div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          onClick={() => {
            void copyToClipboard(value);
            toast.success('已复制');
          }}
        >
          <Copy className="mr-1.5 h-3.5 w-3.5" />
          复制
        </Button>
      </div>
      <pre className="max-h-80 overflow-auto p-4 text-xs leading-relaxed text-muted-foreground">{value}</pre>
    </div>
  );
}
