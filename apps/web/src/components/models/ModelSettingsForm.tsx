import type { ReactNode } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import {
  Button,
  Checkbox,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@relay-api/ui';
import { MODEL_PROVIDERS } from '@relay-api/lib';
import type { ModelFormat, ModelProvider, SourceKeyStatus } from '@relay-api/lib';

export type ModelSettingsBinding = {
  clientId: string;
  id?: string;
  sourceId: string;
  sourceKeyId: string;
  routingWeight: number;
};

export type ModelSettingsSource = {
  id: string;
  name: string;
};

export type ModelSettingsSourceKey = {
  id: string;
  alias: string;
  masked?: string;
  status?: SourceKeyStatus;
};

export type ModelSettingsGroupOption = {
  id: string;
  name: string;
};

type ModelBindingFieldsProps = {
  bindings: ModelSettingsBinding[];
  sources: ModelSettingsSource[];
  sourceKeysBySource: Record<string, ModelSettingsSourceKey[]>;
  sourceKeyLoadingBySource?: Record<string, boolean>;
  routingHint?: ReactNode;
  onUpdate: (clientId: string, patch: Partial<ModelSettingsBinding>) => void;
  onAdd: () => void;
  onRemove: (clientId: string) => void;
};

const MODEL_FORMAT_OPTIONS: { value: ModelFormat; label: string }[] = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
];

