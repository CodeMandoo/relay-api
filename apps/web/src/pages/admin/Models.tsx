import { Fragment, useEffect, useMemo, useState } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import {
  Button,
  Card,
  CardContent,
  Input,
  Label,
  Switch,
  Checkbox,
  Badge,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@relay-api/ui';
import {
  Search,
  RefreshCw,
  Plus,
  MoreHorizontal,
  Edit,
  Copy,
  Trash2,
  Layers,
  Power,
  PowerOff,
  Cpu,
  Sparkles,
  Info,
} from 'lucide-react';
import { MODEL_PROVIDERS, copyToClipboard } from '@relay-api/lib';
import type { ModelAccessGroup, ModelFormat, PlatformModel, SourceKey, UpstreamSource } from '@relay-api/lib';
import { adminApi, getErrorMessage } from '@/lib/api';
import { PageHeader } from '@/components/common/PageHeader';
import { StatCard } from '@/components/common/StatCard';
import { ProviderIcon } from '@/components/common/ProviderIcon';
import { StatusBadge } from '@/components/common/StatusBadge';
import { ModelBindingFields, ModelSettingsForm } from '@/components/models/ModelSettingsForm';

type Provider = PlatformModel['provider'];
type Filter = 'all' | Provider | 'disabled';
type AdminModel = PlatformModel & { modelGroupId?: string };

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: '全部' },
  ...MODEL_PROVIDERS.map((provider) => ({ key: provider, label: provider })),
  { key: 'disabled', label: '已禁用' },
];

type ModelGroupDefinition = {
  id: string;
  name: string;
  description: string;
  bindings: GroupBindingConfig[];
  locked?: boolean;
};

type GroupBindingConfig = {
  sourceId: string;
  sourceKeyId: string;
  routingWeight: number;
};

const INITIAL_MODEL_GROUPS: ModelGroupDefinition[] = [
  { id: 'g_default', name: '默认分组', description: '当前所有已配置 Key 默认使用的模型集合', bindings: [], locked: true },
  { id: 'g_prod', name: '生产环境', description: '生产 Key 可访问的模型集合', bindings: [] },
  { id: 'g_team', name: '团队 A', description: '团队或项目专属模型集合', bindings: [] },
];

const DEFAULT_MODEL_GROUP_ID = 'g_default';

const MODEL_GROUP_STYLES: Record<string, { stripe: string; badge: string; dot: string }> = {
  g_default: {
    stripe: 'bg-sky-500',
    badge: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
    dot: 'bg-sky-500',
  },
  g_prod: {
    stripe: 'bg-emerald-500',
    badge: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
    dot: 'bg-emerald-500',
  },
  g_team: {
    stripe: 'bg-amber-500',
    badge: 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
    dot: 'bg-amber-500',
  },
};

const fallbackModelGroupStyle = {
  stripe: 'bg-muted-foreground',
  badge: 'border-border bg-muted text-muted-foreground',
  dot: 'bg-muted-foreground',
};

const defaultFormatsForProvider = (provider: Provider): ModelFormat[] => (provider === 'Anthropic' ? ['anthropic'] : ['openai']);

const modelFormats = (model: PlatformModel): ModelFormat[] =>
  model.formats?.length ? model.formats : defaultFormatsForProvider(model.provider);

type RouteCandidate = NonNullable<PlatformModel['routingCandidates']>[number];
type ModelBindingDraft = {
  clientId: string;
  id?: string;
  sourceId: string;
  sourceKeyId: string;
  routingWeight: number;
};
type ModelGroup = {
  key: string;
  modelGroupId: string;
  name: string;
  provider: Provider;
  formats: ModelFormat[];
  enabled: boolean;
  ids: string[];
  models: AdminModel[];
  candidates: RouteCandidate[];
  currentCandidate?: RouteCandidate;
};
const publicIdNumber = (id: string) => Number(id.split('_').pop() ?? 0) || 0;

const routeCandidateWeight = (candidate: RouteCandidate) => Math.max(1, candidate.routingWeight || 1);

const isRouteCandidateSchedulable = (candidate: RouteCandidate) =>
  candidate.modelEnabled && candidate.sourceStatus === 'online' && !candidate.coolingDown;

const routeCandidateOrder = (left: RouteCandidate, right: RouteCandidate) => {
  const leftSchedulable = isRouteCandidateSchedulable(left);
  const rightSchedulable = isRouteCandidateSchedulable(right);
  if (leftSchedulable !== rightSchedulable) return leftSchedulable ? -1 : 1;
  const weightDiff = routeCandidateWeight(right) - routeCandidateWeight(left);
  if (weightDiff !== 0) return weightDiff;
  if (left.sourcePriority !== right.sourcePriority) return left.sourcePriority - right.sourcePriority;
  return publicIdNumber(left.id) - publicIdNumber(right.id);
};

const sortedRouteCandidates = (model: PlatformModel) => [...(model.routingCandidates ?? [])].sort(routeCandidateOrder);

const currentRouteCandidate = (model: PlatformModel) =>
  sortedRouteCandidates(model).find(isRouteCandidateSchedulable);

const routeCandidateTone = (candidate: NonNullable<PlatformModel['routingCandidates']>[number]): string => {
  if (!candidate.modelEnabled) return 'text-muted-foreground';
  if (candidate.coolingDown || candidate.sourceStatus === 'offline') return 'text-amber-600 dark:text-amber-400';
  if (candidate.sourceStatus === 'disabled') return 'text-destructive';
  return 'text-emerald-600 dark:text-emerald-400';
};

