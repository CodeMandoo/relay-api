import { motion } from 'framer-motion';
import type { LucideIcon } from 'lucide-react';
import { Card } from '@relay-api/ui';
import { cn } from '@relay-api/ui';

interface StatCardProps {
  label: string;
  value: string | number;
  hint?: React.ReactNode;
  icon: LucideIcon;
  tone?: 'primary' | 'success' | 'warning' | 'destructive' | 'neutral';
  delay?: number;
  trend?: number;
}

const toneClasses: Record<NonNullable<StatCardProps['tone']>, { icon: string }> = {
  primary: { icon: 'text-primary bg-primary/10' },
  success: { icon: 'text-emerald-600 bg-emerald-500/10' },
  warning: { icon: 'text-amber-600 bg-amber-500/10' },
  destructive: { icon: 'text-destructive bg-destructive/10' },
  neutral: { icon: 'text-foreground bg-muted' },
};

export function StatCard({ label, value, hint, icon: Icon, tone = 'primary', delay = 0 }: StatCardProps) {      
  const t = toneClasses[tone];
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.6, delay, ease: [0.22, 1, 0.36, 1] }}
    >
      <Card className="group relative overflow-hidden border-border/40 p-6 card-hover bg-card/40 backdrop-blur-sm">
        <div className="absolute -right-6 -top-6 h-24 w-24 rounded-full bg-primary/5 blur-2xl transition-all duration-500 group-hover:scale-150 group-hover:bg-primary/10" />
        
        <div className="flex items-center gap-3">
          <div className={cn('flex h-10 w-10 items-center justify-center rounded-xl border border-white/10 shadow-sm transition-transform duration-500 group-hover:rotate-6', t.icon)}>
            <Icon className="h-5 w-5" />
          </div>
          <div className="text-[11px] font-bold uppercase tracking-[0.2em] text-muted-foreground/80">
            {label}
          </div>
        </div>
        
        <div className="mt-6 flex items-baseline gap-2">
          <div className="text-4xl font-semibold tracking-tighter text-foreground">
            {value}
          </div>
          {hint && <div className="text-xs font-medium">{hint}</div>}
        </div>
      </Card>
    </motion.div>
  );
}
