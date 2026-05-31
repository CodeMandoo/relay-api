import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Coins,
  Download,
  Loader2,
  Server,
  Sparkles,
  Users,
} from 'lucide-react';
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Progress,
  cn,
} from '@relay-api/ui';
import {
  formatCurrency,
  formatNumberFull,
  type PlatformModel,
  type SourceStatus,
  type UsageStats,
} from '@relay-api/lib';
import { adminApi, type DashboardMetrics, getErrorMessage } from '@/lib/api';
import { downloadTextFile } from '@/lib/download';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusDot } from '@/components/common/StatusDot';
import { StatusBadge } from '@/components/common/StatusBadge';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { UsageBarChart } from '@/components/charts/UsageBarChart';

const statusDotTone = (s: SourceStatus): 'online' | 'offline' | 'idle' => {
  if (s === 'online') return 'online';
  if (s === 'offline') return 'offline';
  return 'idle';
};

const statusBadgeTone = (s: SourceStatus): 'success' | 'destructive' | 'neutral' => {
  if (s === 'online') return 'success';
  if (s === 'offline') return 'destructive';
  return 'neutral';
};

const statusLabel = (s: SourceStatus): string => {
  if (s === 'online') return '在线';
  if (s === 'offline') return '离线';
  return '已禁用';
};

const changeText = (value: number): string => {
  if (value > 0) return `+${value}%`;
  if (value < 0) return `${value}%`;
  return '0%';
};

const changeClass = (value: number): string => {
  if (value > 0) return 'text-emerald-500';
  if (value < 0) return 'text-destructive';
  return 'text-muted-foreground';
};

const emptyDashboard: DashboardMetrics = {
  todayRequests: 0,
  todayRequestsChangePct: 0,
  activeUsers: 0,
  activeUsersChange: 0,
  upstreamOnline: 0,
  upstreamTotal: 0,
  monthlySpend: 0,
  monthlySpendPct: 0,
  trendChangePct: 0,
  trend7d: [],
  upstreamStatuses: [],
};

const emptyUsage: UsageStats = {
  totalTokens: 0,
  totalCost: 0,
  totalRequests: 0,
  trend: [],
  byModel: [],
  byUser: [],
};

