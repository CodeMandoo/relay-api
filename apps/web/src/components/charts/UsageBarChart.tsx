import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

interface Point {
  label: string;
  value: number;
  isToday?: boolean;
}

interface UsageBarChartProps {
  data: Point[];
  height?: number;
  valueFormatter?: (n: number) => string;
}

export function UsageBarChart({
  data,
  height = 300,
  valueFormatter = (n) => n.toLocaleString(),
}: UsageBarChartProps) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} margin={{ top: 16, right: 0, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="barNormal" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={0.55} />
            <stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0.1} />
          </linearGradient>
          <linearGradient id="barAccent" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity={1} />
            <stop offset="100%" stopColor="hsl(var(--primary))" stopOpacity={0.45} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="4 4" stroke="hsl(var(--border))" vertical={false} />
        <XAxis
          dataKey="label"
          stroke="hsl(var(--muted-foreground))"
          fontSize={11}
          tickLine={false}
          axisLine={false}
        />
        <YAxis
          stroke="hsl(var(--muted-foreground))"
          fontSize={11}
          tickLine={false}
          axisLine={false}
          tickFormatter={valueFormatter}
          width={48}
        />
        <Tooltip
          cursor={{ fill: 'hsl(var(--muted) / 0.5)' }}
          contentStyle={{
            background: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: 8,
            fontSize: 12,
            boxShadow: '0 8px 24px -8px rgb(0 0 0 / 0.2)',
          }}
          labelStyle={{ color: 'hsl(var(--muted-foreground))', fontSize: 11 }}
          formatter={(value: number) => [valueFormatter(value), '请求量']}
        />
        <Bar
          dataKey="value"
          radius={[6, 6, 0, 0]}
          fill="url(#barNormal)"
          maxBarSize={36}
          isAnimationActive
          animationDuration={650}
        />
      </BarChart>
    </ResponsiveContainer>
  );
}
