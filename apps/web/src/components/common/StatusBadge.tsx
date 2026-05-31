import { Badge } from '@relay-api/ui';
import { cn } from '@relay-api/ui';

type Tone = 'success' | 'warning' | 'destructive' | 'neutral' | 'info';

interface StatusBadgeProps {
  tone: Tone;
  label: string;
  dotted?: boolean;
  className?: string;
}

const toneMap: Record<Tone, string> = {
  success: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  warning: 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  destructive: 'border-destructive/30 bg-destructive/10 text-destructive',
  neutral: 'border-muted-foreground/30 bg-muted text-muted-foreground',
  info: 'border-sky-500/30 bg-sky-500/10 text-sky-600 dark:text-sky-400',
};

const dotMap: Record<Tone, string> = {
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  destructive: 'bg-destructive',
  neutral: 'bg-muted-foreground',
  info: 'bg-sky-500',
};

export function StatusBadge({ tone, label, dotted = true, className }: StatusBadgeProps) {
  return (
    <Badge
      variant="outline"
      className={cn('gap-1.5 font-medium', toneMap[tone], className)}
    >
      {dotted && <span className={cn('h-1.5 w-1.5 rounded-full', dotMap[tone])} />}
      {label}
    </Badge>
  );
}
