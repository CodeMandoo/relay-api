import { NavLink, useLocation } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  LayoutDashboard,
  Users,
  ServerCog,
  Blocks,
  Ticket,
  ScrollText,
  BarChart3,
  Settings,
  Key,
  Zap,
} from 'lucide-react';
import { cn } from '@relay-api/ui';

export type NavItem = {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
  exact?: boolean;
};

export type NavGroup = {
  title?: string;
  items: NavItem[];
};

export const adminNav: NavGroup[] = [
  {
    title: '运营',
    items: [
      { to: '/admin/dashboard', label: '总览看板', icon: LayoutDashboard, exact: true },
      { to: '/admin/users', label: '用户管理', icon: Users },
      { to: '/admin/sources', label: '上游源管理', icon: ServerCog },
      { to: '/admin/models', label: '模型配置', icon: Blocks },
      { to: '/admin/invite-codes', label: '邀请码管理', icon: Ticket },
    ],
  },
  {
    title: '观测',
    items: [
      { to: '/admin/logs', label: '请求日志', icon: ScrollText },
      { to: '/admin/usage', label: '全局用量', icon: BarChart3 },
    ],
  },
  {
    title: '系统',
    items: [{ to: '/admin/settings', label: '系统设置', icon: Settings }],
  },
];

export const userNav: NavGroup[] = [
  {
    items: [
      { to: '/user/dashboard', label: '用量概览', icon: LayoutDashboard, exact: true },
      { to: '/user/models', label: '可用模型', icon: Blocks },
      { to: '/user/api-keys', label: 'API Keys', icon: Key },
      { to: '/user/usage', label: '用量明细', icon: BarChart3 },
    ],
  },
];

interface SidebarProps {
  groups: NavGroup[];
  onNavigate?: () => void;
}

export function SidebarBrand() {
  return (
    <div className="flex h-[64px] items-center gap-3 px-5">
      <div className="relative flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
        <Zap className="h-5 w-5 fill-current" strokeWidth={2.4} />
        <span className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-sidebar bg-success shadow" />
      </div>
      <div className="flex flex-col leading-tight">
        <span className="text-[15px] font-semibold tracking-tight text-foreground">
          Relay API
        </span>
        <span className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
          Relay API
        </span>
      </div>
    </div>
  );
}

export function Sidebar({ groups, onNavigate }: SidebarProps) {
  const location = useLocation();

  return (
    <div className="flex h-full flex-col bg-transparent text-sidebar-foreground">
      <SidebarBrand />
      <nav className="flex-1 space-y-8 overflow-y-auto px-4 py-6 scrollbar-none">
        {groups.map((group, gi) => (
          <div key={gi} className="space-y-2">
            {group.title && (
              <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-[0.25em] text-muted-foreground/50">
                {group.title}
              </div>
            )}
            <div className="space-y-1.5">
              {group.items.map((item) => {
                const active = item.exact
                  ? location.pathname === item.to
                  : location.pathname === item.to || location.pathname.startsWith(item.to + '/');
                const Icon = item.icon;
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    onClick={onNavigate}
                    end={item.exact}
                    className={cn(
                      'group relative flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-semibold transition-all duration-300',
                      active
                        ? 'text-primary bg-primary/5 shadow-[inset_0_0_0_1px_rgba(0,0,0,0.02)]'
                        : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                    )}
                  >
                    <Icon
                      className={cn(
                        'relative z-10 h-4 w-4 shrink-0 transition-transform duration-500 group-hover:scale-110',
                        active && 'text-primary',
                      )}
                    />
                    <span className="relative z-10 truncate">{item.label}</span>
                    {active && (
                      <motion.div
                        layoutId="sidebar-dot"
                        className="relative z-10 ml-auto h-1.2 w-1.2 rounded-full bg-primary"
                        transition={{ type: 'spring', stiffness: 300, damping: 30 }}
                      />
                    )}
                  </NavLink>
                );
              })}
            </div>
          </div>
        ))}
      </nav>
      <div className="p-4">
        <div className="rounded-[18px] border border-border/40 bg-muted/20 p-4 shadow-sm">
          <div className="text-[10px] font-bold uppercase tracking-[0.2em] text-muted-foreground/70">
            系统状态
          </div>
          <div className="mt-2 flex items-center gap-2">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
            </span>
            <span className="text-xs font-bold">服务运行正常</span>
          </div>
        </div>
      </div>
    </div>
  );
}
