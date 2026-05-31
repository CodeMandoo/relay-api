import { cn } from '@relay-api/ui';

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: React.ReactNode;
  eyebrow?: string;
  className?: string;
}

export function PageHeader({ title, description, actions, eyebrow, className }: PageHeaderProps) {
  return (
    <div className={cn('flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between px-2', className)}>
      <div className="space-y-1.5">
        {eyebrow && (
          <div className="inline-flex items-center gap-2 text-[10px] font-bold uppercase tracking-[0.25em] text-primary/80">
            <span className="h-1 w-4 rounded-full bg-primary" />
            {eyebrow}
          </div>
        )}
        <h1 className="text-3xl font-bold tracking-tighter text-foreground text-balance">{title}</h1>
        {description && (
          <p className="max-w-xl text-sm font-medium text-muted-foreground leading-relaxed">{description}</p>
        )}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-3">{actions}</div>}
    </div>
  );
}
