import { useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import { BarChart3, Coins, Download, Hash, Loader2, Trophy, Users } from 'lucide-react';
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Progress, Tabs, TabsList, TabsTrigger } from '@relay-api/ui';
import { formatCurrency, formatNumber, formatNumberFull, type PlatformModel, type UsageStats } from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { downloadTextFile, toCsv } from '@/lib/download';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { AreaTrendChart } from '@/components/charts/AreaTrendChart';
import { StackedSegmentBar } from '@/components/charts/StackedSegmentBar';
import { ProviderIcon } from '@/components/common/ProviderIcon';

type Period = 'day' | 'week' | 'month';

const emptyUsage: UsageStats = {
  totalTokens: 0,
  totalCost: 0,
  totalRequests: 0,
  trend: [],
  byModel: [],
  byUser: [],
};

export default function Page() {
  const [period, setPeriod] = useState<Period>('week');
  const [isExporting, setIsExporting] = useState(false);
  const [usage, setUsage] = useState<UsageStats>(emptyUsage);
  const [models, setModels] = useState<PlatformModel[]>([]);

  useEffect(() => {
    Promise.all([adminApi.usageStats(period), adminApi.models()])
      .then(([usageResponse, modelsResponse]) => {
        setUsage(usageResponse.data);
        setModels(modelsResponse.data);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载用量统计失败')));
  }, [period]);

  const handleExport = async () => {
    setIsExporting(true);
    try {
      const response = await adminApi.usageStats(period);
      downloadTextFile(
        `relay-global-usage-${period}-${new Date().toISOString().slice(0, 10)}.csv`,
        toCsv(response.data.trend),
        'text/csv;charset=utf-8',
      );
      toast.success('全局用量明细已导出');
    } catch (error) {
      toast.error(getErrorMessage(error, '导出失败'));
    } finally {
      setIsExporting(false);
    }
  };

  const topUsers = useMemo(() => {
    return usage.byUser.slice(0, 5).map((user) => ({
      ...user,
      pct: Math.round(user.percentage),
    }));
  }, [usage.byUser]);

  const topModels = useMemo(() => {
    const modelMap = new Map(models.map((model) => [model.name, model]));
    return usage.byModel.slice(0, 5).map((item) => ({
        id: item.model,
        name: item.model || 'unknown',
        provider: modelMap.get(item.model)?.provider ?? 'OpenAI',
        tokens: item.tokens,
        cost: usage.totalTokens > 0 ? (item.tokens / usage.totalTokens) * usage.totalCost : 0,
      }));
  }, [models, usage.byModel, usage.totalCost, usage.totalTokens]);

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="平台用量"
        title="全局用量统计"
        description="按时间、模型和用户维度观察全平台 Token 消耗 with 成本走势。"
        actions={
          <div className="flex items-center gap-3">
            <Tabs value={period} onValueChange={(value) => setPeriod(value as Period)}>
              <TabsList>
                <TabsTrigger value="day">日</TabsTrigger>
                <TabsTrigger value="week">周</TabsTrigger>
                <TabsTrigger value="month">月</TabsTrigger>
              </TabsList>
            </Tabs>
            <Button onClick={handleExport} disabled={isExporting} className="shadow-sm">
              {isExporting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
              导出
            </Button>
          </div>
        }
      />

      <div className="grid gap-6 md:grid-cols-3">
        <StatCard label="总 Tokens" value={formatNumberFull(usage.totalTokens)} icon={Hash} tone="primary" delay={0} hint="当前筛选周期" />
        <StatCard label="总成本" value={formatCurrency(usage.totalCost)} icon={Coins} tone="warning" delay={0.05} hint="按模型倍率估算" />
        <StatCard label="请求量" value={formatNumberFull(usage.totalRequests)} icon={BarChart3} tone="success" delay={0.1} hint="成功与失败请求合计" />
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2 border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle>用量趋势</CardTitle>
            <CardDescription>
              当前维度: {period === 'day' ? '按日' : period === 'week' ? '按周' : '按月'} · Tokens 与成本同步观测
            </CardDescription>
          </CardHeader>
          <CardContent>
            <AreaTrendChart data={usage.trend} height={320} />
          </CardContent>
        </Card>

        <Card className="border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle>模型分布</CardTitle>
            <CardDescription>按 Token 消耗聚合</CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            <StackedSegmentBar
              segments={usage.byModel.map((item) => ({
                label: item.model,
                value: item.tokens,
                percentage: item.percentage,
                color: item.color,
              }))}
              valueFormatter={formatNumber}
            />
            <div className="space-y-3">
              {usage.byModel.map((item, index) => (
                <motion.div
                  key={item.model}
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: index * 0.04, ease: [0.22, 1, 0.36, 1] }}
                  className="flex items-center justify-between rounded-lg border border-border/40 bg-muted/20 px-3 py-2"
                >
                  <span className="text-sm font-medium">{item.model}</span>
                  <span className="font-mono text-xs text-muted-foreground">{formatNumberFull(item.tokens)}</span>
                </motion.div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Users className="h-4 w-4 text-primary" />
              Top 5 用户
            </CardTitle>
            <CardDescription>按本周期 Token 消耗排序</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {topUsers.map((user, index) => (
              <motion.div
                key={user.userId}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.04, ease: [0.22, 1, 0.36, 1] }}
                className="space-y-2"
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">{user.email}</div>
                    <div className="text-xs text-muted-foreground">{formatNumberFull(user.requests)} 次请求</div>
                  </div>
                  <span className="font-mono text-sm">{formatNumberFull(user.tokens)}</span>
                </div>
                <Progress value={user.pct} className="h-1.5" />
              </motion.div>
            ))}
          </CardContent>
        </Card>

        <Card className="border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Trophy className="h-4 w-4 text-amber-500" />
              Top 5 模型
            </CardTitle>
            <CardDescription>成本贡献最高的模型</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {topModels.map((model, index) => (
              <motion.div
                key={model.id}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.04, ease: [0.22, 1, 0.36, 1] }}
                className="flex items-center gap-3 rounded-lg border border-border/40 bg-muted/20 px-3 py-2.5"
              >
                <ProviderIcon provider={model.provider} size="md" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{model.name}</div>
                  <div className="text-xs text-muted-foreground">{formatNumberFull(model.tokens)} tokens</div>
                </div>
                <div className="text-right">
                  <div className="font-mono text-sm">{formatCurrency(model.cost)}</div>
                  <div className="text-[11px] text-muted-foreground">估算成本</div>
                </div>
              </motion.div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
