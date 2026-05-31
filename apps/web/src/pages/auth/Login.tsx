import { useEffect, useState, type FormEvent } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  Activity,
  ArrowRight,
  Loader2,
  Lock,
  Mail,
  ShieldCheck,
  Sparkles,
  Zap,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  Button,
  Card,
  Input,
  Label,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  cn,
} from '@relay-api/ui';
import type { Role } from '@relay-api/lib';
import { useAuth } from '@/stores/auth';
import { authApi } from '@/lib/api';
import { StatusDot } from '@/components/common/StatusDot';

const featurePills = [
  { icon: ShieldCheck, label: '组织级鉴权 · 权限隔离', tone: 'text-emerald-500' },
  { icon: Zap, label: '多源容灾 · SLA 监控', tone: 'text-amber-500' },
  { icon: Activity, label: '用量审计 · 成本治理', tone: 'text-sky-500' },
] as const;

const fakeProviders = [
  { name: 'OpenAI · GPT-4o', latency: '142ms', tone: 'online' as const },
  { name: 'Anthropic · Claude 3.5', latency: '178ms', tone: 'online' as const },
  { name: 'Google · Gemini 2.0', latency: '105ms', tone: 'online' as const },
  { name: 'xAI · Grok 2', latency: '320ms', tone: 'idle' as const },
];

