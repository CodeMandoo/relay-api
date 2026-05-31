import { motion } from 'framer-motion';
import { useNavigate } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import {
  Activity,
  ArrowRight,
  BadgeCheck,
  Blocks,
  CalendarDays,
  Hash,
  KeyRound,
  Layers,
  Sparkles,
  TrendingUp,
} from 'lucide-react';
import { Button, Card, Progress, cn } from '@relay-api/ui';
import {
  formatNumber,
  formatNumberFull,
  formatDate,
  type UsageStats,
  type UserModel,
  type UserQuota,
} from '@relay-api/lib';
import { getErrorMessage, userApi } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { UsageBarChart } from '@/components/charts/UsageBarChart';

const emptyQuota: UserQuota = {
  used: 0,
  total: 0,
  remaining: 0,
  percentageUsed: 0,
  billingPeriodStart: new Date().toISOString(),
  billingPeriodEnd: new Date().toISOString(),
  todayRequests: 0,
  todayTokens: 0,
  monthRequests: 0,
  monthTokens: 0,
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
  const navigate = useNavigate();
  const [quota, setQuota] = useState<UserQuota>(emptyQuota);
  const [usage, setUsage] = useState<UsageStats>(emptyUsage);
  const [models, setModels] = useState<UserModel[]>([]);
  const used = formatNumber(quota.used);
  const trendData = usage.trend.map((t) => ({
    label: t.date.slice(5),
    value: t.tokens,
  }));

  useEffect(() => {
    Promise.all([userApi.dashboard(), userApi.models()])
      .then(([dashboardResponse, modelsResponse]) => {
        setQuota(dashboardResponse.data.quota);
        setUsage(dashboardResponse.data.usage);
        setModels(modelsResponse.data);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载用户概览失败')));
  }, []);

  return (
    <div className="space-y-10">
      <PageHeader
        eyebrow="用量概览"
        title="欢迎回来"
        description="掌握您当前周期内的额度、调用和费用, 一切尽在掌握。"
        actions={
          <Button
            size="lg"
            onClick={() => navigate('/user/api-keys')}
            className="rounded-xl font-bold shadow-xl shadow-primary/10"
          >
            <KeyRound className="mr-2 h-4 w-4" />
            获取 API Key
          </Button>
        }
      />

      {/* Hero Quota + Trend */}
      <div className="grid gap-8 lg:grid-cols-3">
        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
          className="lg:col-span-2"
        >
          <Card className="relative overflow-hidden border-border/40 p-8 shadow-sm">
            <div className="pointer-events-none absolute -right-24 -top-24 h-72 w-72 rounded-full bg-primary/5 blur-3xl" />
            <div className="pointer-events-none absolute inset-0 dotted-grid opacity-[0.15]" />

            <div className="relative flex flex-col gap-8">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="inline-flex items-center gap-1.5 rounded-full border border-primary/10 bg-primary/5 px-3 py-1 text-[10px] font-bold uppercase tracking-wider text-primary backdrop-blur">
                    <BadgeCheck className="h-3 w-3" />
                    当前计费周期
                  </div>
                  <div className="mt-4 text-sm font-medium text-muted-foreground/60">
                    {formatDate(quota.billingPeriodStart)} — {formatDate(quota.billingPeriodEnd)}
                  </div>
                </div>
                <div className="hidden text-right sm:block">
                  <div className="text-[10px] font-bold uppercase tracking-[0.2em] text-muted-foreground/40">
                    剩余额度
                  </div>
                  <div className="mt-1 text-3xl font-bold text-emerald-600 tracking-tighter">
                    {formatNumber(quota.remaining)}
                  </div>
                </div>
              </div>

              <div className="flex items-end gap-3">
                <div className="text-6xl font-bold tracking-tighter text-foreground sm:text-7xl">       
                  {used}
                </div>
                <div className="pb-3 text-sm font-bold text-muted-foreground/40 uppercase tracking-widest">
                  已用
                </div>
              </div>

              <div className="space-y-3">
                <Progress value={quota.percentageUsed} className="h-2 bg-muted/50" />
                <div className="flex items-center justify-between text-[11px] font-bold uppercase tracking-wider">
                  <span className="text-foreground">
                    已消耗 {quota.percentageUsed.toFixed(1)}%
                  </span>
                  <span className="text-muted-foreground/60">
                    {quota.percentageUsed >= 80
                      ? '接近限额'
                      : '用量节奏稳定'}
                  </span>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-3 pt-2">
                <Button onClick={() => navigate('/user/api-keys')} className="rounded-lg font-bold">
                  管理 API Key
                </Button>
                <Button variant="outline" onClick={() => navigate('/user/usage')} className="rounded-lg font-bold">
                  查看用量明细
                </Button>
              </div>
            </div>
          </Card>
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 0.1, ease: [0.22, 1, 0.36, 1] }}
        >
          <Card className="flex h-full flex-col border-border/40 p-6 shadow-sm">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-[10px] font-bold uppercase tracking-[0.2em] text-muted-foreground/40">
                  近 7 日 Tokens
                </div>
                <div className="mt-1 text-2xl font-bold tracking-tighter">
                  {formatNumber(usage.totalTokens)}
                </div>
              </div>
              <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/20 bg-emerald-500/5 px-2.5 py-1 text-[10px] font-bold text-emerald-600">
                <TrendingUp className="h-3 w-3" />
                +12.4%
              </span>
            </div>
            <div className="mt-6 flex-1">
              <UsageBarChart
                data={trendData}
                height={220}
              />
            </div>
          </Card>
        </motion.div>
      </div>

      {/* Quick stat cards */}
      <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="今日请求"
          value={formatNumberFull(quota.todayRequests)}
          hint="较昨日 +12%"
          icon={Activity}
          tone="primary"
          delay={0.2}
        />
        <StatCard
          label="今日 Tokens"
          value={formatNumber(quota.todayTokens)}
          hint="实际消耗"
          icon={Hash}
          tone="success"
          delay={0.25}
        />
        <StatCard
          label="本月请求"
          value={formatNumberFull(quota.monthRequests)}
          hint="覆盖全部 Key"
          icon={CalendarDays}
          tone="neutral"
          delay={0.3}
        />
        <StatCard
          label="本月 Tokens"
          value={formatNumber(quota.monthTokens)}
          hint="累计总量"
          icon={Layers}
          tone="warning"
          delay={0.35}
        />
      </div>

      {/* Action cards */}
      <div className="grid gap-8 md:grid-cols-2">
        <ActionCard
          title="创建 API Key"
          description="创建新的安全密钥, 用于在您的应用程序中调用 Relay 平台。"
          icon={KeyRound}
          onClick={() => navigate('/user/api-keys')}
          delay={0.4}
        />
        <ActionCard
          title="浏览可用模型"
          description="探索平台支持的全部模型、计费价格以及实时性能测试。"
          icon={Blocks}
          onClick={() => navigate('/user/models')}
          delay={0.45}
          extra={
            <div className="flex items-center gap-1.5 text-[10px] font-bold text-primary/60 uppercase tracking-widest">
              <Sparkles className="h-3 w-3" />
              {models.filter((model) => model.status === 'online').length} 个模型在线
            </div>
          }
        />
      </div>
    </div>
  );
}

