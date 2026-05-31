import { motion } from 'framer-motion';

interface Segment {
  label: string;
  value: number;
  percentage: number;
  color: string;
}

interface StackedSegmentBarProps {
  segments: Segment[];
  valueFormatter?: (n: number) => string;
}

export function StackedSegmentBar({ segments, valueFormatter }: StackedSegmentBarProps) {
  return (
    <div className="space-y-3">
      <div className="flex h-3 w-full overflow-hidden rounded-full bg-muted">
        {segments.map((s, i) => (
          <motion.div
            key={s.label}
            initial={{ width: 0 }}
            animate={{ width: `${s.percentage}%` }}
            transition={{ delay: 0.1 + i * 0.08, duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
            style={{ background: s.color }}
            className="h-full"
            title={`${s.label} · ${s.percentage}%`}
          />
        ))}
      </div>
      <ul className="grid gap-2 text-xs sm:grid-cols-2">
        {segments.map((s) => (
          <li key={s.label} className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-md bg-muted/30 px-2 py-1.5">
            <span className="h-2.5 w-2.5 shrink-0 rounded-sm" style={{ background: s.color }} />
            <span className="min-w-0 truncate font-medium" title={s.label}>
              {s.label}
            </span>
            <span className="shrink-0 font-mono text-muted-foreground">
              {valueFormatter ? valueFormatter(s.value) : `${s.percentage}%`}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