export default function Page() {
  const [isExporting, setIsExporting] = useState(false);
  const [dashboard, setDashboard] = useState<DashboardMetrics>(emptyDashboard);
  const [models, setModels] = useState<PlatformModel[]>([]);
  const [usage, setUsage] = useState<UsageStats>(emptyUsage);

  useEffect(() => {
    Promise.all([adminApi.dashboard(), adminApi.models(), adminApi.usageStats('week')])
      .then(([dashboardResponse, modelsResponse, usageResponse]) => {
        setDashboard(dashboardResponse.data);
        setModels(modelsResponse.data);
        setUsage(usageResponse.data);
      })
      .catch((error) => {
        toast.error(getErrorMessage(error, '加载控制面板失败'));
      });
  }, []);

  const handleExport = async () => {
    setIsExporting(true);
    try {
      const [dashboardResponse, usageResponse, logsResponse] = await Promise.all([
        adminApi.dashboard(),
        adminApi.usageStats('week'),
        adminApi.logs(),
      ]);
      downloadTextFile(
        `relay-audit-${new Date().toISOString().slice(0, 10)}.json`,
        JSON.stringify({
          exportedAt: new Date().toISOString(),
          dashboard: dashboardResponse.data,
          usage: usageResponse.data,
          recentLogs: logsResponse.data,
        }, null, 2),
        'application/json;charset=utf-8',
      );
      toast.success('审计报告已导出');
    } catch (error) {
      toast.error(getErrorMessage(error, '导出失败'));
    } finally {
      setIsExporting(false);
    }
  };

  const trendData = useMemo(
    () => dashboard.trend7d.map((d) => ({ label: d.day, value: d.value, isToday: d.isToday })),
    [dashboard.trend7d],
  );

  const topModels = useMemo(() => {
    const usageByModel = new Map(usage.byModel.map((item) => [item.model, item.tokens]));
    const list = models
      .filter((m) => m.enabled || usageByModel.has(m.name))
      .slice(0, 5)
      .map((m) => ({ ...m, requests: usageByModel.get(m.name) ?? 0 }));
    const max = Math.max(...list.map((m) => m.requests), 1);
    return list.map((m) => ({ ...m, pct: Math.round((m.requests / max) * 100) }));
  }, [models, usage.byModel]);

  const runtimeAlerts = useMemo(() => {
    return dashboard.upstreamStatuses.filter((item) => item.status !== 'online').map((item, index) => ({
      id: `${item.name}-${index}`,
      title: `${item.name} 当前${statusLabel(item.status)}`,
      meta: item.status === 'disabled' ? '已从调度池移除' : '上游不可用 · 需要检查',
      tone: item.status === 'offline' ? 'destructive' : 'warning',
    })) satisfies { id: string; title: string; meta: string; tone: 'destructive' | 'warning' }[];
  }, [dashboard.upstreamStatuses]);

  return (
    <div className="space-y-10">
      <PageHeader
        eyebrow="运营概览"
        title="平台实时动态"
        description="实时洞察请求流量、节点健康和成本消耗 — 让平台每一次心跳尽在掌握。"
        actions={
          <Button variant="outline" className="shadow-sm font-bold" onClick={handleExport} disabled={isExporting}>
            {isExporting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
            导出审计报告
          </Button>
        }
      />

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="今日请求量"
          value={formatNumberFull(dashboard.todayRequests)}
          hint={
            <span className="inline-flex items-center gap-1">
              <span className={cn('font-bold', changeClass(dashboard.todayRequestsChangePct))}>
                {changeText(dashboard.todayRequestsChangePct)}
              </span>
              <span className="text-muted-foreground/60">较昨日</span>
            </span>
          }
          icon={Activity}
          tone="primary"
          delay={0}
        />
        <StatCard
          label="活跃用户"
          value={formatNumberFull(dashboard.activeUsers)}
          hint={
            <span className="inline-flex items-center gap-1">
              <span className="font-bold text-emerald-500">+{dashboard.activeUsersChange}</span>
              <span className="text-muted-foreground/60">较上周</span>
            </span>
          }
          icon={Users}
          tone="success"
          delay={0.05}
        />
        <StatCard
          label="上游在线"
          value={`${dashboard.upstreamOnline} / ${dashboard.upstreamTotal}`}
          hint={
            <span className="text-destructive font-bold">
              {dashboard.upstreamTotal - dashboard.upstreamOnline} 个节点异常
            </span>
          }
          icon={Server}
          tone="neutral"
          delay={0.1}
        />
        <StatCard
          label="本月消费"
          value={formatCurrency(dashboard.monthlySpend)}
          hint={
            <span className="inline-flex items-center gap-1">
              <span className={cn('font-bold', changeClass(dashboard.monthlySpendPct))}>
                {changeText(dashboard.monthlySpendPct)}
              </span>
              <span className="text-muted-foreground/60">较上月同期</span>
            </span>
          }
          icon={Coins}
          tone="warning"
          delay={0.15}
        />
      </div>

      <div className="grid gap-8 lg:grid-cols-3">
        <Card className="overflow-hidden border-border/40 lg:col-span-2 shadow-sm">
          <CardHeader className="flex flex-row items-start justify-between space-y-0 border-b bg-muted/10">
            <div className="space-y-1">
              <CardTitle className="text-lg font-bold">用量趋势</CardTitle>
              <CardDescription>最近 7 天的请求量变化</CardDescription>
            </div>
            <div className="flex items-center gap-1.5 rounded-full border border-primary/10 bg-primary/5 px-3 py-1 text-[10px] font-bold text-primary uppercase tracking-wider">
              <Sparkles className="h-3 w-3" />
              趋势 {changeText(dashboard.trendChangePct)}
            </div>
          </CardHeader>
          <CardContent className="pt-6">
            <UsageBarChart data={trendData} height={300} />
          </CardContent>
        </Card>

        <Card className="overflow-hidden border-border/40 shadow-sm">
          <CardHeader className="border-b bg-muted/10">
            <CardTitle className="text-lg font-bold">服务状态</CardTitle>
            <CardDescription>实时节点监控</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <div className="divide-y divide-border/40">
              {dashboard.upstreamStatuses.map((u, i) => (
                <motion.div
                  key={u.name}
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: i * 0.04, ease: [0.22, 1, 0.36, 1] }}
                  className="flex items-center justify-between gap-3 px-6 py-4 transition-colors hover:bg-muted/30"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <StatusDot tone={statusDotTone(u.status)} pulsing={u.pulsing ?? u.status === 'online'} />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-bold">{u.name}</p>
                      <p className="text-[11px] font-medium text-muted-foreground/60">
                        {u.status === 'online'
                          ? `负载 ${u.load}% · 延迟 ${u.latencyMs}ms`
                          : '节点不可用 · 已隔离'}
                      </p>
                    </div>
                  </div>
                  <StatusBadge tone={statusBadgeTone(u.status)} label={statusLabel(u.status)} className="font-bold text-[10px]" />
                </motion.div>
              ))}
            </div>
            <div className="p-6">
              <Button asChild variant="ghost" className="w-full font-bold text-xs">
                <Link to="/admin/sources">
                  查看所有上游源
                  <ArrowRight className="ml-2 h-3.5 w-3.5" />
                </Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-8 lg:grid-cols-2">
        <Card className="border-border/40 shadow-sm">
          <CardHeader className="border-b bg-muted/10">
            <CardTitle className="flex items-center gap-2 text-lg font-bold">
              <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600">
                <AlertTriangle className="h-4 w-4" />
              </span>
              最近告警
            </CardTitle>
            <CardDescription>异常事件与频率限制通知</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            {runtimeAlerts.length === 0 ? (
              <div className="rounded-xl border border-border/40 bg-muted/20 p-6 text-sm font-medium text-muted-foreground">
                暂无上游异常
              </div>
            ) : runtimeAlerts.map((a, i) => (
              <motion.div
                key={a.id}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.05, ease: [0.22, 1, 0.36, 1] }}
                className={cn(
                  'flex items-start gap-4 rounded-xl border border-border/40 p-4 transition-all hover:bg-muted/20',
                  a.tone === 'destructive' && 'bg-destructive/[0.02]',
                  a.tone === 'warning' && 'bg-amber-500/[0.02]',
                )}
              >
                <span
                  className={cn(
                    'mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg shadow-sm',
                    a.tone === 'destructive' && 'bg-destructive/10 text-destructive',
                    a.tone === 'warning' && 'bg-amber-500/10 text-amber-600',
                  )}
                >
                  <AlertTriangle className="h-4 w-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-bold leading-tight">{a.title}</p>
                  <p className="mt-1.5 text-xs font-medium text-muted-foreground/60">{a.meta}</p>
                </div>
              </motion.div>
            ))}
          </CardContent>
        </Card>

        <Card className="border-border/40 shadow-sm">
          <CardHeader className="border-b bg-muted/10">
            <CardTitle className="flex items-center gap-2 text-lg font-bold">
              <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-primary/10 text-primary">
                <Sparkles className="h-4 w-4" />
              </span>
              热门模型
            </CardTitle>
            <CardDescription>今日请求量 Top 5</CardDescription>
          </CardHeader>
          <CardContent className="space-y-6 pt-6">
            {topModels.map((m, i) => (
              <motion.div
                key={m.id}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.04, ease: [0.22, 1, 0.36, 1] }}
                className="flex items-center gap-4"
              >
                <ProviderIcon provider={m.provider} size="md" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="truncate text-sm font-bold">{m.name}</span>
                    <span className="shrink-0 font-mono text-[10px] font-bold text-muted-foreground/60">
                      {formatNumberFull(m.requests)}
                    </span>
                  </div>
                  <Progress value={m.pct} className="mt-2 h-1.5 bg-muted" />
                </div>
              </motion.div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
