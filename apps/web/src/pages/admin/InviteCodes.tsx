import { useMemo, useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { toast } from 'sonner';
import {
  Button,
  Card,
  CardContent,
  Input,
  Label,
  Badge,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  cn,
} from '@relay-api/ui';
import {
  Search,
  Plus,
  Copy,
  Trash2,
  Ticket,
  CheckCircle2,
  CalendarDays,
  AlertTriangle,
  Sparkles,
  X,
  Link as LinkIcon,
} from 'lucide-react';
import {
  copyToClipboard,
  formatDateTime,
  formatRelative,
} from '@relay-api/lib';
import type { InviteCode, InviteStatus } from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { EmptyState } from '@/components/common/EmptyState';

const statusToTone: Record<InviteStatus, 'success' | 'neutral' | 'warning'> = {
  valid: 'success',
  expired: 'neutral',
  exhausted: 'warning',
};

const statusLabel: Record<InviteStatus, string> = {
  valid: '有效',
  expired: '已过期',
  exhausted: '已耗尽',
};

type Validity = '7d' | '30d' | '90d' | 'never';
const validityToMs: Record<Validity, number | null> = {
  '7d': 7 * 24 * 3600 * 1000,
  '30d': 30 * 24 * 3600 * 1000,
  '90d': 90 * 24 * 3600 * 1000,
  never: null,
};

export default function Page() {
  const [invites, setInvites] = useState<InviteCode[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const [lastCreated, setLastCreated] = useState<{ code: string; url: string } | null>(null);

  // Form
  const [customCode, setCustomCode] = useState('');
  const [validity, setValidity] = useState<Validity>('30d');
  const [limit, setLimit] = useState('10');
  const [remark, setRemark] = useState('');

  // Auto-dismiss the toast banner
  useEffect(() => {
    adminApi
      .invites()
      .then((response) => setInvites(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载邀请码失败')))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!lastCreated) return;
    const t = setTimeout(() => setLastCreated(null), 12000);
    return () => clearTimeout(t);
  }, [lastCreated]);

  const filtered = useMemo(() => {
    if (!search) return invites;
    const q = search.toLowerCase();
    return invites.filter((i) => i.code.toLowerCase().includes(q) || i.remark.toLowerCase().includes(q));
  }, [invites, search]);

  const stats = useMemo(() => {
    const now = Date.now();
    const sevenDays = 7 * 24 * 3600 * 1000;
    const valid = invites.filter((i) => i.status === 'valid').length;
    const used = invites.reduce((s, i) => s + i.usedCount, 0);
    const expired = invites.filter((i) => i.status === 'expired').length;
    const soon = invites.filter((i) => {
      if (i.status !== 'valid') return false;
      const exp = new Date(i.expiresAt).getTime();
      return exp - now > 0 && exp - now <= sevenDays;
    }).length;
    return { valid, used, expired, soon };
  }, [invites]);

  const handleCreate = async () => {
    const ms = validityToMs[validity];
    const expiresAt = ms === null ? undefined : new Date(Date.now() + ms).toISOString();
    const limitN = Math.max(1, Number(limit) || 10);
    const code = customCode.trim().toUpperCase();
    if (code && invites.some((invite) => invite.code.toUpperCase() === code)) {
      toast.error('邀请码已存在');
      return;
    }
    try {
      const response = await adminApi.createInvite({
        code: code || undefined,
        limit: limitN,
        expiresAt,
        remark: remark.trim() || '未备注',
      });
      setInvites((prev) => [response.data, ...prev]);
      const url = `${window.location.origin}${response.link ?? `/register?code=${response.data.code}`}`;
      setLastCreated({ code: response.data.code, url });
      toast.success('邀请码已生成', { description: response.data.code });
      setOpen(false);
      setCustomCode('');
      setRemark('');
    } catch (error) {
      toast.error(getErrorMessage(error, '创建邀请码失败'));
    }
  };

  const removeInvite = async (id: string) => {
    try {
      await adminApi.deleteInvite(id);
      setInvites((prev) => prev.filter((i) => i.id !== id));
      toast.success('邀请码已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除邀请码失败'));
    }
  };

  const copyCode = async (code: string) => {
    await copyToClipboard(code);
    toast.success('已复制', { description: code });
  };

  const copyLink = async (code: string) => {
    const url = `${window.location.origin}/register?code=${code}`;
    await copyToClipboard(url);
    toast.success('邀请链接已复制', { description: url });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="增长策略"
        title="邀请码管理"
        description="生成定向邀请码，控制新用户注册的来源、配额与有效期。"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索邀请码 / 备注..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 w-64 pl-9"
              />
            </div>
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button className="shadow-sm">
                  <Plus className="mr-2 h-4 w-4" />
                  生成邀请码
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>生成新的邀请码</DialogTitle>
                  <DialogDescription>可手动指定邀请码；留空时系统自动生成。</DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                  <div className="grid gap-2">
                    <Label htmlFor="invite-code">自定义邀请码 (可选)</Label>
                    <Input
                      id="invite-code"
                      value={customCode}
                      onChange={(e) => setCustomCode(e.target.value.toUpperCase())}
                      placeholder="例如：TEAM-DEV-2026"
                      className="font-mono"
                    />
                    <p className="text-xs text-muted-foreground">
                      支持字母、数字、连字符和下划线，不能与已有邀请码重复。
                    </p>
                  </div>
                  <div className="grid gap-2">
                    <Label>有效期</Label>
                    <Select value={validity} onValueChange={(v) => setValidity(v as Validity)}>
                      <SelectTrigger><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="7d">1 周</SelectItem>
                        <SelectItem value="30d">1 个月</SelectItem>
                        <SelectItem value="90d">3 个月</SelectItem>
                        <SelectItem value="never">永久</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="limit">使用次数上限</Label>
                    <Input id="limit" type="number" min={1} value={limit} onChange={(e) => setLimit(e.target.value)} />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="remark">备注 (可选)</Label>
                    <Input id="remark" placeholder="例如：5月推广活动" value={remark} onChange={(e) => setRemark(e.target.value)} />
                  </div>
                </div>
                <DialogFooter>
                  <DialogClose asChild>
                    <Button variant="ghost">取消</Button>
                  </DialogClose>
                  <Button onClick={handleCreate}>生成</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
        }
      />

      {/* Banner: last created */}
      <AnimatePresence>
        {lastCreated && (
          <motion.div
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
          >
            <Card className="relative overflow-hidden border-primary/30 bg-gradient-to-br from-primary/10 via-background to-background">
              <CardContent className="flex flex-wrap items-center gap-4 p-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/15 text-primary">
                  <CheckCircle2 className="h-5 w-5" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-semibold">邀请码已就绪 · 可立即分发</div>
                  <div className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{lastCreated.url}</div>
                </div>
                <Button size="sm" onClick={() => copyLink(lastCreated.code)}>
                  <LinkIcon className="mr-1.5 h-3.5 w-3.5" />
                  复制邀请链接
                </Button>
                <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => setLastCreated(null)}>
                  <X className="h-4 w-4" />
                </Button>
              </CardContent>
            </Card>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Stat cards */}
      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="有效邀请码" value={stats.valid} icon={Ticket} tone="primary" delay={0.02} hint="可被新用户兑换" />
        <StatCard label="累计使用次数" value={stats.used} icon={CheckCircle2} tone="success" delay={0.06} hint="历史已消耗名额" />
        <StatCard label="已过期" value={stats.expired} icon={CalendarDays} tone="neutral" delay={0.1} hint="超出有效期" />
        <StatCard label="即将到期" value={stats.soon} icon={AlertTriangle} tone="warning" delay={0.14} hint="7 天内即将失效" />
      </div>

      {/* Table */}
      {loading ? (
        <div className="rounded-xl border bg-card p-10 text-center text-sm text-muted-foreground">正在加载邀请码...</div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Sparkles}
          title="还没有邀请码"
          description="生成你的第一个邀请码，开始定向邀请新用户。"
          action={
            <Button size="sm" onClick={() => setOpen(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />生成邀请码
            </Button>
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead>邀请码</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>使用次数</TableHead>
                <TableHead>过期时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-32 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((iv, i) => {
                const pct = Math.min(100, (iv.usedCount / Math.max(1, iv.limit)) * 100);
                return (
                  <motion.tr
                    key={iv.id}
                    initial={{ opacity: 0, y: 6 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: i * 0.03, ease: [0.22, 1, 0.36, 1] }}
                    className={cn(
                      'group border-b transition-colors hover:bg-muted/40',
                      iv.status !== 'valid' && 'opacity-70',
                    )}
                  >
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className="border-primary/20 bg-primary/5 px-2 py-1 font-mono text-xs">
                          {iv.code}
                        </Badge>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 opacity-0 group-hover:opacity-100"
                          onClick={() => copyCode(iv.code)}
                        >
                          <Copy className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">{iv.remark}</TableCell>
                    <TableCell>
                      <div className="flex w-40 items-center gap-2">
                        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                          <div
                            className={cn(
                              'h-full rounded-full transition-all',
                              pct >= 100 ? 'bg-amber-500' : 'bg-primary',
                            )}
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                        <span className="font-mono text-xs tabular-nums text-muted-foreground">
                          {iv.usedCount}/{iv.limit}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="text-sm">{formatDateTime(iv.expiresAt)}</span>
                        <span className="text-[11px] text-muted-foreground">{formatRelative(iv.expiresAt)}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge tone={statusToTone[iv.status]} label={statusLabel[iv.status]} />
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => copyLink(iv.code)} title="复制邀请链接">
                          <LinkIcon className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-destructive hover:text-destructive"
                          onClick={() => removeInvite(iv.id)}
                          title="删除"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </motion.tr>
                );
              })}
            </TableBody>
          </Table>
        </Card>
      )}
    </div>
  );
}