const modelGroupStyle = (groupId: string) => MODEL_GROUP_STYLES[groupId] ?? fallbackModelGroupStyle;

const modelGroupDefinitionFromDTO = (group: ModelAccessGroup): ModelGroupDefinition => ({
  id: group.id,
  name: group.name,
  description: group.description ?? '模型分组',
  locked: group.isDefault,
  bindings: (group.bindings ?? []).map((binding) => ({
    sourceId: binding.sourceId,
    sourceKeyId: binding.sourceKeyId ?? 'default',
    routingWeight: binding.routingWeight,
  })),
});

function RoutingRuleHint() {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="inline-flex h-5 w-5 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground">
          <Info className="h-3.5 w-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" align="start" className="max-w-sm text-xs leading-relaxed">
        启用状态即参与调度。先过滤未启用、上游非在线和冷却中候选；正常请求按权重 W 做平滑加权轮询，失败、超时、429 或 5xx 会切到下一个候选，并按连续失败进入短/中/长冷却。
      </TooltipContent>
    </Tooltip>
  );
}

const makeBindingDraft = (sourceId = '', overrides: Partial<ModelBindingDraft> = {}): ModelBindingDraft => ({
  clientId: `binding_${Date.now()}_${Math.random().toString(36).slice(2)}`,
  sourceId,
  sourceKeyId: 'default',
  routingWeight: 1,
  ...overrides,
});

const attachModelGroupIds = (rows: PlatformModel[], previous: AdminModel[] = []): AdminModel[] => {
  const previousGroupById = new Map(previous.map((model) => [model.id, model.modelGroupId ?? DEFAULT_MODEL_GROUP_ID]));
  return rows.map((model) => ({
    ...model,
    modelGroupId: model.modelGroupId ?? previousGroupById.get(model.id) ?? DEFAULT_MODEL_GROUP_ID,
  }));
};

const buildModelGroups = (models: AdminModel[]): ModelGroup[] => {
  const grouped = new Map<string, AdminModel[]>();
  for (const model of models) {
    const modelGroupId = model.modelGroupId ?? DEFAULT_MODEL_GROUP_ID;
    const key = `${modelGroupId}:${model.name}`;
    grouped.set(key, [...(grouped.get(key) ?? []), model]);
  }
  return Array.from(grouped.entries())
    .map(([key, rows]) => {
      const modelGroupId = rows[0].modelGroupId ?? DEFAULT_MODEL_GROUP_ID;
      const name = rows[0].name;
      const candidates = sortedRouteCandidates(rows[0]);
      const currentCandidate = currentRouteCandidate(rows[0]);
      const currentModel = rows.find((row) => row.id === currentCandidate?.id) ?? rows[0];
      return {
        key,
        modelGroupId,
        name,
        provider: currentModel.provider,
        formats: modelFormats(currentModel),
        enabled: rows.some((row) => row.enabled),
        ids: rows.map((row) => row.id),
        models: rows,
        candidates,
        currentCandidate,
      };
    })
    .sort((left, right) => left.name.localeCompare(right.name));
};

