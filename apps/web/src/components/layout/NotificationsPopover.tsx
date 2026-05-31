import * as React from 'react';
import { AlertCircle, AlertTriangle, Bell, Check, Info, Trash2, type LucideIcon } from 'lucide-react';
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTrigger,
  ScrollArea,
  cn,
} from '@relay-api/ui';
import { toast } from 'sonner';
import { adminApi, userApi } from '@/lib/api';

type NotificationType = 'info' | 'success' | 'warning' | 'error';

interface Notification {
  id: string;
  title: string;
  description: string;
  time: string;
  type: NotificationType;
  read: boolean;
}

const notificationMeta: Record<
  NotificationType,
  {
    Icon: LucideIcon;
    iconClassName: string;
  }
> = {
  info: {
    Icon: Info,
    iconClassName: 'text-blue-600',
  },
  success: {
    Icon: Check,
    iconClassName: 'text-emerald-600',
  },
  warning: {
    Icon: AlertTriangle,
    iconClassName: 'text-amber-600',
  },
  error: {
    Icon: AlertCircle,
    iconClassName: 'text-destructive',
  },
};

export function NotificationsPopover({ scope }: { scope: 'admin' | 'user' }) {
  const [items, setItems] = React.useState<Notification[]>([]);
  const unreadCount = items.filter((i) => !i.read).length;

  React.useEffect(() => {
    let alive = true;
    if (scope === 'admin') {
      adminApi.dashboard().then((response) => {
        if (!alive) return;
        const warnings = response.data.upstreamStatuses
          .filter((source) => source.status !== 'online')
          .map<Notification>((source, index) => ({
            id: `source-${source.name}-${index}`,
            title: source.status === 'disabled' ? '上游已禁用' : '上游异常告警',
            description: `${source.name} 当前不可参与调度。`,
            time: '刚刚',
            type: source.status === 'disabled' ? 'warning' : 'error',
            read: false,
          }));
        setItems(
          warnings.length > 0
            ? warnings
            : [
                {
                  id: 'admin-ok',
                  title: '平台运行正常',
                  description: '当前没有未处理的上游异常。',
                  time: '刚刚',
                  type: 'success',
                  read: true,
                },
              ],
        );
      }).catch(() => {
        if (alive) setItems([]);
      });
      return () => {
        alive = false;
      };
    }

    userApi.dashboard().then((response) => {
      if (!alive) return;
      const quota = response.data.quota;
      const next: Notification[] = [];
      if (quota.percentageUsed >= 90) {
        next.push({
          id: 'quota-danger',
          title: '额度即将耗尽',
          description: `当前周期已使用 ${quota.percentageUsed.toFixed(1)}%，请及时调整用量。`,
          time: '刚刚',
          type: 'error',
          read: false,
        });
      } else if (quota.percentageUsed >= 75) {
        next.push({
          id: 'quota-warning',
          title: '额度接近上限',
          description: `当前周期已使用 ${quota.percentageUsed.toFixed(1)}%。`,
          time: '刚刚',
          type: 'warning',
          read: false,
        });
      }
      if (next.length === 0) {
        next.push({
          id: 'user-ok',
          title: '账户状态正常',
          description: '当前计费周期内额度充足。',
          time: '刚刚',
          type: 'success',
          read: true,
        });
      }
      setItems(next);
    }).catch(() => {
      if (alive) setItems([]);
    });
    return () => {
      alive = false;
    };
  }, [scope]);

  const markAllRead = () => {
    setItems(items.map((i) => ({ ...i, read: true })));
    toast.success('全部标记为已读');
  };

  const markRead = (id: string) => {
    setItems(items.map((i) => (i.id === id ? { ...i, read: true } : i)));
  };

  const clearAll = () => {
    setItems([]);
    toast.info('通知列表已清空');
  };

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label="通知">
          <Bell className="h-[18px] w-[18px]" />
          {unreadCount > 0 && (
            <span className="absolute right-2 top-2 flex h-2 w-2">
               <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-destructive opacity-75" />
               <span className="relative inline-flex h-2 w-2 rounded-full bg-destructive ring-2 ring-background" />
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[380px] p-0 shadow-2xl">
        <div className="flex items-center justify-between border-b px-4 py-3 bg-muted/20">
          <div className="flex items-center gap-2">
            <span className="text-sm font-bold tracking-tight">中心通知</span>
            {unreadCount > 0 && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-bold text-primary">
                {unreadCount} 条未读
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" className="h-7 px-2 text-[11px] font-bold" onClick={markAllRead}>
              全部已读
            </Button>
            <Button variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-destructive" onClick={clearAll}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <ScrollArea className="h-[400px]">
          {items.length === 0 ? (
            <div className="flex h-[300px] flex-col items-center justify-center gap-2 text-center">
              <div className="rounded-full bg-muted p-3">
                <Bell className="h-6 w-6 text-muted-foreground/40" />
              </div>
              <p className="text-sm font-medium text-muted-foreground/60">暂无任何通知</p>
            </div>
          ) : (
            <div className="divide-y divide-border/40">
              {items.map((item) => {
                const meta = notificationMeta[item.type];
                const Icon = meta.Icon;

                return (
                  <div
                    key={item.id}
                    className={cn(
                      'grid cursor-pointer grid-cols-[36px_1fr] gap-3 px-4 py-4 transition-colors hover:bg-muted/35',
                      !item.read && 'bg-primary/[0.04] hover:bg-primary/[0.06]'
                    )}
                    onClick={() => markRead(item.id)}
                  >
                    <div className="flex h-9 w-9 shrink-0 items-center justify-center">
                      <Icon className={cn('h-4 w-4', meta.iconClassName)} />
                    </div>
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-start justify-between gap-3">
                        <p
                          className={cn(
                            'min-w-0 text-sm leading-5',
                            item.read ? 'font-medium text-muted-foreground' : 'font-semibold text-foreground'
                          )}
                        >
                          {item.title}
                        </p>
                        <div className="flex shrink-0 items-center gap-1.5">
                          {!item.read && (
                            <span className="rounded-full border border-primary/20 bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-primary">
                              未读
                            </span>
                          )}
                          <span className="text-[10px] font-medium leading-5 text-muted-foreground/60">
                            {item.time}
                          </span>
                        </div>
                      </div>
                      <p className="line-clamp-2 text-[12px] leading-5 text-muted-foreground/70">
                        {item.description}
                      </p>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </ScrollArea>
        <div className="border-t bg-muted/10 p-2 text-center">
          <Button variant="ghost" size="sm" className="w-full text-[11px] font-bold text-muted-foreground">
            进入通知中心
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
