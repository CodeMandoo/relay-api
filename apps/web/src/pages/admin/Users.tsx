import { useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Edit,
  MoreHorizontal,
  Plus,
  Power,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users as UsersIcon,
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
  Avatar,
  AvatarFallback,
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Input,
  Label,
  Progress,
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
import {
  formatNumberFull,
  formatRelative,
  type PlatformUser,
  type Role,
  type UserStatus,
} from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { StatusBadge } from '@/components/common/StatusBadge';
import { EmptyState } from '@/components/common/EmptyState';

const initials = (name: string): string => {
  const parts = name.trim().split(/\s+/);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + (parts[1][0] ?? '')).toUpperCase();
};

const roleLabel = (r: Role): string => (r === 'admin' ? '管理员' : '普通用户');

export default function Page() {
  const [users, setUsers] = useState<PlatformUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const [roleFilter, setRoleFilter] = useState<'all' | Role>('all');
  const [statusFilter, setStatusFilter] = useState<'all' | UserStatus>('all');
  const [addOpen, setAddOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<PlatformUser | null>(null);

  const [newEmail, setNewEmail] = useState('');
  const [newBalance, setNewBalance] = useState('500');
  const [newRole, setNewRole] = useState<Role>('user');
  const [newQuota, setNewQuota] = useState('1000');
  const [newPassword, setNewPassword] = useState('user123456');
  const [editEmail, setEditEmail] = useState('');
  const [editName, setEditName] = useState('');
  const [editRole, setEditRole] = useState<Role>('user');
  const [editStatus, setEditStatus] = useState<UserStatus>('normal');
  const [editQuota, setEditQuota] = useState('0');
  const [editBalance, setEditBalance] = useState('0');
  const [editPassword, setEditPassword] = useState('');

  const reloadUsers = () => {
    setLoading(true);
    adminApi
      .users()
      .then((response) => setUsers(response.data))
      .catch((error) => toast.error(getErrorMessage(error, '加载用户失败')))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    reloadUsers();
  }, []);

  const stats = useMemo(() => {
    return {
      total: users.length,
      monthlyNew: users.filter((user) => {
        const registeredAt = new Date(user.registeredAt);
        const now = new Date();
        return registeredAt.getFullYear() === now.getFullYear() && registeredAt.getMonth() === now.getMonth();
      }).length,
      disabled: users.filter((u) => u.status === 'disabled').length,
    };
  }, [users]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return users.filter((u) => {
      const inviteCode = u.inviteCode?.toLowerCase() ?? '';
      if (q && !u.email.toLowerCase().includes(q) && !u.name.toLowerCase().includes(q) && !inviteCode.includes(q)) {
        return false;
      }
      if (roleFilter !== 'all' && u.role !== roleFilter) return false;
      if (statusFilter !== 'all' && u.status !== statusFilter) return false;
      return true;
    });
  }, [users, query, roleFilter, statusFilter]);

  const toggleStatus = async (id: string) => {
    const target = users.find((u) => u.id === id);
    if (!target) return;
    const status = target.status === 'normal' ? 'disabled' : 'normal';
    try {
      const response = await adminApi.updateUser(id, { status });
      setUsers((prev) => prev.map((u) => (u.id === id ? response.data : u)));
      toast.success(status === 'disabled' ? `已禁用 ${target.email}` : `已启用 ${target.email}`);
    } catch (error) {
      toast.error(getErrorMessage(error, '更新用户状态失败'));
    }
  };

  const resetQuota = async (id: string) => {
    const target = users.find((u) => u.id === id);
    if (!target) return;
    try {
      await adminApi.updateUserQuota(id, {
        monthlyQuota: target.monthlyQuota,
        weeklyQuota: target.weeklyQuota,
        balance: target.balance,
      });
      toast.success('配额配置已同步');
    } catch (error) {
      toast.error(getErrorMessage(error, '同步配额失败'));
    }
  };

  const removeUser = async (id: string) => {
    try {
      await adminApi.deleteUser(id);
      setUsers((prev) => prev.map((u) => (u.id === id ? { ...u, status: 'disabled' } : u)));
      toast.success('用户已禁用');
    } catch (error) {
      toast.error(getErrorMessage(error, '禁用用户失败'));
    }
  };

  const handleEditUser = (user: PlatformUser) => {
    setEditingUser(user);
    setEditEmail(user.email);
    setEditName(user.name);
    setEditRole(user.role);
    setEditStatus(user.status);
    setEditQuota(String(user.monthlyQuota));
    setEditBalance(String(user.balance));
    setEditPassword('');
    setEditOpen(true);
  };

  const saveEditUser = async () => {
    if (!editingUser) return;
    if (!editEmail.trim() || !editEmail.includes('@')) {
      toast.error('请填写合法的邮箱地址');
      return;
    }
    if (editPassword && editPassword.trim().length < 8) {
      toast.error('新密码至少 8 位');
      return;
    }
    try {
      const response = await adminApi.updateUser(editingUser.id, {
        email: editEmail.trim(),
        name: editName.trim() || editEmail.split('@')[0],
        role: editRole,
        status: editStatus,
        monthlyQuota: Math.max(0, Number(editQuota) || 0),
        weeklyQuota: Math.round(Math.max(0, Number(editQuota) || 0) / 4),
        balance: Math.max(0, Number(editBalance) || 0),
        ...(editPassword.trim() ? { password: editPassword.trim() } : {}),
      });
      setUsers((prev) => prev.map((user) => (user.id === editingUser.id ? response.data : user)));
      setEditOpen(false);
      setEditingUser(null);
      toast.success('用户资料已更新');
    } catch (error) {
      toast.error(getErrorMessage(error, '更新用户失败'));
    }
  };

  const addUser = async () => {
    if (!newEmail.trim() || !newEmail.includes('@')) {
      toast.error('请填写合法的邮箱地址');
      return;
    }
    if (newPassword.trim().length < 8) {
      toast.error('密码至少 8 位');
      return;
    }
    const quota = Math.max(0, Number(newQuota) || 0);
    const balance = Math.max(0, Number(newBalance) || 0);
    try {
      const response = await adminApi.createUser({
        email: newEmail.trim(),
        name: newEmail.split('@')[0],
        password: newPassword.trim(),
        role: newRole,
        status: 'normal',
        monthlyQuota: quota,
        weeklyQuota: Math.round(quota / 4),
        balance,
      });
      setUsers((prev) => [response.data, ...prev]);
      toast.success(`已添加用户 ${response.data.email}`);
      setAddOpen(false);
      setNewEmail('');
      setNewBalance('500');
      setNewRole('user');
      setNewQuota('1000');
      setNewPassword('user123456');
    } catch (error) {
      toast.error(getErrorMessage(error, '创建用户失败'));
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="成员中心"
        title="用户管理"
        description="查看、邀请并管理平台用户 — 控制配额、调整角色、保持团队井然有序。"
        actions={
          <Dialog open={addOpen} onOpenChange={setAddOpen}>
            <DialogTrigger asChild>
              <Button className="shadow-sm">
                <Plus className="mr-1.5 h-4 w-4" />
                添加用户
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle className="flex items-center gap-2">
                  <UserPlus className="h-4 w-4 text-primary" />
                  添加新用户
                </DialogTitle>
                <DialogDescription>用户将通过邮箱激活并接收邀请。</DialogDescription>
              </DialogHeader>
              <div className="grid gap-4 py-2">
                <div className="space-y-1.5">
                  <Label htmlFor="email">邮箱地址</Label>
                  <Input
                    id="email"
                    placeholder="user@example.com"
                    value={newEmail}
                    onChange={(e) => setNewEmail(e.target.value)}
                  />
                </div>
                <div className="grid gap-6 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="balance">初始余额 (USD)</Label>
                    <Input
                      id="balance"
                      type="number"
                      min={0}
                      value={newBalance}
                      onChange={(e) => setNewBalance(e.target.value)}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="quota">月配额</Label>
                    <Input
                      id="quota"
                      type="number"
                      min={0}
                      value={newQuota}
                      onChange={(e) => setNewQuota(e.target.value)}
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="password">初始密码</Label>
                  <Input
                    id="password"
                    type="password"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>角色</Label>
                  <Select value={newRole} onValueChange={(v) => setNewRole(v as Role)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="user">普通用户</SelectItem>
                      <SelectItem value="admin">管理员</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter>
                <Button variant="ghost" onClick={() => setAddOpen(false)}>
                  取消
                </Button>
                <Button onClick={addUser}>创建用户</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑用户资料</DialogTitle>
            <DialogDescription>更新用户身份、状态、额度与余额。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-email">邮箱地址</Label>
                <Input id="edit-user-email" value={editEmail} onChange={(e) => setEditEmail(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-name">姓名</Label>
                <Input id="edit-user-name" value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>角色</Label>
                <Select value={editRole} onValueChange={(v) => setEditRole(v as Role)}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="user">普通用户</SelectItem>
                    <SelectItem value="admin">管理员</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>状态</Label>
                <Select value={editStatus} onValueChange={(v) => setEditStatus(v as UserStatus)}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="normal">正常</SelectItem>
                    <SelectItem value="disabled">已禁用</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-quota">月配额</Label>
                <Input id="edit-user-quota" type="number" min={0} value={editQuota} onChange={(e) => setEditQuota(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-balance">余额 (USD)</Label>
                <Input id="edit-user-balance" type="number" min={0} value={editBalance} onChange={(e) => setEditBalance(e.target.value)} />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="edit-user-password">新密码</Label>
              <Input id="edit-user-password" type="password" value={editPassword} onChange={(e) => setEditPassword(e.target.value)} placeholder="留空则不修改" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setEditOpen(false)}>取消</Button>
            <Button onClick={saveEditUser}>保存更改</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="grid gap-6 md:grid-cols-3">
        <StatCard label="总用户数" value={stats.total} hint="覆盖所有团队成员" icon={UsersIcon} tone="primary" delay={0} />
        <StatCard
          label="本月新增"
          value={`+${stats.monthlyNew}`}
          hint={<span className="text-emerald-500 font-medium">较上月增长 32%</span>}
          icon={UserPlus}
          tone="success"
          delay={0.05}
        />
        <StatCard
          label="已禁用"
          value={stats.disabled}
          hint="违规或闲置账号"
          icon={ShieldCheck}
          tone={stats.disabled > 0 ? 'destructive' : 'neutral'}
          delay={0.1}
        />
      </div>

      <Card className="overflow-hidden">
        <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full sm:max-w-xs">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="搜索邮箱、姓名或邀请码..."
              className="pl-9"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <div className="flex items-center gap-2">
            <Select value={roleFilter} onValueChange={(v) => setRoleFilter(v as 'all' | Role)}>
              <SelectTrigger className="h-9 w-36">
                <SelectValue placeholder="角色" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部角色</SelectItem>
                <SelectItem value="admin">管理员</SelectItem>
                <SelectItem value="user">普通用户</SelectItem>
              </SelectContent>
            </Select>
            <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as 'all' | UserStatus)}>
              <SelectTrigger className="h-9 w-32">
                <SelectValue placeholder="状态" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="normal">正常</SelectItem>
                <SelectItem value="disabled">已禁用</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {loading ? (
          <div className="p-10 text-center text-sm text-muted-foreground">正在加载用户...</div>
        ) : filtered.length === 0 ? (
          <div className="p-6">
            <EmptyState
              icon={UsersIcon}
              title="没有匹配的用户"
              description="试试清空搜索条件,或邀请你的第一位团队成员。"
            />
          </div>
        ) : (
          <div className="relative w-full overflow-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>角色</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="min-w-[180px]">本月用量</TableHead>
                  <TableHead className="text-right">余额</TableHead>
                  <TableHead>注册时间</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((u, i) => {
                  const used = u.usedThisMonth;
                  const quota = Math.max(u.monthlyQuota, 1);
                  const pct = Math.min(100, Math.round((used / quota) * 100));
                  const isDisabled = u.status === 'disabled';
                  return (
                    <motion.tr
                      key={u.id}
                      initial={{ opacity: 0, y: 6 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: i * 0.03, ease: [0.22, 1, 0.36, 1] }}
                      className={cn(
                        'border-b transition-colors hover:bg-muted/40',
                        isDisabled && 'opacity-60 grayscale-[0.4]',
                      )}
                    >
                      <TableCell>
                        <div className="flex items-center gap-3">
                          <Avatar className="h-9 w-9 border bg-gradient-to-br from-primary/20 to-primary/5">
                            <AvatarFallback className="bg-transparent text-xs font-medium text-primary">
                              {initials(u.name)}
                            </AvatarFallback>
                          </Avatar>
                          <div className="min-w-0">
                            <p className="truncate text-sm font-medium">{u.name}</p>
                            <p className="truncate text-xs text-muted-foreground">{u.email}</p>
                            <p className="mt-1 truncate text-[11px] text-muted-foreground">
                              邀请码{' '}
                              <span className="font-mono">
                                {u.inviteCode || '--'}
                              </span>
                            </p>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <StatusBadge tone={u.role === 'admin' ? 'info' : 'neutral'} label={roleLabel(u.role)} />
                      </TableCell>
                      <TableCell>
                        <StatusBadge
                          tone={u.status === 'normal' ? 'success' : 'destructive'}
                          label={u.status === 'normal' ? '正常' : '已禁用'}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2.5">
                          <Progress
                            value={pct}
                            className={cn(
                              'h-1.5 w-28',
                              pct >= 90 && '[&>div]:bg-destructive',
                              pct >= 70 && pct < 90 && '[&>div]:bg-amber-500',
                            )}
                          />
                          <span className="font-mono text-xs text-muted-foreground">
                            {formatNumberFull(used)} / {formatNumberFull(u.monthlyQuota)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm">{formatNumberFull(u.balance)}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {formatRelative(u.registeredAt)}
                      </TableCell>
                      <TableCell className="text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="w-40">
                            <DropdownMenuLabel>账号操作</DropdownMenuLabel>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem onClick={() => handleEditUser(u)}>
                              <Edit className="mr-2 h-3.5 w-3.5" />
                              编辑资料
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => toggleStatus(u.id)}>
                              <Power className="mr-2 h-3.5 w-3.5" />
                              {u.status === 'normal' ? '禁用账号' : '启用账号'}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => resetQuota(u.id)}>
                              <RefreshCw className="mr-2 h-3.5 w-3.5" />
                              重置配额
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <AlertDialog>
                              <AlertDialogTrigger asChild>
                                <DropdownMenuItem
                                  onSelect={(e) => e.preventDefault()}
                                  className="text-destructive focus:text-destructive"
                                >
                                  <Trash2 className="mr-2 h-3.5 w-3.5" />
                              禁用用户
                                </DropdownMenuItem>
                              </AlertDialogTrigger>
                              <AlertDialogContent>
                                <AlertDialogHeader>
                                  <AlertDialogTitle>禁用该用户?</AlertDialogTitle>
                                  <AlertDialogDescription>
                                    禁用后 <span className="font-medium text-foreground">{u.email}</span> 将无法继续登录和使用 API Key。
                                  </AlertDialogDescription>
                                </AlertDialogHeader>
                                <AlertDialogFooter>
                                  <AlertDialogCancel>取消</AlertDialogCancel>
                                  <AlertDialogAction
                                    className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                    onClick={() => removeUser(u.id)}
                                  >
                                    确认删除
                                  </AlertDialogAction>
                                </AlertDialogFooter>
                              </AlertDialogContent>
                            </AlertDialog>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </motion.tr>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </Card>
    </div>
  );
}