export default function Page() {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const login = useAuth((s) => s.login);
  const register = useAuth((s) => s.register);
  const initialRole: Role = searchParams.get('role') === 'admin' ? 'admin' : 'user';
  const [loading, setLoading] = useState(false);
  const [settings, setSettings] = useState({
    platformName: 'Relay API',
    openRegistration: true,
    requireInviteCode: true,
    requireEmailVerification: false,
  });
  const [remember, setRemember] = useState(true);
  const [loginRole, setLoginRole] = useState<Role>(initialRole);
  const [loginEmail, setLoginEmail] = useState('');
  const [loginPwd, setLoginPwd] = useState('');
  const [regEmail, setRegEmail] = useState('');
  const [regPwd, setRegPwd] = useState('');
  const [regConfirm, setRegConfirm] = useState('');
  const [regInvite, setRegInvite] = useState(searchParams.get('code') ?? '');
  const [regEmailCode, setRegEmailCode] = useState('');
  const [sendingCode, setSendingCode] = useState(false);
  const [codeCooldown, setCodeCooldown] = useState(0);

  useEffect(() => {
    authApi
      .settings()
      .then((response) => {
        setSettings({
          platformName: response.data.platformName,
          openRegistration: response.data.openRegistration,
          requireInviteCode: response.data.requireInviteCode,
          requireEmailVerification: Boolean(response.data.requireEmailVerification),
        });
      })
      .catch(() => {
        setSettings((current) => ({ ...current, requireInviteCode: true }));
      });
  }, []);

  useEffect(() => {
    if (codeCooldown <= 0) return;
    const timer = window.setTimeout(() => setCodeCooldown((current) => Math.max(0, current - 1)), 1000);
    return () => window.clearTimeout(timer);
  }, [codeCooldown]);

  const submitLogin = async (e: FormEvent) => {
    e.preventDefault();
    if (!loginEmail) {
      toast.error('请输入邮箱');
      return;
    }
    if (!loginPwd) {
      toast.error('请输入密码');
      return;
    }
    setLoading(true);
    try {
      const user = await login(loginEmail, loginPwd, loginRole);
      toast.success(`欢迎回来, ${user.name || user.email}`);
      navigate(loginRole === 'admin' && user.role === 'admin' ? '/admin/dashboard' : '/user/dashboard');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  const sendEmailCode = async () => {
    if (!regEmail || !regEmail.includes('@')) {
      toast.error('请输入有效邮箱');
      return;
    }
    setSendingCode(true);
    try {
      const response = await authApi.sendRegisterEmailCode({ email: regEmail.trim() });
      setCodeCooldown(response.data.cooldownSeconds || 60);
      if (response.data.devCode) {
        setRegEmailCode(response.data.devCode);
        toast.success('验证码已生成', { description: `本地开发验证码：${response.data.devCode}` });
      } else {
        toast.success('验证码已发送', { description: response.data.email });
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '发送验证码失败');
    } finally {
      setSendingCode(false);
    }
  };

  const submitRegister = async (e: FormEvent) => {
    e.preventDefault();
    if (!regEmail || !regPwd) {
      toast.error('请填写邮箱和密码');
      return;
    }
    if (regPwd !== regConfirm) {
      toast.error('两次密码不一致');
      return;
    }
    if (settings.requireInviteCode && !regInvite.trim()) {
      toast.error('请输入邀请码');
      return;
    }
    if (settings.requireEmailVerification && !regEmailCode.trim()) {
      toast.error('请输入邮箱验证码');
      return;
    }
    if (!settings.openRegistration) {
      toast.error('当前未开放注册');
      return;
    }
    setLoading(true);
    try {
      await register({
        email: regEmail,
        password: regPwd,
        name: regEmail.split('@')[0],
        inviteCode: regInvite.trim(),
        emailCode: settings.requireEmailVerification ? regEmailCode.trim() : undefined,
      });
      toast.success('注册成功, 已自动登录');
      navigate('/user/dashboard');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '注册失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="relative flex min-h-screen w-full items-center justify-center overflow-hidden bg-background">
      {/* Fixed Background Layer */}
      <div className="mesh-gradient" />
      
      <div className="relative z-10 mx-auto grid w-full max-w-7xl items-center gap-10 px-6 py-10 lg:grid-cols-[1.1fr_1fr] lg:gap-20 lg:px-12 lg:py-16">
        {/* Left hero column */}
        <motion.aside
          initial={{ opacity: 0, x: -24 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.8, ease: [0.22, 1, 0.36, 1] }}
          className="hidden flex-col gap-12 lg:flex"
        >
          <div className="inline-flex w-fit items-center gap-2 rounded-full border border-primary/10 bg-background/50 px-4 py-1.5 text-[10px] font-bold text-primary backdrop-blur">
            <Zap className="h-3.5 w-3.5 fill-current" />
            <span className="uppercase tracking-[0.25em]">AI API 转发管理平台</span>
          </div>

          <div className="space-y-6">
            <h1 className="text-6xl font-bold leading-[1.02] tracking-tighter text-balance">
              AI API 统一
              <br />
              <span className="text-muted-foreground/40">接入治理网关</span>
            </h1>
            <p className="max-w-md text-base font-medium leading-relaxed text-muted-foreground/60">
              支持接入 OpenAI, Claude 与 Gemini 等主流模型。为团队提供简单的成员管理、配额分配与用量审计功能。
            </p>
          </div>

          <ul className="flex flex-col gap-3">
            {featurePills.map((pill, i) => (
              <motion.li
                key={pill.label}
                initial={{ opacity: 0, x: -12 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: 0.2 + i * 0.08, duration: 0.4, ease: [0.22, 1, 0.36, 1] }}
                className="inline-flex w-fit items-center gap-3 rounded-xl border border-border/40 bg-background/40 px-4 py-2 text-sm font-semibold backdrop-blur"
              >
                <pill.icon className={`h-4 w-4 ${pill.tone}`} />
                <span>{pill.label}</span>
              </motion.li>
            ))}
          </ul>

          {/* Faux dashboard preview */}
          <motion.div
            initial={{ opacity: 0, y: 24 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.5, duration: 0.8, ease: [0.22, 1, 0.36, 1] }}
            className="glass relative max-w-md rounded-[24px] p-6 shadow-2xl shadow-black/5"
          >
            <div className="absolute -inset-px -z-10 rounded-[24px] bg-primary/5 blur-3xl" />
            <div className="mb-6 flex items-center justify-between">
              <div>
                <div className="text-[10px] font-bold uppercase tracking-[0.25em] text-muted-foreground/60">        
                  控制中心
                </div>
                <div className="mt-1 text-sm font-bold">上游源实时状态</div>
              </div>
              <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1 text-[10px] font-bold text-emerald-600 uppercase tracking-wider">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" />
                运行健康
              </span>
            </div>
            <ul className="space-y-3">
              {fakeProviders.map((p) => (
                <li
                  key={p.name}
                  className="flex items-center justify-between rounded-xl border border-border/40 bg-background/40 px-4 py-2.5 text-xs font-semibold"
                >
                  <div className="flex items-center gap-3">
                    <StatusDot tone={p.tone} />
                    <span>{p.name}</span>
                  </div>
                  <span className="font-mono text-[10px] text-muted-foreground">{p.latency}</span>
                </li>
              ))}
            </ul>
          </motion.div>
        </motion.aside>

        {/* Right form column */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6, delay: 0.2, ease: [0.22, 1, 0.36, 1] }}
          className="mx-auto w-full max-w-md"
        >
          <Card className="overflow-hidden rounded-[24px] border-border/40 bg-card/40 shadow-2xl shadow-black/5 backdrop-blur-xl">
            <div className="bg-muted/30 p-8 pb-6">     
              <div className="text-[10px] font-bold uppercase tracking-[0.25em] text-muted-foreground/60">
                身份验证门户
              </div>
              <div className="mt-1 text-2xl font-bold tracking-tight">欢迎回来</div>
              <p className="mt-1.5 text-sm font-medium text-muted-foreground/60">
                登录您的企业工作台 · {settings.platformName}
              </p>
            </div>

            <div className="p-8 pt-6">
              <Tabs defaultValue={location.pathname === '/register' ? 'register' : 'login'} className="w-full">   
                <TabsList className="grid h-12 w-full grid-cols-2 rounded-xl bg-muted/50 p-1.5">
                  <TabsTrigger value="login" className="rounded-lg">登录</TabsTrigger>
                  <TabsTrigger value="register" className="rounded-lg">注册</TabsTrigger>
                </TabsList>

                <TabsContent value="login" className="mt-8 space-y-5">
                  <form onSubmit={submitLogin} className="space-y-5">
                    <div className="space-y-2.5">
                      <Label htmlFor="login-email" className="font-bold">邮箱地址</Label>
                      <div className="relative">
                        <Mail className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
                        <Input
                          id="login-email"
                          type="email"
                          autoComplete="email"
                          value={loginEmail}
                          onChange={(e) => setLoginEmail(e.target.value)}
                          placeholder="you@company.com"
                          className="h-11 rounded-xl pl-10"
                        />
                      </div>
                    </div>
                    
                    <div className="space-y-2.5">
                      <Label className="font-bold">登录入口</Label>
                      <div className="grid grid-cols-2 gap-2 rounded-xl border border-border/40 bg-muted/40 p-1.5 shadow-[inset_0_1px_2px_rgba(0,0,0,0.05)]">
                        <Button
                          type="button"
                          variant={loginRole === 'admin' ? 'secondary' : 'ghost'}
                          onClick={() => setLoginRole('admin')}
                          className={cn(
                            'h-9 rounded-lg font-bold transition-all duration-300',
                            loginRole === 'admin' 
                              ? 'bg-background text-primary shadow-sm ring-1 ring-border' 
                              : 'text-muted-foreground/60 hover:text-foreground'
                          )}
                        >
                          <ShieldCheck className="h-4 w-4" />
                          管理端
                        </Button>
                        <Button
                          type="button"
                          variant={loginRole === 'user' ? 'secondary' : 'ghost'}
                          onClick={() => setLoginRole('user')}
                          className={cn(
                            'h-9 rounded-lg font-bold transition-all duration-300',
                            loginRole === 'user' 
                              ? 'bg-background text-primary shadow-sm ring-1 ring-border' 
                              : 'text-muted-foreground/60 hover:text-foreground'
                          )}
                        >
                          <Activity className="h-4 w-4" />
                          用户端
                        </Button>
                      </div>
                    </div>

                    <div className="space-y-2.5">
                      <Label htmlFor="login-password" className="font-bold">密码</Label>
                      <div className="relative">
                        <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
                        <Input
                          id="login-password"
                          type="password"
                          autoComplete="current-password"
                          value={loginPwd}
                          onChange={(e) => setLoginPwd(e.target.value)}
                          placeholder="••••••••"
                          className="h-11 rounded-xl pl-10"
                        />
                      </div>
                    </div>

                    <div className="flex items-center justify-between px-1">
                      <label className="flex cursor-pointer items-center gap-2.5 text-sm font-semibold text-muted-foreground/60">    
                        <Switch checked={remember} onCheckedChange={setRemember} />
                        <span>记住我</span>
                      </label>
                    </div>

                    <Button
                      type="submit"
                      size="lg"
                      className="h-12 w-full rounded-xl font-bold shadow-xl shadow-primary/10 active:scale-[0.97]"
                      disabled={loading}
                    >
                      {loading ? (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      ) : (
                        <ArrowRight className="mr-2 h-4 w-4" />
                      )}
                      登录工作台
                    </Button>
                  </form>

                </TabsContent>

                <TabsContent value="register" className="mt-8 space-y-5">
                  <form onSubmit={submitRegister} className="space-y-5">
                    <div className="space-y-2.5">
                      <Label htmlFor="reg-email" className="font-bold">邮箱地址</Label>
                      <Input
                        id="reg-email"
                        type="email"
                        autoComplete="email"
                        value={regEmail}
                        onChange={(e) => setRegEmail(e.target.value)}
                        placeholder="you@company.com"
                        className="h-11 rounded-xl"
                      />
                    </div>
                    {settings.requireEmailVerification && (
                      <>
                        <div className="space-y-2.5">
                          <Label htmlFor="reg-email-code-action" className="font-bold">邮箱验证</Label>
                          <Button
                            id="reg-email-code-action"
                            type="button"
                            variant="outline"
                            className="h-11 w-full rounded-xl px-4 font-bold"
                            disabled={sendingCode || codeCooldown > 0}
                            onClick={sendEmailCode}
                          >
                            {sendingCode ? (
                              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                            ) : (
                              <Mail className="mr-2 h-4 w-4" />
                            )}
                            {codeCooldown > 0 ? `${codeCooldown}s 后重试` : '发送邮箱验证码'}
                          </Button>
                        </div>
                        <div className="space-y-2.5">
                          <Label htmlFor="reg-email-code" className="font-bold">邮箱验证码</Label>
                          <Input
                            id="reg-email-code"
                            inputMode="numeric"
                            maxLength={6}
                            value={regEmailCode}
                            onChange={(e) => setRegEmailCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                            placeholder="输入 6 位验证码"
                            className="h-11 rounded-xl font-mono tracking-[0.3em]"
                          />
                        </div>
                      </>
                    )}
                    <div className="grid grid-cols-2 gap-4">
                      <div className="space-y-2.5">
                        <Label htmlFor="reg-password" className="font-bold">设置密码</Label>
                        <Input
                          id="reg-password"
                          type="password"
                          value={regPwd}
                          onChange={(e) => setRegPwd(e.target.value)}
                          className="h-11 rounded-xl"
                        />
                      </div>
                      <div className="space-y-2.5">
                        <Label htmlFor="reg-confirm" className="font-bold">确认密码</Label>
                        <Input
                          id="reg-confirm"
                          type="password"
                          value={regConfirm}
                          onChange={(e) => setRegConfirm(e.target.value)}
                          className="h-11 rounded-xl"
                        />
                      </div>
                    </div>
                    <div className="space-y-2.5">
                      <Label htmlFor="reg-invite" className="font-bold">邀请码</Label>
                      <Input
                        id="reg-invite"
                        required
                        value={regInvite}
                        onChange={(e) => setRegInvite(e.target.value)}
                        placeholder="输入企业邀请码"
                        className="h-11 rounded-xl"
                      />
                    </div>
                    <Button
                      type="submit"
                      size="lg"
                      className="h-12 w-full rounded-xl font-bold shadow-xl shadow-primary/10 active:scale-[0.97]"
                      disabled={loading}
                    >
                      {loading ? (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      ) : (
                        <Sparkles className="mr-2 h-4 w-4" />
                      )}
                      创建账户
                    </Button>
                  </form>
                </TabsContent>
              </Tabs>
            </div>
          </Card>
        </motion.div>
      </div>
    </div>
  );
}
