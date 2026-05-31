import { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Clock,
  DatabaseZap,
  FilterX,
  Loader2,
  Search,
  Server,
  TerminalSquare,
} from 'lucide-react';
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
  cn,
} from '@relay-api/ui';
import { formatDateTime, formatNumberFull, type RequestAttemptLog, type RequestLog } from '@relay-api/lib';
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
  const [expanded, setExpanded] = useState<string | null>(null);
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
    setExpanded(null);
  };

  const updateFilter = (fn: () => void) => {
    fn();
    setPage(1);
    setExpanded(null);
  };

  const toggleLog = (log: RequestLog) => {
    const nextExpanded = expanded === log.id ? null : log.id;
    setExpanded(nextExpanded);
    if (!nextExpanded || attemptsByLog[log.id] || attemptsLoading[log.id]) {
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
        <StatCard label="平均延迟" value={`${stats.avgLatency}ms`} icon={Clock} tone="warning" delay={0.15} hint="端到端响应耗时" />
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
                <TableHead className="w-10" />
                <TableHead>时间</TableHead>
                <TableHead>用户 / API Key</TableHead>
                <TableHead>模型 / 上游</TableHead>
                <TableHead className="text-right">Token</TableHead>
                <TableHead>延迟</TableHead>
                <TableHead>状态</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.map((log, index) => (
                <LogRow
                  key={log.id}
                  log={log}
                  index={index}
                  expanded={expanded === log.id}
                  attempts={attemptsByLog[log.id] ?? []}
                  attemptsLoading={Boolean(attemptsLoading[log.id])}
                  onToggle={() => toggleLog(log)}
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
    </div>
  );
}

interface LogRowProps {
  log: RequestLog;
  index: number;
  expanded: boolean;
  attempts: RequestAttemptLog[];
  attemptsLoading: boolean;
  onToggle: () => void;
}

function LogRow({ log, index, expanded, attempts, attemptsLoading, onToggle }: LogRowProps) {
  const tokensCacheRead = log.tokensCacheRead ?? 0;
  const tokensCacheWrite = log.tokensCacheWrite ?? 0;
  const tokensReasoning = log.tokensReasoning ?? 0;

  return (
    <>
      <motion.tr
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: index * 0.02, ease: [0.22, 1, 0.36, 1] }}
        className="cursor-pointer border-b transition-colors hover:bg-muted/40"
        onClick={onToggle}
      >
        <TableCell>
          <ChevronRight className={cn('h-4 w-4 text-muted-foreground transition-transform', expanded && 'rotate-90')} />
        </TableCell>
        <TableCell>
          <div className="text-sm">{formatDateTime(log.timestamp)}</div>
          <div className="font-mono text-[11px] text-muted-foreground">{log.id}</div>
          {log.requestId && <div className="font-mono text-[11px] text-muted-foreground">{log.requestId}</div>}
        </TableCell>
        <TableCell>
          <div className="text-sm font-medium">{log.userEmail}</div>
          <div className="text-xs text-muted-foreground">{log.apiKeyName}</div>
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-1.5">
            <Badge variant="secondary" className="font-mono text-[11px]">
              {log.model}
            </Badge>
          </div>
          <div className="mt-1 inline-flex items-center gap-1 text-xs text-muted-foreground">
            <Server className="h-3 w-3" />
            {log.upstreamName}
          </div>
          {log.sourceKeyAlias && <div className="mt-1 text-xs text-muted-foreground">Key: {log.sourceKeyAlias}</div>}
        </TableCell>
        <TableCell className="text-right">
          <div className="font-mono text-sm">{formatNumberFull(log.tokensTotal)}</div>
          <div className="font-mono text-[11px] text-muted-foreground">
            P {log.tokensPrompt} / C {log.tokensCompletion}
          </div>
          {(tokensCacheRead > 0 || tokensCacheWrite > 0 || tokensReasoning > 0) && (
            <div className="font-mono text-[10px] text-muted-foreground/70">
              {tokensCacheRead > 0 && <span>缓存读 {tokensCacheRead} </span>}
              {tokensCacheWrite > 0 && <span>缓存写 {tokensCacheWrite} </span>}
              {tokensReasoning > 0 && <span>推理 {tokensReasoning}</span>}
            </div>
          )}
        </TableCell>
        <TableCell>
          <span className={cn('font-mono text-sm font-medium', latencyClass(log.latencyMs))}>{log.latencyMs}ms</span>
        </TableCell>
        <TableCell>
          <StatusBadge
            tone={log.statusText === 'success' ? 'success' : 'destructive'}
            label={log.statusText === 'success' ? `成功 ${log.statusCode}` : `失败 ${log.statusCode}`}
          />
        </TableCell>
      </motion.tr>
      <AnimatePresence>
        {expanded && (
          <TableRow>
            <TableCell colSpan={7} className="bg-muted/20 p-0">
              <motion.div
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                exit={{ opacity: 0, height: 0 }}
                className="overflow-hidden"
              >
                <div className="space-y-4 p-4">
                  <div className="grid gap-3 rounded-lg border bg-background p-3 text-xs md:grid-cols-2 lg:grid-cols-4">
                    <DetailItem label="协议" value={log.protocol ?? '-'} />
                    <DetailItem label="路径" value={log.path ?? '-'} mono />
                    <DetailItem label="流式" value={log.stream ? '是' : '否'} />
                    <DetailItem label="尝试次数" value={String(log.attemptCount ?? attempts.length ?? 0)} />
                  </div>
                  <div className="grid gap-3 rounded-lg border bg-background p-3 text-xs md:grid-cols-2 lg:grid-cols-5">
                    <DetailItem label="输入 Tokens" value={formatNumberFull(log.tokensPrompt)} mono />
                    <DetailItem label="输出 Tokens" value={formatNumberFull(log.tokensCompletion)} mono />
                    <DetailItem label="缓存读取" value={formatNumberFull(tokensCacheRead)} mono />
                    <DetailItem label="缓存写入" value={formatNumberFull(tokensCacheWrite)} mono />
                    <DetailItem label="推理 Tokens" value={formatNumberFull(tokensReasoning)} mono />
                  </div>
                  <AttemptList attempts={attempts} loading={attemptsLoading} />
                  <div className="grid gap-4 lg:grid-cols-2">
                    <PayloadCard title="请求头" value={prettyJson(log.requestHeaders)} />
                    <PayloadCard title="响应头" value={prettyJson(log.responseHeaders)} />
                  </div>
                  <div className="grid gap-4 lg:grid-cols-2">
                    <PayloadCard title="请求负载" value={prettyJson(log.requestPayload)} />
                    {log.statusText === 'error' ? (
                      <div className="rounded-lg border border-destructive/20 bg-destructive/[0.04] p-4">
                        <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-destructive">
                          <AlertTriangle className="h-4 w-4" />
                          错误信息
                        </div>
                        <p className="text-sm text-muted-foreground">{log.errorMessage ?? 'Unknown upstream error'}</p>
                      </div>
                    ) : (
                      <PayloadCard title="响应负载" value={prettyJson(log.responsePayload)} />
                    )}
                  </div>
                </div>
              </motion.div>
            </TableCell>
          </TableRow>
        )}
      </AnimatePresence>
    </>
  );
}

function DetailItem({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
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
          <div key={attempt.id} className="grid gap-3 p-3 text-xs md:grid-cols-[72px_1fr_120px_120px]">
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
          </div>
        ))}
      </div>
    </div>
  );
}

function PayloadCard({ title, value }: { title: string; value: string }) {
  return (
    <div className="overflow-hidden rounded-lg border bg-background">
      <div className="border-b px-3 py-2 text-xs font-semibold text-muted-foreground">{title}</div>
      <pre className="max-h-72 overflow-auto p-3 text-xs leading-relaxed text-muted-foreground">{value}</pre>
    </div>
  );
}
