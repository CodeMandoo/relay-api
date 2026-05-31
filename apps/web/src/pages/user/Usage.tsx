import { useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import { BarChart3, CalendarDays, Coins, Download, Hash, KeyRound, Loader2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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
} from '@relay-api/ui';
import {
  formatCurrency,
  formatDate,
  formatNumber,
  formatNumberFull,
  type ApiKey,
  type UsageDetailRow,
  type UsageStats,
} from '@relay-api/lib';
import { getErrorMessage, userApi } from '@/lib/api';
import { downloadTextFile, toCsv } from '@/lib/download';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { AreaTrendChart } from '@/components/charts/AreaTrendChart';
import { StackedSegmentBar } from '@/components/charts/StackedSegmentBar';

type Range = 'day' | 'week' | 'month';

const emptyUsage: UsageStats = {
  totalTokens: 0,
  totalCost: 0,
  totalRequests: 0,
  trend: [],
  byModel: [],
  byUser: [],
};

export default function Page() {
  const [range, setRange] = useState<Range>('week');
  const [apiKey, setApiKey] = useState('all');
  const [isExporting, setIsExporting] = useState(false);
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [usage, setUsage] = useState<UsageStats>(emptyUsage);
  const [rows, setRows] = useState<UsageDetailRow[]>([]);

  useEffect(() => {
    Promise.all([userApi.apiKeys(), userApi.usage(range, apiKey)])
      .then(([keysResponse, usageResponse]) => {
        setKeys(keysResponse.data);
        setUsage(usageResponse.data.stats);
        setRows(usageResponse.data.rows);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载用量明细失败')));
  }, [apiKey, range]);

  const handleExport = async () => {
    setIsExporting(true);
    try {
      const response = await userApi.usage(range, apiKey);
      downloadTextFile(
        `relay-usage-${range}-${new Date().toISOString().slice(0, 10)}.csv`,
        toCsv(response.data.rows),
        'text/csv;charset=utf-8',
      );
      toast.success('对账单已准备就绪，开始下载');
    } catch (error) {
      toast.error(getErrorMessage(error, '生成失败'));
    } finally {
      setIsExporting(false);
    }
  };

  const totals = useMemo(() => {
    const requests = rows.reduce((sum, row) => sum + row.requests, 0);
    const tokens = rows.reduce((sum, row) => sum + row.promptTokens + row.completionTokens, 0);
    const cost = rows.reduce((sum, row) => sum + row.estimatedCost, 0);
    const avg = rows.length === 0 ? 0 : Math.round(tokens / rows.length);
    return { requests, tokens, cost, avg };
  }, [rows]);

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="消耗明细"
        title="用量明细"
        description="按 API Key 和时间维度查看请求量、Token 消耗和估算成本。"
        actions={
          <div className="flex items-center gap-3">
            <Tabs value={range} onValueChange={(value) => setRange(value as Range)}>
              <TabsList className="bg-muted/60">
                <TabsTrigger value="day">日</TabsTrigger>
                <TabsTrigger value="week">周</TabsTrigger>
                <TabsTrigger value="month">月</TabsTrigger>
              </TabsList>
            </Tabs>
            <Select value={apiKey} onValueChange={setApiKey}>
              <SelectTrigger className="h-9 w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部 API Key</SelectItem>
                {keys.map((key) => (
                  <SelectItem key={key.id} value={key.id}>
                    {key.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={handleExport} disabled={isExporting} className="shadow-sm">
              {isExporting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
              导出
            </Button>
          </div>
        }
      />

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="总请求" value={formatNumberFull(totals.requests)} icon={BarChart3} tone="primary" delay={0} hint={apiKey === 'all' ? '全部 Key' : '当前 Key'} />
        <StatCard label="总 Tokens" value={formatNumber(totals.tokens)} icon={Hash} tone="success" delay={0.05} hint={formatNumberFull(totals.tokens)} />
        <StatCard label="估算成本" value={formatCurrency(totals.cost)} icon={Coins} tone="warning" delay={0.1} hint="按当前倍率估算" />
        <StatCard label="日均 Tokens" value={formatNumber(totals.avg)} icon={CalendarDays} tone="neutral" delay={0.15} hint="按展示天数均摊" />
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2 border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle>Token 趋势</CardTitle>
            <CardDescription>近 7 日调用走势，按当前 API Key 筛选展示。</CardDescription>
          </CardHeader>
          <CardContent>
            <AreaTrendChart data={usage.trend} height={320} />
          </CardContent>
        </Card>
        <Card className="border-border/40 shadow-sm">
          <CardHeader>
            <CardTitle>模型用量分布</CardTitle>
            <CardDescription>按 Token 聚合</CardDescription>
          </CardHeader>
          <CardContent>
            <StackedSegmentBar
              segments={usage.byModel.map((item) => ({
                label: item.model,
                value: item.tokens,
                percentage: item.percentage,
                color: item.color,
              }))}
              valueFormatter={formatNumber}
            />
          </CardContent>
        </Card>
      </div>

      <Card className="overflow-hidden border-border/40 shadow-sm">
        <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b bg-muted/10">
          <div>
            <CardTitle>明细记录</CardTitle>
            <CardDescription>Prompt、Completion 和成本按日期汇总。</CardDescription>
          </div>
          <Badge variant="secondary" className="w-fit border-primary/20 bg-primary/5 text-primary font-bold">
            <KeyRound className="mr-1.5 h-3.5 w-3.5" />
            {apiKey === 'all' ? '全部 API Key' : keys.find((key) => key.id === apiKey)?.name}
          </Badge>
        </CardHeader>
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/40 hover:bg-muted/40">
              <TableHead className="px-6">日期</TableHead>
              <TableHead className="text-right">请求数</TableHead>
              <TableHead className="text-right">Prompt Tokens</TableHead>
              <TableHead className="text-right">Completion Tokens</TableHead>
              <TableHead className="text-right">总 Tokens</TableHead>
              <TableHead className="text-right pr-6">成本</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, index) => {
              const total = row.promptTokens + row.completionTokens;
              return (
                <motion.tr
                  key={`${row.date}-${index}`}
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: index * 0.025, ease: [0.22, 1, 0.36, 1] }}
                  className="border-b border-border/40 transition-colors hover:bg-muted/40"
                >
                  <TableCell className="px-6 font-medium">{formatDate(row.date)}</TableCell>
                  <TableCell className="text-right font-mono text-sm">{formatNumberFull(row.requests)}</TableCell>
                  <TableCell className="text-right font-mono text-sm text-muted-foreground">{formatNumberFull(row.promptTokens)}</TableCell>
                  <TableCell className="text-right font-mono text-sm text-muted-foreground">{formatNumberFull(row.completionTokens)}</TableCell>
                  <TableCell className="text-right font-mono font-bold">{formatNumberFull(total)}</TableCell>
                  <TableCell className="text-right font-mono pr-6 font-semibold text-foreground">{formatCurrency(row.estimatedCost)}</TableCell>
                </motion.tr>
              );
            })}
          </TableBody>
        </Table>
        <CardContent className="border-t bg-muted/20 py-3 text-xs text-muted-foreground">
          展示 {rows.length} 条聚合记录 · 实际账单以后端 usage_logs 为准。
        </CardContent>
      </Card>
    </div>
  );
}