interface ActionCardProps {
  title: string;
  description: string;
  icon: typeof KeyRound;
  onClick: () => void;
  delay?: number;
  extra?: React.ReactNode;
}

function ActionCard({
  title,
  description,
  icon: Icon,
  onClick,
  delay = 0,
  extra,
}: ActionCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.6, delay, ease: [0.22, 1, 0.36, 1] }}
      className="h-full"
    >
      <Card
        className="group relative flex h-full min-h-[200px] cursor-pointer overflow-hidden border-border/40 p-8 shadow-sm transition-all duration-500 ease-emphasized hover:-translate-y-1 hover:border-primary/20 hover:shadow-xl hover:shadow-primary/5 active:scale-[0.98]"
        onClick={onClick}
      >
        <div className="flex min-h-0 w-full items-start justify-between gap-6">
          <div className="min-w-0 space-y-4">
            <div className="inline-flex h-12 w-12 items-center justify-center rounded-[14px] bg-primary/10 text-primary shadow-sm transition-transform duration-500 group-hover:rotate-6 group-hover:scale-110">
              <Icon className="h-6 w-6" />
            </div>
            <div className="space-y-1.5">
              <div className="text-lg font-bold tracking-tight">{title}</div>
              <p className="text-sm font-medium leading-relaxed text-muted-foreground/60">{description}</p>
            </div>
            {extra}
          </div>
          <div className="mt-1 flex h-10 w-10 shrink-0 items-center justify-center rounded-full border border-border/40 bg-muted/10 opacity-0 transition-all duration-500 group-hover:translate-x-0 group-hover:opacity-100 -translate-x-2">
            <ArrowRight className="h-4 w-4" />
          </div>
        </div>
      </Card>
    </motion.div>
  );
}
