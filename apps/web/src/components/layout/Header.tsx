import { useNavigate } from 'react-router-dom';
import {
  Activity,
  ChevronDown,
  LogOut,
  Menu,
  Settings,
  ShieldCheck,
  User as UserIcon,
} from 'lucide-react';
import {
  Avatar,
  AvatarFallback,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Sheet,
  SheetContent,
  SheetTrigger,
  SheetTitle,
} from '@relay-api/ui';
import { useAuth } from '@/stores/auth';
import { ThemeToggle } from '@/components/common/ThemeToggle';
import { Sidebar, type NavGroup } from './Sidebar';
import { NotificationsPopover } from './NotificationsPopover';
import { CommandPalette } from './CommandPalette';

interface HeaderProps {
  title: string;
  subtitle?: string;
  nav: NavGroup[];
  scope: 'admin' | 'user';
}

export function Header({ title, subtitle, nav, scope }: HeaderProps) {
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);
  const crossPortalTarget =
    user?.role === 'admin'
      ? scope === 'admin'
        ? { label: '用户端', path: '/user/dashboard', Icon: Activity }
        : { label: '管理端', path: '/admin/dashboard', Icon: ShieldCheck }
      : null;
  const CrossPortalIcon = crossPortalTarget?.Icon;

  const handleLogout = () => {
    logout();
    navigate('/login', { replace: true });
  };

  return (
    <header className="flex h-[64px] items-center gap-3 px-4 sm:px-6">
      <Sheet>
        <SheetTrigger asChild>
          <Button variant="outline" size="icon" className="lg:hidden">
            <Menu className="h-4 w-4" />
            <span className="sr-only">打开导航</span>
          </Button>
        </SheetTrigger>
        <SheetContent side="left" className="w-72 p-0">
          <SheetTitle className="sr-only">主导航</SheetTitle>
          <Sidebar groups={nav} />
        </SheetContent>
      </Sheet>

      <div className="hidden min-w-0 flex-col leading-tight md:flex">
        <h1 className="truncate text-lg font-semibold tracking-tight">{title}</h1>
        {subtitle && (
          <p className="truncate text-xs text-muted-foreground">{subtitle}</p>
        )}
      </div>

      <div className="ml-auto flex items-center gap-2">
        <CommandPalette scope={scope} />

        <NotificationsPopover scope={scope} />

        <ThemeToggle />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" className="h-auto rounded-full p-1 pr-3">
              <Avatar className="h-8 w-8 ring-2 ring-background">
                <AvatarFallback className="bg-primary text-xs font-semibold text-primary-foreground">
                  {user?.avatarText ?? 'GU'}
                </AvatarFallback>
              </Avatar>
              <div className="hidden text-left leading-tight sm:block">
                <div className="text-xs font-semibold">
                  {user?.name ?? 'Guest'}
                </div>
                <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
                  {scope === 'admin' ? '管理员' : '用户'}
                </div>
              </div>
              <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel className="flex flex-col">
              <span className="text-sm font-semibold">{user?.name ?? 'Guest'}</span>
              <span className="text-xs font-normal text-muted-foreground">
                {user?.email ?? 'unknown'}
              </span>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => navigate(scope === 'admin' ? '/admin/dashboard' : '/user/dashboard')}>
              <UserIcon className="mr-2 h-4 w-4" />
              <span>个人主页</span>
            </DropdownMenuItem>
            {crossPortalTarget && (
              <DropdownMenuItem onSelect={() => navigate(crossPortalTarget.path)}>
                {CrossPortalIcon && <CrossPortalIcon className="mr-2 h-4 w-4" />}
                <span>{crossPortalTarget.label}</span>
              </DropdownMenuItem>
            )}
            {scope === 'admin' && (
              <DropdownMenuItem onSelect={() => navigate('/admin/settings')}>
                <Settings className="mr-2 h-4 w-4" />
                <span>系统设置</span>
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={handleLogout}
              className="text-destructive focus:text-destructive"
            >
              <LogOut className="mr-2 h-4 w-4" />
              <span>退出登录</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