export default function Page() {
  const [models, setModels] = useState<AdminModel[]>([]);
  const [sources, setSources] = useState<UpstreamSource[]>([]);
  const [sourceKeysBySource, setSourceKeysBySource] = useState<Record<string, SourceKey[]>>({});
  const [sourceKeyLoadingBySource, setSourceKeyLoadingBySource] = useState<Record<string, boolean>>({});
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [modelGroupFilter, setModelGroupFilter] = useState<string>('all');
  const [modelGroupOptions, setModelGroupOptions] = useState<ModelGroupDefinition[]>(INITIAL_MODEL_GROUPS);
  const [groupEditorOpen, setGroupEditorOpen] = useState(false);
  const [editingModelGroup, setEditingModelGroup] = useState<ModelGroupDefinition | null>(null);
  const [groupNameDraft, setGroupNameDraft] = useState('');
  const [groupDescriptionDraft, setGroupDescriptionDraft] = useState('');
  const [groupBindings, setGroupBindings] = useState<ModelBindingDraft[]>([]);
  const [pendingGroupUpdate, setPendingGroupUpdate] = useState<{ groupId: string; groupName: string; bindings: GroupBindingConfig[] } | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [syncing, setSyncing] = useState(false);
  
  const [addModelOpen, setAddModelOpen] = useState(false);
  const [addingModelGroupId, setAddingModelGroupId] = useState<string>(DEFAULT_MODEL_GROUP_ID);
  const [editOpen, setEditOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<ModelGroup | null>(null);

  // Form state
  const [newName, setNewName] = useState('');
  const [newProvider, setNewProvider] = useState<Provider>('OpenAI');
  const [newFormats, setNewFormats] = useState<ModelFormat[]>(['openai']);
  const [newBindings, setNewBindings] = useState<ModelBindingDraft[]>([]);

  const reloadModels = () => {
    setSyncing(true);
    return Promise.all([adminApi.models(), adminApi.sources(), adminApi.modelGroups()])
      .then(([modelResponse, sourceResponse, groupResponse]) => {
        const previousGroupById = new Map(modelGroupOptions.map((group) => [group.id, group]));
        const nextGroups = groupResponse.data.map((group) => {
          const nextGroup = modelGroupDefinitionFromDTO(group);
          return {
            ...nextGroup,
            bindings: nextGroup.bindings.length ? nextGroup.bindings : previousGroupById.get(group.id)?.bindings ?? [],
          };
        });
        setModels((current) => attachModelGroupIds(modelResponse.data, current));
        setSources(sourceResponse.data);
        setModelGroupOptions(nextGroups.length ? nextGroups : INITIAL_MODEL_GROUPS);
      })
      .catch((error) => toast.error(getErrorMessage(error, '加载模型配置失败')))
      .finally(() => setSyncing(false));
  };

  useEffect(() => {
    reloadModels();
  }, []);

  const ensureSourceKeys = (sourceId: string) => {
    if (!sourceId || sourceKeysBySource[sourceId] || sourceKeyLoadingBySource[sourceId]) return;
    const source = sources.find((item) => item.id === sourceId);
    if (!source || source.type === 'CLIProxyAPI') {
      setSourceKeysBySource((current) => ({ ...current, [sourceId]: [] }));
      return;
    }
    setSourceKeyLoadingBySource((current) => ({ ...current, [sourceId]: true }));
    adminApi
      .sourceKeys(sourceId)
      .then((response) => setSourceKeysBySource((current) => ({ ...current, [sourceId]: response.data })))
      .catch((error) => toast.error(getErrorMessage(error, '加载上游 Key 失败')))
      .finally(() => setSourceKeyLoadingBySource((current) => ({ ...current, [sourceId]: false })));
  };

  useEffect(() => {
    if (!editOpen && !addModelOpen && !groupEditorOpen) return;
    [...newBindings, ...groupBindings].forEach((binding) => ensureSourceKeys(binding.sourceId));
  }, [editOpen, addModelOpen, groupEditorOpen, newBindings, groupBindings, sources, sourceKeysBySource, sourceKeyLoadingBySource]);

  const modelGroups = useMemo(() => buildModelGroups(models), [models]);
  const modelGroupFilterOptions = useMemo(
    () => [{ id: 'all', name: '全部模型分组' }, ...modelGroupOptions],
    [modelGroupOptions],
  );

  const modelGroupName = (groupId: string) =>
    modelGroupOptions.find((group) => group.id === groupId)?.name ?? '默认分组';

  const groupContainsModel = (groupId: string, model: ModelGroup) => model.modelGroupId === groupId;

  const modelVisibleInGroup = (model: ModelGroup, groupId: string) => {
    if (groupId === 'all') return true;
    return groupContainsModel(groupId, model);
  };

  const filtered = useMemo(() => {
    return modelGroups
      .filter((model) => {
        const keyword = search.toLowerCase();
        if (keyword && !model.name.toLowerCase().includes(keyword)) return false;
        if (!modelVisibleInGroup(model, modelGroupFilter)) return false;
        if (filter === 'all') return true;
        if (filter === 'disabled') return !model.enabled;
        return model.provider === filter;
      });
  }, [modelGroups, search, filter, modelGroupFilter]);

  const displayGroups = useMemo(() => {
    const visibleGroups =
      modelGroupFilter === 'all'
        ? modelGroupOptions
        : modelGroupOptions.filter((group) => group.id === modelGroupFilter);
    return visibleGroups
      .map((group) => ({
        group,
        models: filtered.filter((model) => groupContainsModel(group.id, model)),
      }))
      .filter((section) => section.models.length > 0);
  }, [filtered, modelGroupFilter, modelGroupOptions]);

  const displayModelCount = useMemo(
    () => displayGroups.reduce((total, section) => total + section.models.length, 0),
    [displayGroups],
  );

  const groupBindingDrafts = (group?: ModelGroupDefinition | null) => {
    if (group?.bindings.length) {
      return group.bindings.map((binding) =>
        makeBindingDraft(binding.sourceId, {
          sourceKeyId: binding.sourceKeyId,
          routingWeight: binding.routingWeight,
        }),
      );
    }
    return [makeBindingDraft(sources[0]?.id ?? '')];
  };

  const serializeBindingDrafts = (bindings: ModelBindingDraft[]): GroupBindingConfig[] =>
    bindings
      .filter((binding) => binding.sourceId)
      .map((binding) => ({
        sourceId: binding.sourceId,
        sourceKeyId: binding.sourceKeyId,
        routingWeight: Math.max(1, binding.routingWeight || 1),
      }));

  const bindingsChanged = (left: GroupBindingConfig[], right: GroupBindingConfig[]) =>
    JSON.stringify(left) !== JSON.stringify(right);

  const stats = useMemo(() => {
    const enabled = modelGroups.filter((model) => model.enabled).length;
    const disabled = modelGroups.length - enabled;
    return { total: modelGroups.length, enabled, disabled, groups: modelGroupOptions.length };
  }, [modelGroups, modelGroupOptions]);

  const allChecked = filtered.length > 0 && filtered.every((model) => selected.has(model.key));
  const someChecked = filtered.some((model) => selected.has(model.key)) && !allChecked;
  const hasSelection = selected.size > 0;

  const toggleAll = () => {
    if (allChecked) {
      const next = new Set(selected);
      filtered.forEach((model) => next.delete(model.key));
      setSelected(next);
    } else {
      const next = new Set(selected);
      filtered.forEach((model) => next.add(model.key));
      setSelected(next);
    }
  };

  const toggleOne = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  const selectedModels = () => modelGroups.filter((model) => selected.has(model.key));

  const removeModelFromGroup = async (modelName: string, groupId: string) => {
    if (modelGroupOptions.find((group) => group.id === groupId)?.locked) {
      toast.error('默认分组包含所有已配置模型，不能单独移出');
      return;
    }
    const target = modelGroups.find((model) => model.modelGroupId === groupId && model.name === modelName);
    if (!target) return;
    try {
      await adminApi.batchModels(target.ids, 'delete');
      setModels((current) => current.filter((model) => !target.ids.includes(model.id)));
      toast.success('已从分组移出', { description: `${modelGroupName(groupId)} · ${modelName}` });
    } catch (error) {
      toast.error(getErrorMessage(error, '移出模型失败'));
    }
  };

  const openCreateGroup = () => {
    setEditingModelGroup(null);
    setGroupNameDraft('');
    setGroupDescriptionDraft('');
    setGroupBindings(groupBindingDrafts(null));
    setGroupEditorOpen(true);
  };

  const openEditGroup = (group: ModelGroupDefinition) => {
    setEditingModelGroup(group);
    setGroupNameDraft(group.name);
    setGroupDescriptionDraft(group.description);
    setGroupBindings(groupBindingDrafts(group));
    setGroupEditorOpen(true);
  };

  const saveModelGroup = async () => {
    const name = groupNameDraft.trim();
    if (!name) {
      toast.error('请输入分组名称');
      return;
    }
    const bindings = serializeBindingDrafts(groupBindings);
    if (bindings.length === 0) {
      toast.error('请至少选择一个上游源');
      return;
    }
    if (editingModelGroup) {
      const upstreamChanged = bindingsChanged(editingModelGroup.bindings, bindings);
      try {
        const response = await adminApi.updateModelGroup(editingModelGroup.id, {
          name,
          description: groupDescriptionDraft.trim() || editingModelGroup.description,
          bindings,
        });
        setModelGroupOptions((current) =>
          current.map((group) =>
            group.id === editingModelGroup.id
              ? {
                  ...group,
                  name: response.data.name,
                  description: response.data.description ?? group.description,
                  bindings,
                }
              : group,
          ),
        );
        toast.success('模型分组已更新');
        if (upstreamChanged) {
          setPendingGroupUpdate({ groupId: editingModelGroup.id, groupName: name, bindings });
        }
      } catch (error) {
        toast.error(getErrorMessage(error, '更新模型分组失败'));
        return;
      }
    } else {
      try {
        const response = await adminApi.createModelGroup({
          name,
          description: groupDescriptionDraft.trim() || '自定义模型分组',
          bindings,
        });
        const nextGroup = { ...modelGroupDefinitionFromDTO(response.data), bindings };
        setModelGroupOptions((current) => [...current, nextGroup]);
        setModelGroupFilter(nextGroup.id);
        toast.success('模型分组已添加，可以继续添加模型');
        setAddingModelGroupId(nextGroup.id);
        setNewName('');
        setNewProvider('OpenAI');
        setNewFormats(['openai']);
        setNewBindings(groupBindingDrafts(nextGroup));
        setAddModelOpen(true);
      } catch (error) {
        toast.error(getErrorMessage(error, '添加模型分组失败'));
        return;
      }
    }
    setGroupEditorOpen(false);
  };

  const deleteModelGroupDefinition = async (group: ModelGroupDefinition) => {
    if (group.locked) {
      toast.error('默认分组不能删除');
      return;
    }
    try {
      await adminApi.deleteModelGroup(group.id);
      setModelGroupOptions((current) => current.filter((item) => item.id !== group.id));
      setModels((current) => current.filter((model) => (model.modelGroupId ?? DEFAULT_MODEL_GROUP_ID) !== group.id));
      if (modelGroupFilter === group.id) setModelGroupFilter('all');
      toast.success('模型分组已删除', { description: group.name });
    } catch (error) {
      toast.error(getErrorMessage(error, '删除模型分组失败'));
    }
  };

  const openAddModelDialog = (groupId: string) => {
    const group = modelGroupOptions.find((item) => item.id === groupId);
    setAddingModelGroupId(groupId);
    setNewName('');
    setNewProvider('OpenAI');
    setNewFormats(['openai']);
    setNewBindings(groupBindingDrafts(group));
    setAddModelOpen(true);
  };

  const toggleModelFormat = (format: ModelFormat) => {
    setNewFormats((current) => {
      if (current.includes(format)) {
        return current.length === 1 ? current : current.filter((item) => item !== format);
      }
      return [...current, format];
    });
  };

  const updateBinding = (clientId: string, patch: Partial<ModelBindingDraft>) => {
    setNewBindings((current) =>
      current.map((binding) =>
        binding.clientId === clientId
          ? { ...binding, ...patch, sourceKeyId: patch.sourceId && patch.sourceId !== binding.sourceId ? 'default' : patch.sourceKeyId ?? binding.sourceKeyId }
          : binding,
      ),
    );
  };

  const updateGroupBinding = (clientId: string, patch: Partial<ModelBindingDraft>) => {
    setGroupBindings((current) =>
      current.map((binding) =>
        binding.clientId === clientId
          ? { ...binding, ...patch, sourceKeyId: patch.sourceId && patch.sourceId !== binding.sourceId ? 'default' : patch.sourceKeyId ?? binding.sourceKeyId }
          : binding,
      ),
    );
  };

  const addBinding = () => {
    const sourceId = sources[0]?.id ?? '';
    setNewBindings((current) => [...current, makeBindingDraft(sourceId)]);
  };

  const addGroupBinding = () => {
    const sourceId = sources[0]?.id ?? '';
    setGroupBindings((current) => [...current, makeBindingDraft(sourceId)]);
  };

  const removeBinding = (clientId: string) => {
    setNewBindings((current) => (current.length > 1 ? current.filter((binding) => binding.clientId !== clientId) : current));
  };

  const removeGroupBinding = (clientId: string) => {
    setGroupBindings((current) => (current.length > 1 ? current.filter((binding) => binding.clientId !== clientId) : current));
  };

  const validateBindings = () => {
    const validBindings = newBindings.filter((binding) => binding.sourceId);
    if (validBindings.length === 0) {
      toast.error('请至少选择一个上游源');
      return [];
    }
    return validBindings;
  };

  const handleAddModelToGroup = async () => {
    const modelName = newName.trim();
    if (!modelName) {
      toast.error('请输入模型名称');
      return;
    }
    const bindings = validateBindings();
    if (bindings.length === 0) return;
    try {
      const response = await adminApi.createModel({
        name: modelName,
        modelGroupId: addingModelGroupId,
        provider: newProvider,
        formats: newFormats,
        enabled: true,
        bindings: bindings.map((binding) => ({
          sourceId: binding.sourceId,
          sourceKeyId: binding.sourceKeyId === 'default' ? undefined : binding.sourceKeyId,
          routingWeight: binding.routingWeight,
        })),
      });
      setModels((current) => [{ ...response.data, modelGroupId: response.data.modelGroupId ?? addingModelGroupId }, ...current]);
      toast.success('模型已添加', { description: `${modelGroupName(addingModelGroupId)} · ${modelName}` });
      setAddModelOpen(false);
    } catch (error) {
      toast.error(getErrorMessage(error, '添加模型失败'));
    }
  };

  const applyGroupBindingsToModels = async () => {
    if (!pendingGroupUpdate) return;
    const affectedGroups = modelGroups.filter((model) => groupContainsModel(pendingGroupUpdate.groupId, model));
    try {
      await Promise.all(
        affectedGroups.map((group) =>
          adminApi.updateModel(group.ids[0], {
            modelGroupId: pendingGroupUpdate.groupId,
            bindings: pendingGroupUpdate.bindings.map((binding) => ({
              sourceId: binding.sourceId,
              sourceKeyId: binding.sourceKeyId === 'default' ? 'default' : binding.sourceKeyId,
              routingWeight: binding.routingWeight,
            })),
          }),
        ),
      );
      await reloadModels();
      toast.success('已更新当前分组所有模型的上游源配置', {
        description: `${pendingGroupUpdate.groupName} · ${affectedGroups.length} 个模型`,
      });
      setPendingGroupUpdate(null);
    } catch (error) {
      toast.error(getErrorMessage(error, '批量更新模型上游源失败'));
    }
  };

  const selectedModelIds = () =>
    selectedModels().flatMap((group) => group.ids);

  const runBatch = async (action: 'enable' | 'disable' | 'delete') => {
    const ids = selectedModelIds();
    if (ids.length === 0) return;
    try {
      await adminApi.batchModels(ids, action);
      if (action === 'delete') {
        setModels((prev) => prev.filter((model) => !ids.includes(model.id)));
      } else {
        setModels((prev) =>
          prev.map((model) => (ids.includes(model.id) ? { ...model, enabled: action === 'enable' } : model)),
        );
      }
      setSelected(new Set());
      toast.success(`已处理 ${selected.size} 个模型`);
    } catch (error) {
      toast.error(getErrorMessage(error, '批量操作失败'));
    }
  };

  const handleSync = async () => {
    await toast.promise(
      Promise.all([adminApi.models(), adminApi.sources(), adminApi.modelGroups()]).then(([modelResponse, sourceResponse, groupResponse]) => {
        const nextModels = attachModelGroupIds(modelResponse.data, models);
        const previousGroupById = new Map(modelGroupOptions.map((group) => [group.id, group]));
        const nextGroups = groupResponse.data.map((group) => {
          const nextGroup = modelGroupDefinitionFromDTO(group);
          return {
            ...nextGroup,
            bindings: nextGroup.bindings.length ? nextGroup.bindings : previousGroupById.get(group.id)?.bindings ?? [],
          };
        });
        setModels(nextModels);
        setSources(sourceResponse.data);
        setModelGroupOptions(nextGroups.length ? nextGroups : INITIAL_MODEL_GROUPS);
        return buildModelGroups(nextModels).length;
      }),
      {
        loading: '正在同步模型配置...',
        success: (count) => `成功同步 ${count} 个模型配置`,
        error: (error) => getErrorMessage(error, '同步失败'),
      },
    );
  };

  const [syncingPricing, setSyncingPricing] = useState(false);
  const handleSyncPricing = async () => {
    setSyncingPricing(true);
    try {
      const response = await adminApi.syncPricing();
      const result = response.result;
      const parts = [`已同步 ${result.synced} 个模型定价`];
      if (result.skipped > 0) parts.push(`${result.skipped} 个未匹配`);
      if (result.errors && result.errors.length > 0) parts.push(`${result.errors.length} 个错误`);
      toast.success(parts.join('，'));
      // Refresh model list to show updated pricing
      const modelResponse = await adminApi.models();
      setModels((current) => attachModelGroupIds(modelResponse.data, current));
    } catch (error) {
      toast.error(getErrorMessage(error, '同步定价失败'));
    } finally {
      setSyncingPricing(false);
    }
  };

  const toggleEnabled = async (group: ModelGroup) => {
    const action = group.enabled ? 'disable' : 'enable';
    try {
      await adminApi.batchModels(group.ids, action);
      setModels((prev) => prev.map((m) => (group.ids.includes(m.id) ? { ...m, enabled: action === 'enable' } : m)));
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型状态失败'));
    }
  };

  const deleteModelGroup = async (group: ModelGroup) => {
    try {
      await adminApi.batchModels(group.ids, 'delete');
      setModels((prev) => prev.filter((m) => !group.ids.includes(m.id)));
      setSelected((prev) => {
          const next = new Set(prev);
          next.delete(group.key);
          return next;
      });
      toast.success('模型已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除模型失败'));
    }
  };

  const handleEdit = (group: ModelGroup) => {
    setEditingGroup(group);
    setNewProvider(group.provider);
    setNewFormats(group.formats);
    setNewBindings(
      group.candidates.map((candidate) =>
        makeBindingDraft(candidate.sourceId, {
          id: candidate.id,
          sourceKeyId: candidate.sourceKeyId ?? 'default',
          routingWeight: routeCandidateWeight(candidate),
        }),
      ),
    );
    setEditOpen(true);
  };

  const saveEdit = async () => {
    if (!editingGroup) return;
    const bindings = validateBindings();
    if (bindings.length === 0) return;
    try {
      await adminApi.updateModel(editingGroup.ids[0], {
        provider: newProvider,
        formats: newFormats,
        bindings: bindings.map((binding) => ({
          id: binding.id,
          sourceId: binding.sourceId,
          sourceKeyId: binding.sourceKeyId === 'default' ? 'default' : binding.sourceKeyId,
          routingWeight: binding.routingWeight,
        })),
      });
      await reloadModels();
      toast.success('模型配置已更新');
      setEditOpen(false);
      setEditingGroup(null);
    } catch (error) {
      toast.error(getErrorMessage(error, '更新模型配置失败'));
    }
  };

  const renderBindingFields = (
    bindings = newBindings,
    onUpdate = updateBinding,
    onAdd = addBinding,
    onRemove = removeBinding,
  ) => (
    <ModelBindingFields
      bindings={bindings}
      sources={sources}
      sourceKeysBySource={sourceKeysBySource}
      sourceKeyLoadingBySource={sourceKeyLoadingBySource}
      routingHint={<RoutingRuleHint />}
      onUpdate={onUpdate}
      onAdd={onAdd}
      onRemove={onRemove}
    />
  );

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="模型治理"
        title="模型配置"
        description="管理可被代理调用的模型清单、上游绑定与启用状态。"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索模型名称..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 w-64 pl-9"
              />
            </div>
            <Button variant="outline" onClick={handleSync} disabled={syncing}>
              <RefreshCw className={cn('mr-2 h-4 w-4', syncing && 'animate-spin')} />
              同步上游模型
            </Button>
            <Button variant="outline" onClick={handleSyncPricing} disabled={syncingPricing}>
              <RefreshCw className={cn('mr-2 h-4 w-4', syncingPricing && 'animate-spin')} />
              从 LiteLLM 同步定价
            </Button>
            <Button variant="outline" onClick={openCreateGroup}>
              <Plus className="mr-2 h-4 w-4" />
              添加分组
            </Button>
          </div>
        }
      />

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="grid max-h-[calc(100vh-64px)] w-[min(92vw,672px)] max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-7">
          <DialogHeader>
            <DialogTitle className="text-2xl">编辑模型配置</DialogTitle>
            <DialogDescription>
              当前模型：<span className="font-mono font-semibold text-foreground">{editingGroup?.name}</span>。这里配置后台模型和上游源。
            </DialogDescription>
          </DialogHeader>
          <ModelSettingsForm
            modelName={editingGroup?.name ?? ''}
            modelNameReadOnly
            provider={newProvider}
            onProviderChange={(provider) => {
              setNewProvider(provider as Provider);
              setNewFormats(defaultFormatsForProvider(provider as Provider));
            }}
            formats={newFormats}
            onFormatToggle={toggleModelFormat}
            bindings={newBindings}
            sources={sources}
            sourceKeysBySource={sourceKeysBySource}
            sourceKeyLoadingBySource={sourceKeyLoadingBySource}
            routingHint={<RoutingRuleHint />}
            onUpdateBinding={updateBinding}
            onAddBinding={addBinding}
            onRemoveBinding={removeBinding}
          />
          <DialogFooter>
             <Button variant="ghost" onClick={() => setEditOpen(false)}>取消</Button>
             <Button onClick={saveEdit}>保存模型</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={groupEditorOpen} onOpenChange={setGroupEditorOpen}>
        <DialogContent className="grid max-h-[calc(100vh-64px)] w-[min(92vw,680px)] max-w-[680px] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden">
          <DialogHeader>
            <DialogTitle>{editingModelGroup ? '编辑模型分组' : '新建模型分组'}</DialogTitle>
            <DialogDescription>分组默认上游会用于分组内新增模型，也可以同步更新当前分组已有模型。</DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 gap-4 overflow-y-auto py-2 pr-1">
            <div className="grid gap-2">
              <Label>分组名称</Label>
              <Input value={groupNameDraft} onChange={(event) => setGroupNameDraft(event.target.value)} placeholder="例如：研发测试" />
            </div>
            <div className="grid gap-2">
              <Label>说明</Label>
              <Input value={groupDescriptionDraft} onChange={(event) => setGroupDescriptionDraft(event.target.value)} placeholder="这个分组的使用场景" />
            </div>
            <div className="grid gap-2">
              <Label>默认上游源配置</Label>
              {renderBindingFields(groupBindings, updateGroupBinding, addGroupBinding, removeGroupBinding)}
            </div>
          </div>
          <DialogFooter>
            {editingModelGroup && !editingModelGroup.locked && (
              <Button variant="ghost" className="mr-auto text-destructive hover:text-destructive" onClick={() => {
                deleteModelGroupDefinition(editingModelGroup);
                setGroupEditorOpen(false);
              }}>
                删除分组
              </Button>
            )}
            <Button variant="ghost" onClick={() => setGroupEditorOpen(false)}>取消</Button>
            <Button onClick={saveModelGroup}>{editingModelGroup ? '保存分组' : '创建分组'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(pendingGroupUpdate)} onOpenChange={(open) => !open && setPendingGroupUpdate(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>是否更新当前分组所有模型的上游源配置？</AlertDialogTitle>
            <AlertDialogDescription>
              已修改 <span className="font-semibold text-foreground">{pendingGroupUpdate?.groupName}</span> 的默认上游源配置。确认后会把当前分组下所有模型的上游源配置同步为新的分组默认配置。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setPendingGroupUpdate(null)}>仅保存分组配置</AlertDialogCancel>
            <AlertDialogAction onClick={applyGroupBindingsToModels}>更新所有模型</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={addModelOpen} onOpenChange={setAddModelOpen}>
        <DialogContent className="grid max-h-[calc(100vh-64px)] w-[min(92vw,672px)] max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-7">
          <DialogHeader>
            <DialogTitle className="text-2xl">添加模型</DialogTitle>
            <DialogDescription>
              当前模型分组：<span className="font-mono font-semibold text-foreground">{modelGroupName(addingModelGroupId)}</span>。这里配置后台模型和上游源。
            </DialogDescription>
          </DialogHeader>
          <ModelSettingsForm
            groupField={{
              label: '模型分组',
              value: addingModelGroupId,
              options: modelGroupOptions,
              onChange: (groupId) => {
                const group = modelGroupOptions.find((item) => item.id === groupId);
                setAddingModelGroupId(groupId);
                setNewBindings(groupBindingDrafts(group));
              },
            }}
            modelName={newName}
            modelNameInputId="group-model-name"
            onModelNameChange={setNewName}
            provider={newProvider}
            onProviderChange={(provider) => {
              setNewProvider(provider as Provider);
              setNewFormats(defaultFormatsForProvider(provider as Provider));
            }}
            formats={newFormats}
            onFormatToggle={toggleModelFormat}
            bindings={newBindings}
            sources={sources}
            sourceKeysBySource={sourceKeysBySource}
            sourceKeyLoadingBySource={sourceKeyLoadingBySource}
            routingHint={<RoutingRuleHint />}
            onUpdateBinding={updateBinding}
            onAddBinding={addBinding}
            onRemoveBinding={removeBinding}
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setAddModelOpen(false)}>取消</Button>
            <Button onClick={handleAddModelToGroup}>保存模型</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="模型总数" value={stats.total} icon={Layers} tone="primary" delay={0.02} hint="可被路由调度的模型" />
        <StatCard label="已启用" value={stats.enabled} icon={Power} tone="success" delay={0.06} hint="模型本身启用状态" />
        <StatCard label="已禁用" value={stats.disabled} icon={PowerOff} tone="warning" delay={0.1} hint="模型本身禁用状态" />
        <StatCard label="模型分组" value={stats.groups} icon={Sparkles} tone="neutral" delay={0.14} hint="独立分组数量" />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Select value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <SelectTrigger className="h-10 w-56 rounded-xl bg-muted/40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FILTERS.map((f) => (
              <SelectItem key={f.key} value={f.key}>
                {f.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={modelGroupFilter} onValueChange={setModelGroupFilter}>
          <SelectTrigger className="h-10 w-56 rounded-xl bg-muted/40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {modelGroupFilterOptions.map((group) => (
              <SelectItem key={group.id} value={group.id}>
                {group.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <Card className="overflow-hidden">
        <div className="flex min-h-[64px] flex-wrap items-center justify-between gap-3 border-b bg-card px-4 py-3">
          <div className="text-sm font-semibold">
            已选择 <span className="font-mono font-bold">{selected.size}</span> 个模型
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => runBatch('enable')} disabled={!hasSelection}>
              <Power className="mr-1.5 h-3.5 w-3.5" />
              批量启用
            </Button>
            <Button variant="outline" size="sm" onClick={() => runBatch('disable')} disabled={!hasSelection}>
              <PowerOff className="mr-1.5 h-3.5 w-3.5" />
              批量禁用
            </Button>
            <Button variant="destructive" size="sm" onClick={() => runBatch('delete')} disabled={!hasSelection}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              删除
            </Button>
          </div>
        </div>
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/40 hover:bg-muted/40">
              <TableHead className="w-10">
                <Checkbox
                  checked={someChecked ? 'indeterminate' : allChecked}
                  onCheckedChange={toggleAll}
                />
              </TableHead>
              <TableHead>模型名称</TableHead>
              <TableHead>上游源</TableHead>
              <TableHead>支持格式</TableHead>
              <TableHead className="text-center">状态</TableHead>
              <TableHead className="w-20 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {displayGroups.map((section, sectionIndex) => {
              const style = modelGroupStyle(section.group.id);
              return (
                <Fragment key={section.group.id}>
                  <TableRow className="border-b bg-muted/25 hover:bg-muted/25">
                    <TableCell colSpan={6} className="px-4 py-2">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                          <span className={cn('h-2 w-2 shrink-0 rounded-full', style.dot)} />
                          <span className="font-semibold text-foreground">{section.group.name}</span>
                          <span>· {section.models.length} 个模型</span>
                          <span className="min-w-0 truncate">{section.group.description}</span>
                        </div>
                        <div className="flex items-center gap-1.5">
                          <Button size="sm" variant="ghost" onClick={() => openAddModelDialog(section.group.id)}>
                            <Plus className="mr-1.5 h-3.5 w-3.5" />
                            添加模型
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => openEditGroup(section.group)}>
                            <Edit className="mr-1.5 h-3.5 w-3.5" />
                            编辑分组
                          </Button>
                        </div>
                      </div>
                    </TableCell>
                  </TableRow>
                  {section.models.map((group, i) => {
                    const currentCandidateId = group.currentCandidate?.id;
                    return (
                      <motion.tr
                        key={`${section.group.id}:${group.key}`}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: (sectionIndex + i) * 0.02, ease: [0.22, 1, 0.36, 1] }}
                        className={cn(
                          'group border-b transition-colors hover:bg-muted/40',
                          !group.enabled && 'opacity-60',
                        )}
                      >
                        <TableCell>
                          <Checkbox checked={selected.has(group.key)} onCheckedChange={() => toggleOne(group.key)} />
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2.5">
                            <ProviderIcon provider={group.provider} />
                            <div>
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="font-mono text-sm font-medium">{group.name}</span>
                              </div>
                              <div className="text-xs text-muted-foreground">{group.provider}</div>
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex min-w-0 flex-col items-start gap-1.5">
                            {group.candidates.map((candidate) => {
                              const isCurrent = candidate.id === currentCandidateId;
                              const muted = group.candidates.length > 1 && !isCurrent;
                              return (
                                <div
                                  key={candidate.id}
                                  className={cn(
                                    'flex max-w-[320px] flex-wrap items-center gap-1.5 text-[11px]',
                                    muted && 'text-muted-foreground',
                                  )}
                                >
                                  <span className={cn('h-1.5 w-1.5 rounded-full bg-current', muted ? 'text-muted-foreground' : routeCandidateTone(candidate))} />
                                  <span className={cn('truncate font-mono', !muted && 'font-semibold text-foreground')}>
                                    {candidate.sourceName}
                                  </span>
                                  <span className="font-mono">W{routeCandidateWeight(candidate)}</span>
                                </div>
                              );
                            })}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap items-center gap-1.5">
                            {group.formats.map((format) => (
                              <Badge
                                key={format}
                                variant="outline"
                                className={cn(
                                  'h-6 rounded-md px-2 py-0 font-mono text-[10px] font-bold uppercase tracking-wider shadow-none',
                                  format === 'openai'
                                    ? 'border-sky-500/20 bg-sky-500/5 text-sky-600'
                                    : 'border-orange-500/20 bg-orange-500/5 text-orange-600',
                                )}
                              >
                                {format}
                              </Badge>
                            ))}
                          </div>
                        </TableCell>
                        <TableCell className="text-center">
                          <div className="inline-flex items-center gap-2">
                            <Switch checked={group.enabled} onCheckedChange={() => toggleEnabled(group)} />
                            <StatusBadge tone={group.enabled ? 'success' : 'neutral'} label={group.enabled ? '启用' : '已禁用'} />
                          </div>
                        </TableCell>
                        <TableCell className="text-right">
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon" className="h-8 w-8">
                                <MoreHorizontal className="h-4 w-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onClick={() => handleEdit(group)}>
                                <Edit className="mr-2 h-4 w-4" />编辑配置
                              </DropdownMenuItem>
                              {!section.group.locked && (
                                <DropdownMenuItem onClick={() => removeModelFromGroup(group.name, section.group.id)}>
                                  <Trash2 className="mr-2 h-4 w-4" />移出当前分组
                                </DropdownMenuItem>
                              )}
                              <DropdownMenuItem
                                onClick={async () => {
                                  await copyToClipboard(group.name);
                                  toast.success('已复制', { description: group.name });
                                }}
                              >
                                <Copy className="mr-2 h-4 w-4" />复制名称
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <AlertDialog>
                                <AlertDialogTrigger asChild>
                                  <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={(e) => e.preventDefault()}>
                                    <Trash2 className="mr-2 h-4 w-4" />删除
                                  </DropdownMenuItem>
                                </AlertDialogTrigger>
                                <AlertDialogContent>
                                  <AlertDialogHeader>
                                    <AlertDialogTitle>删除模型 {group.name}?</AlertDialogTitle>
                                    <AlertDialogDescription>
                                      此操作不可撤销。该模型下的所有上游绑定将从路由列表移除，所有引用此模型的 API 请求将返回 404。
                                    </AlertDialogDescription>
                                  </AlertDialogHeader>
                                  <AlertDialogFooter>
                                    <AlertDialogCancel>取消</AlertDialogCancel>
                                    <AlertDialogAction onClick={() => deleteModelGroup(group)} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                                      确认删除
                                    </AlertDialogAction>
                                  </AlertDialogFooter>
                                </AlertDialogContent>
                              </AlertDialog>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </motion.tr>
                    );
                  })}
                </Fragment>
              );
            })}
            {displayGroups.length === 0 && (
              <TableRow>
                <TableCell colSpan={6}>
                  <div className="py-12 text-center text-sm text-muted-foreground">
                    <Cpu className="mx-auto mb-2 h-8 w-8 opacity-40" />
                    没有匹配的模型
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <CardContent className="flex items-center justify-between border-t bg-muted/20 py-3 text-xs text-muted-foreground">
          <div>显示 {displayModelCount} 条 · 共 {modelGroups.length} 个模型</div>
          <div className="text-[11px] uppercase tracking-wider">同步周期 · 每 6 小时</div>
        </CardContent>
      </Card>
    </div>
  );
}
