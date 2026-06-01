import * as React from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Search,
  LayoutDashboard,
  Users,
  ServerCog,
  Blocks,
  Ticket,
  ScrollText,
  BarChart3,
  Settings,
  Key,
  LogOut,
} from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  Input,
  ScrollArea,
  Button,
  cn,
} from '@relay-api/ui';
import { useAuth } from '@/stores/auth';

interface CommandItem {
  id: string;
  label: string;
  to?: string;
  action?: () => void;
  icon: React.ElementType;
  category: string;
}

const adminCommands: CommandItem[] = [
  { id: 'dash', label: '控制面板', to: '/admin/dashboard', icon: LayoutDashboard, category: '导航' },
  { id: 'users', label: '用户管理', to: '/admin/users', icon: Users, category: '导航' },
  { id: 'sources', label: '上游源管理', to: '/admin/sources', icon: ServerCog, category: '导航' },
  { id: 'models', label: '模型配置', to: '/admin/models', icon: Blocks, category: '导航' },
  { id: 'logs', label: '查看请求日志', to: '/admin/logs', icon: ScrollText, category: '功能' },
  { id: 'usage', label: '全局用量统计', to: '/admin/usage', icon: BarChart3, category: '功能' },
  { id: 'settings', label: '系统设置', to: '/admin/settings', icon: Settings, category: '系统' },
];

const userCommands: CommandItem[] = [
  { id: 'user-dashboard', label: '用量概览', to: '/user/dashboard', icon: LayoutDashboard, category: '导航' },
  { id: 'user-models', label: '可用模型', to: '/user/models', icon: Blocks, category: '导航' },
  { id: 'user-keys', label: 'API Keys', to: '/user/api-keys', icon: Key, category: '用户' },
  { id: 'user-usage', label: '用量明细', to: '/user/usage', icon: BarChart3, category: '用户' },
  { id: 'user-logs', label: '请求日志', to: '/user/logs', icon: ScrollText, category: '用户' },
];

const systemCommands: CommandItem[] = [
  { id: 'logout', label: '退出登录', action: () => {}, icon: LogOut, category: '系统' },
];

interface CommandPaletteProps {
  scope: 'admin' | 'user';
}

export function CommandPalette({ scope }: CommandPaletteProps) {
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState('');
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);

  React.useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((open) => !open);
      }
    };
    document.addEventListener('keydown', down);
    return () => document.removeEventListener('keydown', down);
  }, []);

  const commands = React.useMemo(() => {
    if (scope === 'admin') {
      return [...adminCommands, { id: 'switch-user', label: '切换到用户端', to: '/user/dashboard', icon: Key, category: '切换' }, ...systemCommands];
    }
    const switchCommands: CommandItem[] =
      user?.role === 'admin'
        ? [{ id: 'switch-admin', label: '切换到管理端', to: '/admin/dashboard', icon: Settings, category: '切换' }]
        : [];
    return [...userCommands, ...switchCommands, ...systemCommands];
  }, [scope, user?.role]);

  const filtered = commands.filter((c) =>
    c.label.toLowerCase().includes(query.toLowerCase()) ||
    c.category.toLowerCase().includes(query.toLowerCase())
  );

  const handleSelect = (item: CommandItem) => {
    if (item.id === 'logout') {
      logout();
      navigate('/login');
    } else if (item.to) {
      navigate(item.to);
    }
    setOpen(false);
    setQuery('');
  };

  const categories = Array.from(new Set(filtered.map((c) => c.category)));

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <div className="hidden h-9 w-72 cursor-pointer items-center gap-2 rounded-lg border bg-muted/40 px-3 text-sm text-muted-foreground transition-all hover:bg-muted hover:border-foreground/20 lg:flex">
          <Search className="h-4 w-4" />
          <span>搜索任何内容…</span>
          <kbd className="ml-auto rounded border bg-background px-1.5 py-0.5 text-[10px] font-bold text-muted-foreground/60 shadow-sm">
            ⌘K
          </kbd>
        </div>
      </DialogTrigger>
      <DialogContent showClose={false} className="max-w-[640px] p-0 overflow-hidden border-none bg-transparent shadow-none top-[15%] translate-y-0">
        <div className="rounded-2xl border bg-card shadow-2xl shadow-black/20 overflow-hidden">
          <div className="flex items-center gap-3 px-4 h-14 border-b">
            <Search className="h-5 w-5 text-muted-foreground/60" />
            <Input
              autoFocus
              className="flex-1 border-none bg-transparent h-full px-0 focus-visible:ring-0 text-base font-medium placeholder:text-muted-foreground/40"
              placeholder="输入命令或关键词搜索..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <kbd className="rounded border bg-muted px-1.5 py-0.5 text-[10px] font-bold text-muted-foreground/60">
              ESC
            </kbd>
          </div>
          <ScrollArea className="max-h-[420px]">
            <div className="p-2 space-y-4">
              {categories.length === 0 && (
                <div className="py-12 text-center">
                  <p className="text-sm font-bold text-muted-foreground/40 tracking-tight">未找到相关结果</p>
                </div>
              )}
              {categories.map((cat) => (
                <div key={cat} className="space-y-1">
                  <div className="px-3 py-2 text-[10px] font-bold uppercase tracking-[0.25em] text-muted-foreground/40">
                    {cat}
                  </div>
                  <div className="space-y-0.5">
                    {filtered
                      .filter((c) => c.category === cat)
                      .map((item) => (
                        <button
                          key={item.id}
                          className="w-full flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-semibold transition-all hover:bg-muted group"
                          onClick={() => handleSelect(item)}
                        >
                          <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border/40 bg-muted/40 group-hover:bg-background transition-colors">
                            <item.icon className="h-4 w-4 text-muted-foreground group-hover:text-primary transition-colors" />
                          </div>
                          <span className="flex-1 text-left">{item.label}</span>
                          <span className="text-[10px] font-bold text-muted-foreground/20 uppercase group-hover:text-muted-foreground/40">跳至</span>
                        </button>
                      ))}
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
          <div className="flex items-center gap-4 px-4 h-10 border-t bg-muted/20 text-[10px] font-bold text-muted-foreground/60 uppercase tracking-widest">
             <div className="flex items-center gap-1.5">
                <kbd className="rounded bg-background px-1 border border-border/40 shadow-sm">↑↓</kbd>
                <span>选择</span>
             </div>
             <div className="flex items-center gap-1.5">
                <kbd className="rounded bg-background px-1 border border-border/40 shadow-sm">Enter</kbd>
                <span>确定</span>
             </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
