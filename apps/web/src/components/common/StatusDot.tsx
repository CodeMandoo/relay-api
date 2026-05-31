import { cn } from '@relay-api/ui';

type Tone = 'online' | 'offline' | 'idle' | 'warning';

const palette: Record<Tone, { dot: string; ping: string }> = {
  online: { dot: 'bg-emerald-500', ping: 'bg-emerald-400' },
  offline: { dot: 'bg-destructive', ping: 'bg-destructive/60' },
  idle: { dot: 'bg-muted-foreground/60', ping: 'bg-muted-foreground/30' },
  warning: { dot: 'bg-amber-500', ping: 'bg-amber-300' },
};

interface StatusDotProps {
  tone: Tone;
  pulsing?: boolean;
  className?: string;
  size?: 'sm' | 'md';
}

export function StatusDot({ tone, pulsing = true, className, size = 'sm' }: StatusDotProps) {
  const colors = palette[tone];
  const dim = size === 'md' ? 'h-3 w-3' : 'h-2.5 w-2.5';
  return (
    <span className={cn('relative flex shrink-0', dim, className)}>
      {pulsing && tone !== 'offline' && (
        <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-70 animate-ping', colors.ping)} />
      )}
      <span className={cn('relative inline-flex rounded-full', dim, colors.dot)} />
    </span>
  );
}
