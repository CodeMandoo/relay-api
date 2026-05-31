import { Bot, BrainCircuit, Cloud, Cpu, Flame, Hexagon, Layers, Moon, Network, Orbit, Sparkles, Zap } from 'lucide-react';
import { cn } from '@relay-api/ui';

type ProviderIconConfig = { icon: typeof Bot; color: string; bg: string };

const map: Record<string, ProviderIconConfig> = {
  OpenAI: { icon: Bot, color: 'text-emerald-600 dark:text-emerald-400', bg: 'bg-emerald-500/10' },
  Anthropic: { icon: BrainCircuit, color: 'text-orange-600 dark:text-orange-400', bg: 'bg-orange-500/10' },
  Google: { icon: Sparkles, color: 'text-sky-600 dark:text-sky-400', bg: 'bg-sky-500/10' },
  xAI: { icon: Zap, color: 'text-foreground', bg: 'bg-muted' },
  DeepSeek: { icon: Network, color: 'text-blue-600 dark:text-blue-400', bg: 'bg-blue-500/10' },
  Qwen: { icon: Hexagon, color: 'text-violet-600 dark:text-violet-400', bg: 'bg-violet-500/10' },
  Kimi: { icon: Moon, color: 'text-indigo-600 dark:text-indigo-400', bg: 'bg-indigo-500/10' },
  GLM: { icon: Cpu, color: 'text-cyan-600 dark:text-cyan-400', bg: 'bg-cyan-500/10' },
  MiMo: { icon: Orbit, color: 'text-rose-600 dark:text-rose-400', bg: 'bg-rose-500/10' },
  MiniMax: { icon: Layers, color: 'text-fuchsia-600 dark:text-fuchsia-400', bg: 'bg-fuchsia-500/10' },
  Doubao: { icon: Bot, color: 'text-lime-700 dark:text-lime-400', bg: 'bg-lime-500/10' },
  Hunyuan: { icon: Cloud, color: 'text-teal-600 dark:text-teal-400', bg: 'bg-teal-500/10' },
  ERNIE: { icon: Sparkles, color: 'text-red-600 dark:text-red-400', bg: 'bg-red-500/10' },
  Baichuan: { icon: Network, color: 'text-amber-700 dark:text-amber-400', bg: 'bg-amber-500/10' },
  Yi: { icon: Bot, color: 'text-stone-700 dark:text-stone-300', bg: 'bg-stone-500/10' },
  StepFun: { icon: Orbit, color: 'text-purple-600 dark:text-purple-400', bg: 'bg-purple-500/10' },
  Mistral: { icon: Flame, color: 'text-orange-700 dark:text-orange-400', bg: 'bg-orange-500/10' },
  Meta: { icon: Network, color: 'text-blue-700 dark:text-blue-400', bg: 'bg-blue-500/10' },
  Cohere: { icon: Layers, color: 'text-green-700 dark:text-green-400', bg: 'bg-green-500/10' },
  Perplexity: { icon: Sparkles, color: 'text-teal-700 dark:text-teal-400', bg: 'bg-teal-500/10' },
  NVIDIA: { icon: Cpu, color: 'text-green-700 dark:text-green-400', bg: 'bg-green-500/10' },
};

interface ProviderIconProps {
  provider: string;
  className?: string;
  size?: 'sm' | 'md' | 'lg';
}

export function ProviderIcon({ provider, className, size = 'sm' }: ProviderIconProps) {
  const m = map[provider] ?? map.OpenAI;
  const Icon = m.icon;
  const box = size === 'lg' ? 'h-10 w-10' : size === 'md' ? 'h-8 w-8' : 'h-6 w-6';
  const ic = size === 'lg' ? 'h-5 w-5' : size === 'md' ? 'h-4 w-4' : 'h-3.5 w-3.5';
  return (
    <span className={cn('inline-flex items-center justify-center rounded-md', box, m.bg, className)}>
      <Icon className={cn(ic, m.color)} />
    </span>
  );
}
