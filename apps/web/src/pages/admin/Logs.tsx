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
  DatabaseZap,
  FilterX,
  Hash,
  KeyRound,
  Loader2,
  Search,
  Server,
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
  type RequestAttemptLog,
  type RequestLog,
} from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { EmptyState } from '@/components/common/EmptyState';

type StatusFilter = 'all' | 'success' | 'error';

const latencyClass = (ms: number): string => {
  if (ms < 500) return 'text-emerald-600 dark:text-emerald-400';
  if (ms < 1500) return 'text-amber-600 dark:text-amber-400';
  return 'text-destructive';
};

const prettyJson = (payload: unknown): string => JSON.stringify(payload ?? {}, null, 2);

export default function Page() {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [model, setModel] = useState('all');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null);
  const [logs, setLogs] = useState<RequestLog[]>([]);
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20, total: 0, totalPages: 1 });
  const [attemptsByLog, setAttemptsByLog] = useState<Record<string, RequestAttemptLog[]>>({});
  const [attemptsLoading, setAttemptsLoading] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    adminApi
      .logs({ status, model, q: query, page, pageSize, from, to })
      .then((response) => {
        setLogs(response.data);
        setPagination(response.pagination);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载请求日志失败')))
      .finally(() => setLoading(false));
  }, [from, model, page, pageSize, query, status, to]);

  const models = useMemo(() => Array.from(new Set(logs.map((log) => log.model).filter(Boolean))), [logs]);

  const stats = useMemo(() => {
    const success = logs.filter((log) => log.statusText === 'success').length;
    const error = logs.length - success;
    const tokens = logs.reduce((sum, log) => sum + log.tokensTotal, 0);
    const avgLatency =
      logs.length === 0 ? 0 : Math.round(logs.reduce((sum, log) => sum + log.latencyMs, 0) / logs.length);
    return { success, error, tokens, avgLatency };
  }, [logs]);

  const reset = () => {
    setQuery('');
    setStatus('all');
    setModel('all');
    setFrom('');
    setTo('');
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
    adminApi
      .logAttempts(log.id)
      .then((response) => setAttemptsByLog((current) => ({ ...current, [log.id]: response.data })))
      .catch((error) => toast.error(getErrorMessage(error, '加载上游尝试链失败')))
      .finally(() => setAttemptsLoading((current) => ({ ...current, [log.id]: false })));
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="请求观测"
        title="请求日志"
        description="检索用户、模型、上游和 API Key 的调用记录，快速定位失败请求。"
        actions={
          <Button variant="outline" onClick={reset}>
            <FilterX className="mr-2 h-4 w-4" />
            重置筛选
          </Button>
        }
      />

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="成功请求" value={stats.success} icon={DatabaseZap} tone="success" delay={0} hint="当前页" />
        <StatCard label="失败请求" value={stats.error} icon={AlertTriangle} tone={stats.error > 0 ? 'destructive' : 'neutral'} delay={0.05} hint="含上游错误与超时" />
        <StatCard label="总 Tokens" value={formatNumberFull(stats.tokens)} icon={TerminalSquare} tone="primary" delay={0.1} hint="Prompt + Completion" />
        <StatCard label="平均耗时" value={`${stats.avgLatency}ms`} icon={Clock} tone="warning" delay={0.15} hint="端到端响应耗时" />
      </div>

      <Card className="overflow-hidden">
        <div className="grid gap-3 border-b p-4 md:grid-cols-[1fr_150px_180px_190px_190px]">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              className="pl-9"
              placeholder="搜索用户、API Key、模型或上游..."
              value={query}
              onChange={(e) => updateFilter(() => setQuery(e.target.value))}
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
          <Input type="datetime-local" className="min-w-0" aria-label="开始时间" value={from} onChange={(e) => updateFilter(() => setFrom(e.target.value))} />
          <Input type="datetime-local" className="min-w-0" aria-label="结束时间" value={to} onChange={(e) => updateFilter(() => setTo(e.target.value))} />
        </div>

        {loading ? (
          <div className="p-10 text-center text-sm text-muted-foreground">正在加载请求日志...</div>
        ) : logs.length === 0 ? (
          <div className="p-6">
            <EmptyState icon={Search} title="没有匹配的日志" description="修改筛选条件后重新查询。" />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead>时间 / 请求</TableHead>
                <TableHead>用户 / API Key</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>上游</TableHead>
                <TableHead className="text-right">Token</TableHead>
                <TableHead className="text-right">成本</TableHead>
                <TableHead>耗时</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.map((log, index) => (
                <LogRow
                  key={log.id}
                  log={log}
                  index={index}
                  onOpen={() => openLog(log)}
                />
              ))}
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

interface LogRowProps {
  log: RequestLog;
  index: number;
  onOpen: () => void;
}

function LogRow({ log, index, onOpen }: LogRowProps) {
  const tokensCacheRead = log.tokensCacheRead ?? 0;
  const tokensCacheWrite = log.tokensCacheWrite ?? 0;
  const tokensReasoning = log.tokensReasoning ?? 0;

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
        <div className="text-sm font-medium">{log.userEmail}</div>
        <div className="text-xs text-muted-foreground">{log.apiKeyName}</div>
      </TableCell>
      <TableCell>
        <Badge variant="secondary" className="max-w-[220px] truncate font-mono text-[11px]">
          {log.model}
        </Badge>
      </TableCell>
      <TableCell>
        <div className="inline-flex max-w-[220px] items-center gap-1 text-sm">
          <Server className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{log.upstreamName || '-'}</span>
        </div>
        {log.sourceKeyAlias && <div className="mt-1 text-xs text-muted-foreground">Key: {log.sourceKeyAlias}</div>}
      </TableCell>
      <TableCell className="text-right">
        <div className="font-mono text-sm">{formatNumberFull(log.tokensTotal)}</div>
        <div className="font-mono text-[11px] text-muted-foreground">
          P {formatNumberFull(log.tokensPrompt)} / C {formatNumberFull(log.tokensCompletion)}
        </div>
        {(tokensCacheRead > 0 || tokensCacheWrite > 0 || tokensReasoning > 0) && (
          <div className="font-mono text-[10px] text-muted-foreground/70">
            {tokensCacheRead > 0 && <span>缓存读 {formatNumberFull(tokensCacheRead)} </span>}
            {tokensCacheWrite > 0 && <span>缓存写 {formatNumberFull(tokensCacheWrite)} </span>}
            {tokensReasoning > 0 && <span>推理 {formatNumberFull(tokensReasoning)}</span>}
          </div>
        )}
      </TableCell>
      <TableCell className="text-right font-mono text-sm font-medium">{formatCurrency(log.estimatedCost ?? 0)}</TableCell>
      <TableCell>
        <span className={cn('font-mono text-sm font-medium', latencyClass(log.latencyMs))}>{log.latencyMs}ms</span>
      </TableCell>
      <TableCell>
        <StatusBadge
          tone={log.statusText === 'success' ? 'success' : 'destructive'}
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

interface TokenUsage {
  prompt: number;
  completion: number;
  cacheRead: number;
  cacheWrite: number;
  reasoning: number;
  total: number;
}

const getTokenUsage = (log: RequestLog): TokenUsage => ({
  prompt: log.tokensPrompt,
  completion: log.tokensCompletion,
  cacheRead: log.tokensCacheRead ?? 0,
  cacheWrite: log.tokensCacheWrite ?? 0,
  reasoning: log.tokensReasoning ?? 0,
  total: log.tokensTotal,
});

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
                      tone={log.statusText === 'success' ? 'success' : 'destructive'}
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
                  <InfoTile icon={KeyRound} label="用户" value={log.userEmail} />
                  <InfoTile icon={TerminalSquare} label="模型" value={log.model} />
                  <InfoTile icon={Server} label="上游" value={log.upstreamName || '-'} />
                  <InfoTile icon={KeyRound} label="API Key" value={log.apiKeyName} />
                  <InfoTile icon={Clock} label="耗时" value={`${log.latencyMs}ms`} tone={latencyClass(log.latencyMs)} />
                  <InfoTile icon={Hash} label="总 Tokens" value={formatNumberFull(tokens.total)} />
                  <InfoTile icon={Coins} label="成本" value={formatCurrency(log.estimatedCost ?? 0)} />
                  <InfoTile icon={Server} label="尝试次数" value={String(log.attemptCount ?? attempts.length ?? 0)} />
                </div>

                <div className="grid gap-3 rounded-lg border bg-muted/15 p-4 text-xs md:grid-cols-2 xl:grid-cols-4">
                  <MetaItem label="协议" value={log.protocol ?? '-'} />
                  <MetaItem label="路径" value={log.path ?? '-'} mono />
                  <MetaItem label="流式" value={log.stream ? '是' : '否'} />
                  <MetaItem label="上游 Key" value={log.sourceKeyAlias ?? log.sourceKeyId ?? '-'} />
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
                    <PayloadPanel title="请求输入" value={prettyJson(log.requestPayload)} />
                    <PayloadPanel title="响应输出" value={prettyJson(log.responsePayload)} />
                  </TabsContent>

                  <TabsContent value="attempts">
                    <AttemptList attempts={attempts} loading={attemptsLoading} />
                  </TabsContent>

                  <TabsContent value="headers" className="space-y-4">
                    <PayloadPanel title="请求 Headers" value={prettyJson(log.requestHeaders)} />
                    <PayloadPanel title="响应 Headers" value={prettyJson(log.responseHeaders)} />
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
        正在加载上游尝试链...
      </div>
    );
  }
  if (attempts.length === 0) {
    return (
      <div className="rounded-lg border bg-background p-3 text-sm text-muted-foreground">
        此日志没有记录上游尝试链。
      </div>
    );
  }
  return (
    <div className="overflow-hidden rounded-lg border bg-background">
      <div className="border-b px-3 py-2 text-xs font-semibold text-muted-foreground">上游尝试链</div>
      <div className="divide-y">
        {attempts.map((attempt) => (
          <div key={attempt.id} className="grid gap-3 p-3 text-xs md:grid-cols-[72px_1fr_120px_120px_120px]">
            <div className="font-mono text-muted-foreground">#{attempt.attemptIndex}</div>
            <div className="min-w-0">
              <div className="truncate font-medium">{attempt.upstreamName || attempt.sourceId || '-'}</div>
              <div className="mt-1 truncate font-mono text-muted-foreground">{attempt.model}</div>
              {attempt.errorMessage && <div className="mt-1 truncate text-destructive">{attempt.errorMessage}</div>}
            </div>
            <div>
              <StatusBadge
                tone={attempt.statusText === 'success' ? 'success' : 'destructive'}
                label={`${attempt.statusText === 'success' ? '成功' : '失败'} ${attempt.statusCode}`}
              />
            </div>
            <div className={cn('font-mono font-medium', latencyClass(attempt.latencyMs))}>{attempt.latencyMs}ms</div>
            <div className="font-mono text-muted-foreground">{formatDateTime(attempt.startedAt)}</div>
          </div>
        ))}
      </div>
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
