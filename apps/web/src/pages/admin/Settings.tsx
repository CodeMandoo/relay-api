import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import {
  AlertTriangle,
  BellRing,
  DatabaseBackup,
  Loader2,
  RotateCcw,
  Save,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
  cn,
} from '@relay-api/ui';
import { type PlatformSettings } from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';

const nav = ['基础信息', '注册与登录', 'API 行为', '通知与告警', '危险区'] as const;
const SETTINGS_SCROLL_OFFSET = 112;

const easeOutCubic = (value: number) => 1 - Math.pow(1 - value, 3);

const defaultSettings: PlatformSettings = {
  platformName: 'Relay API',
  supportEmail: 'support@relay.io',
  openRegistration: true,
  requireInviteCode: true,
  defaultUserBalance: 100,
  maxRetries: 3,
  defaultTimeout: 120,
  streamingEnabled: true,
  hideUpstreamNameFromUsers: false,
};

export default function Page() {
  const [activeTab, setActiveTab] = useState<string>(nav[0]);
  const scrollLockUntilRef = useRef(0);
  const observerFrameRef = useRef<number | null>(null);
  const scrollFrameRef = useRef<number | null>(null);
  const [platformName, setPlatformName] = useState(defaultSettings.platformName);
  const [supportEmail, setSupportEmail] = useState(defaultSettings.supportEmail);
  const [logoUrl, setLogoUrl] = useState('');
  const [language, setLanguage] = useState('zh-CN');
  const [openRegistration, setOpenRegistration] = useState(defaultSettings.openRegistration);
  const [requireInviteCode, setRequireInviteCode] = useState(defaultSettings.requireInviteCode);
  const [defaultUserBalance, setDefaultUserBalance] = useState(String(defaultSettings.defaultUserBalance));
  const [maxRetries, setMaxRetries] = useState(String(defaultSettings.maxRetries));
  const [defaultTimeout, setDefaultTimeout] = useState(String(defaultSettings.defaultTimeout));
  const [streaming, setStreaming] = useState(true);
  const [hideUpstreamNameFromUsers, setHideUpstreamNameFromUsers] = useState(
    defaultSettings.hideUpstreamNameFromUsers ?? false,
  );
  const [alertEmail, setAlertEmail] = useState('ops@relay.io');
  const [sourceAlert, setSourceAlert] = useState(true);
  const [quotaAlert, setQuotaAlert] = useState(true);
  const [saving, setSaving] = useState(false);

  const applySettings = (settings: PlatformSettings) => {
    setPlatformName(settings.platformName);
    setSupportEmail(settings.supportEmail);
    setOpenRegistration(settings.openRegistration);
    setRequireInviteCode(settings.requireInviteCode);
    setDefaultUserBalance(String(settings.defaultUserBalance));
    setMaxRetries(String(settings.maxRetries));
    setDefaultTimeout(String(settings.defaultTimeout));
    setStreaming(settings.streamingEnabled ?? true);
    setHideUpstreamNameFromUsers(settings.hideUpstreamNameFromUsers ?? false);
  };

  useEffect(() => {
    adminApi
      .settings()
      .then((response) => applySettings(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载系统设置失败')));
  }, []);

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        if (Date.now() < scrollLockUntilRef.current) return;

        const next = entries
          .filter((entry) => entry.isIntersecting)
          .sort(
            (a, b) =>
              Math.abs(a.boundingClientRect.top - SETTINGS_SCROLL_OFFSET) -
              Math.abs(b.boundingClientRect.top - SETTINGS_SCROLL_OFFSET),
          )[0]?.target.id;

        if (!next) return;
        if (observerFrameRef.current) {
          window.cancelAnimationFrame(observerFrameRef.current);
        }
        observerFrameRef.current = window.requestAnimationFrame(() => {
          setActiveTab((current) => (current === next ? current : next));
          observerFrameRef.current = null;
        });
      },
      { threshold: 0.1, rootMargin: `-${SETTINGS_SCROLL_OFFSET}px 0px -55% 0px` }
    );

    nav.forEach((id) => {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    });

    return () => {
      observer.disconnect();
      if (observerFrameRef.current) {
        window.cancelAnimationFrame(observerFrameRef.current);
      }
      if (scrollFrameRef.current) {
        window.cancelAnimationFrame(scrollFrameRef.current);
      }
    };
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      const response = await adminApi.updateSettings({
        platformName,
        supportEmail,
        openRegistration,
        requireInviteCode,
        defaultUserBalance: Math.max(0, Number(defaultUserBalance) || 0),
        maxRetries: Math.max(0, Number(maxRetries) || 0),
        defaultTimeout: Math.max(1, Number(defaultTimeout) || 1),
        streamingEnabled: streaming,
        hideUpstreamNameFromUsers,
      });
      applySettings(response.data);
      toast.success('配置已保存', { description: '更改将应用至后续所有请求调度中。' });
    } catch (error) {
      toast.error(getErrorMessage(error, '保存设置失败'));
    } finally {
      setSaving(false);
    }
  };

  const discardChanges = async () => {
    try {
      const response = await adminApi.settings();
      applySettings(response.data);
      toast.info('已恢复最近一次保存的配置');
    } catch (error) {
      toast.error(getErrorMessage(error, '恢复配置失败'));
    }
  };

  const resetDanger = async (label: string) => {
    try {
      if (label.includes('统计')) {
        await adminApi.resetUsage();
      } else {
        await adminApi.clearLogs();
      }
      toast.success(label);
    } catch (error) {
      toast.error(getErrorMessage(error, '操作失败'));
    }
  };

  const scrollToSection = (id: string) => {
    const section = document.getElementById(id);
    if (!section) return;

    const top = Math.max(0, section.getBoundingClientRect().top + window.scrollY - SETTINGS_SCROLL_OFFSET);
    const start = window.scrollY;
    const distance = top - start;
    const duration = Math.min(620, Math.max(360, Math.abs(distance) * 0.42));
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    if (scrollFrameRef.current) {
      window.cancelAnimationFrame(scrollFrameRef.current);
    }

    scrollLockUntilRef.current = Date.now() + duration + 80;
    setActiveTab((current) => (current === id ? current : id));

    if (reduceMotion || Math.abs(distance) < 24) {
      window.scrollTo({ top, behavior: 'auto' });
      scrollLockUntilRef.current = 0;
      return;
    }

    const startedAt = performance.now();
    const animate = (now: number) => {
      const progress = Math.min(1, (now - startedAt) / duration);
      const eased = easeOutCubic(progress);
      window.scrollTo(0, start + distance * eased);
      if (progress < 1) {
        scrollFrameRef.current = window.requestAnimationFrame(animate);
        return;
      }
      scrollFrameRef.current = null;
      scrollLockUntilRef.current = 0;
    };
    scrollFrameRef.current = window.requestAnimationFrame(animate);
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="平台配置"
        title="系统设置"
        description="集中管理品牌、注册策略、代理行为和告警规则。"
      />

      <div className="grid gap-6 lg:grid-cols-[220px_1fr]">
        <aside className="hidden lg:block">
          <div className="sticky top-24 space-y-1 rounded-xl border bg-card p-2 shadow-sm">
            {nav.map((item) => (
              <button
                key={item}
                type="button"
                onClick={() => scrollToSection(item)}
                className={cn(
                  'block w-full rounded-lg px-3 py-2 text-left text-sm transition-[background-color,color,box-shadow] duration-200 ease-out',
                  activeTab === item 
                    ? 'bg-primary/5 text-primary font-bold shadow-[inset_0_0_0_1px_rgba(0,0,0,0.02)]' 
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                {item}
              </button>
            ))}
          </div>
        </aside>

        <div className="space-y-4 pb-24">
          <SettingsSection id="基础信息" icon={Settings2} title="基础信息" description="决定后台展示名称、支持邮箱和默认语言。">
            <div className="grid gap-6 md:grid-cols-2">
              <Field label="平台名称">
                <Input value={platformName} onChange={(e) => setPlatformName(e.target.value)} />
              </Field>
              <Field label="支持邮箱">
                <Input type="email" value={supportEmail} onChange={(e) => setSupportEmail(e.target.value)} />
              </Field>
              <Field label="平台 Logo URL">
                <Input value={logoUrl} onChange={(e) => setLogoUrl(e.target.value)} placeholder="https://example.com/logo.png" />
              </Field>
              <Field label="默认语言">
                <Select value={language} onValueChange={setLanguage}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="zh-CN">简体中文</SelectItem>
                    <SelectItem value="en-US">English</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </SettingsSection>

          <SettingsSection id="注册与登录" icon={ShieldCheck} title="注册与登录" description="控制新用户注册入口和默认额度。">
            <div className="grid gap-4">
              <SwitchRow title="开放注册" description="关闭后仅管理员创建用户可登录。" checked={openRegistration} onCheckedChange={setOpenRegistration} />
              <SwitchRow
                title="必须邀请码"
                description="开启后注册页面必须提交有效邀请码。"
                checked={requireInviteCode}
                onCheckedChange={setRequireInviteCode}
                disabled={!openRegistration}
              />
              <Field label="新用户默认余额">
                <Input type="number" min={0} value={defaultUserBalance} onChange={(e) => setDefaultUserBalance(e.target.value)} />
              </Field>
            </div>
          </SettingsSection>

          <SettingsSection id="API 行为" icon={SlidersHorizontal} title="API 行为" description="设置上游失败后的重试和请求超时边界。">
            <div className="grid gap-6 md:grid-cols-2">
              <Field label="最大重试次数">
                <Input type="number" min={0} max={10} value={maxRetries} onChange={(e) => setMaxRetries(e.target.value)} />
              </Field>
              <Field label="默认超时秒数">
                <Input type="number" min={1} value={defaultTimeout} onChange={(e) => setDefaultTimeout(e.target.value)} />
              </Field>
            </div>
            <div className="mt-4">
              <SwitchRow title="启用流式响应" description="允许 /v1/chat/completions 透传 SSE 流。" checked={streaming} onCheckedChange={setStreaming} />
            </div>
            <div className="mt-4">
              <SwitchRow
                title="用户端隐藏上游名称"
                description="开启后用户模型页仅显示平台中转源，不暴露真实供应商路由名。"
                checked={hideUpstreamNameFromUsers}
                onCheckedChange={setHideUpstreamNameFromUsers}
              />
            </div>
          </SettingsSection>

          <SettingsSection id="通知与告警" icon={BellRing} title="通知与告警" description="将关键异常发送给运维负责人。">
            <div className="grid gap-4">
              <Field label="告警邮件">
                <Input type="email" value={alertEmail} onChange={(e) => setAlertEmail(e.target.value)} />
              </Field>
              <SwitchRow title="上游离线告警" description="上游连续心跳失败时推送告警。" checked={sourceAlert} onCheckedChange={setSourceAlert} />
              <SwitchRow title="用户超额告警" description="用户达到 90% 配额时发送提醒。" checked={quotaAlert} onCheckedChange={setQuotaAlert} />
            </div>
          </SettingsSection>

          <Card id="危险区" className="scroll-mt-28 border-destructive/30 bg-destructive/[0.04]">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-destructive">
                <AlertTriangle className="h-4 w-4" />
                危险区
              </CardTitle>
              <CardDescription>这些操作会影响统计和审计数据，请确认后执行。</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-3">
              <DangerButton icon={RotateCcw} label="重置统计数据" onConfirm={() => resetDanger('统计数据已重置')} />
              <DangerButton icon={DatabaseBackup} label="清空请求日志" onConfirm={() => resetDanger('请求日志已清空')} />
            </CardContent>
          </Card>
        </div>
      </div>

      <div className="fixed inset-x-0 bottom-0 z-30 border-t glass lg:left-[280px]">
        <div className="mx-auto flex max-w-7xl justify-end gap-2 px-4 py-3 sm:px-6 lg:px-8">
          <Button variant="ghost" onClick={discardChanges}>
            取消
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            保存设置
          </Button>
        </div>
      </div>
    </div>
  );
}

function SettingsSection({
  id,
  icon: Icon,
  title,
  description,
  children,
}: {
  id: string;
  icon: typeof Settings2;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <Card id={id} className="scroll-mt-28">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <span className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Icon className="h-4 w-4" />
          </span>
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      {children}
    </div>
  );
}

function SwitchRow({
  title,
  description,
  checked,
  onCheckedChange,
  disabled,
}: {
  title: string;
  description: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <div className={cn('flex items-center justify-between gap-4 rounded-lg border bg-muted/20 p-4', disabled && 'opacity-50')}>
      <div>
        <div className="text-sm font-medium">{title}</div>
        <div className="text-xs text-muted-foreground">{description}</div>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </div>
  );
}

function DangerButton({ icon: Icon, label, onConfirm }: { icon: typeof RotateCcw; label: string; onConfirm: () => void }) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="destructive">
          <Icon className="mr-2 h-4 w-4" />
          {label}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>确认执行「{label}」?</AlertDialogTitle>
          <AlertDialogDescription>该操作会立即修改审计与统计数据，请确认后执行。</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={onConfirm}>
            确认执行
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