export function ModelBindingFields({
  bindings,
  sources,
  sourceKeysBySource,
  sourceKeyLoadingBySource = {},
  routingHint,
  onUpdate,
  onAdd,
  onRemove,
}: ModelBindingFieldsProps) {
  return (
    <div className="grid gap-3">
      {bindings.map((binding, index) => {
        const sourceKeys = sourceKeysBySource[binding.sourceId] ?? [];
        const sourceKeyLoading = sourceKeyLoadingBySource[binding.sourceId];

        return (
          <div key={binding.clientId} className="grid gap-3 rounded-lg border bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-2">
              <div className="text-xs font-semibold text-muted-foreground">上游绑定 {index + 1}</div>
              <Button type="button" variant="ghost" size="sm" onClick={() => onRemove(binding.clientId)} disabled={bindings.length === 1}>
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                移除
              </Button>
            </div>
            <div className="grid min-w-0 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_112px]">
              <div className="grid min-w-0 gap-2">
                <Label>上游源</Label>
                <Select value={binding.sourceId} onValueChange={(value) => onUpdate(binding.clientId, { sourceId: value })}>
                  <SelectTrigger className="min-w-0 [&>span]:min-w-0 [&>span]:truncate">
                    <SelectValue placeholder="选择上游源" />
                  </SelectTrigger>
                  <SelectContent className="max-w-[var(--radix-select-trigger-width)]">
                    {sources.map((source) => (
                      <SelectItem key={source.id} value={source.id}>
                        {source.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid min-w-0 gap-2">
                <Label>API Key 绑定</Label>
                <Select
                  value={binding.sourceKeyId}
                  onValueChange={(value) => onUpdate(binding.clientId, { sourceKeyId: value })}
                  disabled={sourceKeyLoading || sourceKeys.length === 0}
                >
                  <SelectTrigger className="min-w-0 [&>span]:min-w-0 [&>span]:truncate">
                    <SelectValue placeholder="默认上游 Key" />
                  </SelectTrigger>
                  <SelectContent className="max-w-[var(--radix-select-trigger-width)]">
                    <SelectItem value="default">默认上游 Key</SelectItem>
                    {sourceKeys.map((key) => (
                      <SelectItem key={key.id} value={key.id} disabled={key.status !== undefined && key.status !== 'valid'}>
                        <span className="block max-w-full truncate">{key.alias}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid min-w-0 gap-2">
                <Label className="inline-flex items-center gap-1.5">
                  调度权重
                  {routingHint}
                </Label>
                <Input
                  type="number"
                  min={1}
                  value={binding.routingWeight}
                  onChange={(event) => onUpdate(binding.clientId, { routingWeight: Math.max(1, Number(event.target.value) || 1) })}
                />
              </div>
            </div>
          </div>
        );
      })}
      <Button type="button" variant="outline" onClick={onAdd} disabled={sources.length === 0}>
        <Plus className="mr-2 h-4 w-4" />
        添加上游源
      </Button>
    </div>
  );
}

type ModelSettingsFormProps = {
  groupField?: {
    label?: string;
    value: string;
    options: ModelSettingsGroupOption[];
    onChange: (value: string) => void;
  };
  modelName: string;
  modelNameInputId?: string;
  modelNamePlaceholder?: string;
  modelNameReadOnly?: boolean;
  onModelNameChange?: (value: string) => void;
  provider: ModelProvider;
  onProviderChange: (provider: ModelProvider) => void;
  formats: ModelFormat[];
  onFormatToggle: (format: ModelFormat) => void;
  bindings: ModelSettingsBinding[];
  sources: ModelSettingsSource[];
  sourceKeysBySource: Record<string, ModelSettingsSourceKey[]>;
  sourceKeyLoadingBySource?: Record<string, boolean>;
  routingHint?: ReactNode;
  onUpdateBinding: (clientId: string, patch: Partial<ModelSettingsBinding>) => void;
  onAddBinding: () => void;
  onRemoveBinding: (clientId: string) => void;
};

export function ModelSettingsForm({
  groupField,
  modelName,
  modelNameInputId,
  modelNamePlaceholder = 'gpt-4.1 / anthropic/claude-sonnet-4',
  modelNameReadOnly,
  onModelNameChange,
  provider,
  onProviderChange,
  formats,
  onFormatToggle,
  bindings,
  sources,
  sourceKeysBySource,
  sourceKeyLoadingBySource,
  routingHint,
  onUpdateBinding,
  onAddBinding,
  onRemoveBinding,
}: ModelSettingsFormProps) {
  return (
    <div className="grid min-h-0 gap-4 overflow-y-auto py-2 pr-1">
      {groupField && (
        <div className="grid gap-2">
          <Label>{groupField.label ?? '模型分组'}</Label>
          <Select value={groupField.value} onValueChange={groupField.onChange}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {groupField.options.map((group) => (
                <SelectItem key={group.id} value={group.id}>
                  {group.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-[1.4fr_1fr]">
        <div className="grid gap-2">
          <Label htmlFor={modelNameInputId}>模型名称</Label>
          <Input
            id={modelNameInputId}
            value={modelName}
            readOnly={modelNameReadOnly}
            className={modelNameReadOnly ? 'font-mono' : undefined}
            onChange={(event) => onModelNameChange?.(event.target.value)}
            placeholder={modelNamePlaceholder}
          />
        </div>
        <div className="grid gap-2">
          <Label>Provider</Label>
          <Select value={provider} onValueChange={(value) => onProviderChange(value as ModelProvider)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {MODEL_PROVIDERS.map((item) => (
                <SelectItem key={item} value={item}>
                  {item}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="grid gap-3 rounded-lg border bg-muted/20 p-3">
        <Label>协议格式</Label>
        <div className="flex flex-wrap gap-3">
          {MODEL_FORMAT_OPTIONS.map((option) => (
            <label key={option.value} className="flex h-9 cursor-pointer items-center gap-2 rounded-md border bg-background px-3 text-sm">
              <Checkbox checked={formats.includes(option.value)} onCheckedChange={() => onFormatToggle(option.value)} />
              <span>{option.label}</span>
            </label>
          ))}
        </div>
      </div>

      <ModelBindingFields
        bindings={bindings}
        sources={sources}
        sourceKeysBySource={sourceKeysBySource}
        sourceKeyLoadingBySource={sourceKeyLoadingBySource}
        routingHint={routingHint}
        onUpdate={onUpdateBinding}
        onAdd={onAddBinding}
        onRemove={onRemoveBinding}
      />
    </div>
  );
}
