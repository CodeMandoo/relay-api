import { useMemo, useState, useEffect } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Plus,
  Power,
  Search,
  ShieldCheck,
  Trash2,
  WalletCards,
  Loader2,
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  Label,
  Progress,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  cn,
} from '@relay-api/ui';
import {
  copyToClipboard,
  formatCurrency,
  formatRelative,
  type ApiKey,
} from '@relay-api/lib';
import { getErrorMessage, userApi } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { EmptyState } from '@/components/common/EmptyState';

export default function Page() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [visible, setVisible] = useState<Set<string>>(new Set());
  const [name, setName] = useState('');
  const [limit, setLimit] = useState('');

  useEffect(() => {
    userApi
      .apiKeys()
      .then((response) => setKeys(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载 API Key 失败')));
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return keys;
    return keys.filter((key) => key.name.toLowerCase().includes(q) || key.masked.toLowerCase().includes(q));
  }, [keys, query]);

  const stats = useMemo(() => {
    const enabled = keys.filter((key) => key.status === 'valid').length;
    const totalSpent = keys.reduce((sum, key) => sum + key.spent, 0);
    const limited = keys.filter((key) => typeof key.limit === 'number').length;
    return { enabled, totalSpent, limited };
  }, [keys]);

  const createKey = async () => {
    if (!name.trim()) {
      toast.error('请填写 Key 名称');
      return;
    }
    setCreating(true);
    try {
      const response = await userApi.createApiKey({
        name: name.trim(),
        limit: limit ? Number(limit) : undefined,
      });
      setKeys((prev) => [response.data, ...prev]);
      setVisible((prev) => new Set(prev).add(response.data.id));
      setName('');
      setLimit('');
      setOpen(false);
      toast.success('API Key 已创建', { description: '凭证已安全保存至您的账户。' });
    } catch (error) {
      toast.error(getErrorMessage(error, '创建 API Key 失败'));
    } finally {
      setCreating(false);
    }
  };

  const copyKey = async (key: ApiKey) => {
    try {
      const secret = visible.has(key.id) ? key.key : (await userApi.revealApiKey(key.id)).data.key;
      await copyToClipboard(secret);
      setKeys((prev) => prev.map((item) => (item.id === key.id ? { ...item, key: secret } : item)));
      toast.success('API Key 已复制');
    } catch (error) {
      toast.error(getErrorMessage(error, '复制 API Key 失败'));
    }
  };

  const toggleVisible = async (key: ApiKey) => {
    if (visible.has(key.id)) {
      setVisible((prev) => {
        const next = new Set(prev);
        next.delete(key.id);
        return next;
      });
      return;
    }
    try {
      const response = await userApi.revealApiKey(key.id);
      setKeys((prev) => prev.map((item) => (item.id === key.id ? response.data : item)));
      setVisible((prev) => new Set(prev).add(key.id));
    } catch (error) {
      toast.error(getErrorMessage(error, '显示 API Key 失败'));
    }
  };

  const toggleStatus = async (id: string) => {
    const target = keys.find((key) => key.id === id);
    if (!target) return;
    try {
      const response = await userApi.updateApiKey(id, { enabled: target.status !== 'valid' });
      setKeys((prev) => prev.map((key) => (key.id === id ? response.data : key)));
      toast.success('Key 状态已更新');
    } catch (error) {
      toast.error(getErrorMessage(error, '更新 Key 状态失败'));
    }
  };

  const removeKey = async (id: string) => {
    try {
      await userApi.deleteApiKey(id);
      setKeys((prev) => prev.filter((key) => key.id !== id));
      toast.success('API Key 已永久删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除 API Key 失败'));
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="开发凭证"
        title="API Key 管理"
        description="创建、复制、禁用或回收用于 OpenAI 兼容接口的 sk-xxx 凭证。"
        actions={
          <>
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground/60" />
              <Input
                className="h-9 w-64 pl-9 rounded-lg"
                placeholder="搜索 Key 名称..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button className="shadow-sm font-bold">
                  <Plus className="mr-2 h-4 w-4" />
                  创建新的 API Key
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>创建 API Key</DialogTitle>
                  <DialogDescription>为不同应用创建独立 Key，便于分流管理与限额管控。</DialogDescription>
                </DialogHeader>
                <div className="grid gap-4 py-2">
                  <div className="grid gap-2">
                    <Label htmlFor="key-name">Key 名称</Label>
                    <Input id="key-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. production-backend" />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="key-limit">月度预算上限 (USD，可选)</Label>
                    <Input id="key-limit" type="number" min={0} value={limit} onChange={(e) => setLimit(e.target.value)} placeholder="不设上限" />
                  </div>
                </div>
                <DialogFooter>
                  <Button variant="ghost" onClick={() => setOpen(false)}>
                    取消
                  </Button>
                  <Button onClick={createKey} disabled={creating}>
                    {creating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                    确认创建
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        }
      />

      <div className="grid gap-6 md:grid-cols-3">
        <StatCard label="有效 Key" value={stats.enabled} icon={KeyRound} tone="primary" delay={0} hint="当前活跃凭证" />
        <StatCard label="本月消耗" value={formatCurrency(stats.totalSpent)} icon={WalletCards} tone="warning" delay={0.05} hint="全部凭证汇总" />
        <StatCard label="限额管控" value={stats.limited} icon={ShieldCheck} tone="success" delay={0.1} hint="已设置预算限制" />
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title="没有 API Key"
          description="创建一个 Key 后即可在应用中调用 /v1/chat/completions 或 /v1/responses。"
          action={<Button onClick={() => setOpen(true)}>创建 API Key</Button>}
        />
      ) : (
        <TooltipProvider delayDuration={150}>
          <Card className="overflow-hidden border-border/40 shadow-sm">
            <Table className="w-full table-fixed">
              <TableHeader>
                <TableRow className="bg-muted/40 hover:bg-muted/40">
                  <TableHead className="w-[18%] px-6">名称</TableHead>
                  <TableHead className="w-[33%] px-3">Key 值 (SECRET)</TableHead>
                  <TableHead className="w-[10%] px-3">状态</TableHead>
                  <TableHead className="w-[15%] px-3">预算消耗</TableHead>
                  <TableHead className="w-[12%] px-3 text-right">最后使用</TableHead>
                  <TableHead className="w-[12%] px-3 pr-6 text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((key, index) => {
                  const isVisible = visible.has(key.id);
                  const displayKey = isVisible ? key.key : key.masked;
                  const hasLimit = typeof key.limit === 'number';
                  const pct = hasLimit ? Math.min(100, Math.round((key.spent / Math.max(key.limit ?? 1, 1)) * 100)) : 0;
                  return (
                    <motion.tr
                      key={key.id}
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      transition={{ delay: index * 0.02 }}
                      className={cn('border-b border-border/40 transition-colors hover:bg-muted/40', key.status === 'disabled' && 'opacity-60 grayscale-[0.35]')}
                    >
                      <TableCell className="px-6">
                        <div className="flex min-w-0 items-center gap-3">
                          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                            <KeyRound className="h-4 w-4" />
                          </span>
                          <div className="min-w-0">
                            <div className="truncate text-sm font-bold">{key.name}</div>
                            <div className="truncate text-[10px] text-muted-foreground uppercase font-bold tracking-wider">{formatRelative(key.createdAt)}</div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="px-3">
                        <div className="flex min-w-0 items-center gap-1.5">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <code className="block h-8 min-w-0 flex-1 select-all truncate rounded-lg border bg-muted/45 px-2.5 py-2 font-mono text-[11px] leading-none text-foreground/80">
                                {displayKey}
                              </code>
                            </TooltipTrigger>
                            <TooltipContent side="top" align="start" className="max-w-md break-all font-mono text-[10px]">
                              {displayKey}
                            </TooltipContent>
                          </Tooltip>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 shrink-0 text-muted-foreground hover:text-foreground"
                            onClick={() => toggleVisible(key)}
                          >
                            {isVisible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                          </Button>
                          <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0 text-muted-foreground hover:text-primary" onClick={() => copyKey(key)}>
                            <Copy className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                      <TableCell className="px-3">
                        <StatusBadge tone={key.status === 'valid' ? 'success' : 'neutral'} label={key.status === 'valid' ? '启用' : '禁用'} className="font-bold text-[10px]" />
                      </TableCell>
                      <TableCell className="px-3">
                        {hasLimit ? (
                          <div className="space-y-1.5 min-w-[100px]">
                            <div className="flex justify-between text-[10px] font-bold">
                              <span>{formatCurrency(key.spent)}</span>
                              <span className="text-muted-foreground/60">{formatCurrency(key.limit ?? 0)}</span>
                            </div>
                            <Progress value={pct} className={cn('h-1', pct > 85 && '[&>div]:bg-destructive')} />
                          </div>
                        ) : (
                          <span className="text-xs font-bold text-muted-foreground/40 uppercase tracking-widest">Unlimited</span>
                        )}
                      </TableCell>
                      <TableCell className="px-3 text-right text-[11px] font-medium text-muted-foreground">
                        {key.lastUsedAt ? formatRelative(key.lastUsedAt) : '从未使用'}
                      </TableCell>
                      <TableCell className="px-3 pr-6 text-right">
                        <div className="flex min-w-0 items-center justify-end gap-1">
                          <AlertDialog>
                            <AlertDialogTrigger asChild>
                              <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-destructive transition-colors">
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </AlertDialogTrigger>
                            <AlertDialogContent>
                              <AlertDialogHeader>
                                <AlertDialogTitle>永久删除 API Key?</AlertDialogTitle>
                                <AlertDialogDescription>
                                  删除后使用该凭证的应用将立即认证失败。该操作无法撤销。
                                </AlertDialogDescription>
                              </AlertDialogHeader>
                              <AlertDialogFooter>
                                <AlertDialogCancel>取消</AlertDialogCancel>
                                <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => removeKey(key.id)}>
                                  确认删除
                                </AlertDialogAction>
                              </AlertDialogFooter>
                            </AlertDialogContent>
                          </AlertDialog>
                          <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-primary" onClick={() => toggleStatus(key.id)}>
                            <Power className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </motion.tr>
                  );
                })}
              </TableBody>
            </Table>
            <CardContent className="border-t bg-muted/20 py-3 text-[10px] font-bold text-muted-foreground/60 uppercase tracking-widest">
              Security Notice: Never share your secret keys in public or client-side code.
            </CardContent>
          </Card>
        </TooltipProvider>
      )}
    </div>
  );
}
