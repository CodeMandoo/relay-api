interface PageMeta {
  title: string;
  subtitle?: string;
}

const adminMap: Record<string, PageMeta> = {
  '/admin': { title: '总览看板', subtitle: '平台关键指标与上游健康度' },
  '/admin/dashboard': { title: '总览看板', subtitle: '平台关键指标与上游健康度' },
  '/admin/users': { title: '用户管理', subtitle: '所有平台用户、配额与状态' },
  '/admin/sources': { title: '上游源管理', subtitle: '中转池、OAuth 账号、容量' },
  '/admin/models': { title: '模型配置', subtitle: '可暴露模型与上游绑定' },
  '/admin/invite-codes': { title: '邀请码管理', subtitle: '注册邀请、有效期、配额' },
  '/admin/logs': { title: '请求日志', subtitle: '原始请求 / 响应 / 错误检索' },
  '/admin/usage': { title: '全局用量', subtitle: '全平台 Token / 成本 / 分布' },
  '/admin/settings': { title: '系统设置', subtitle: '注册策略、品牌、重试' },
};

const userMap: Record<string, PageMeta> = {
  '/user': { title: '用量概览', subtitle: '当前周期配额与活跃度' },
  '/user/dashboard': { title: '用量概览', subtitle: '当前周期配额与活跃度' },
  '/user/models': { title: '可用模型', subtitle: '在线模型与实时延迟' },
  '/user/api-keys': { title: 'API Keys', subtitle: '创建、管理、回收 sk-xxx' },
  '/user/usage': { title: '用量明细', subtitle: '按时间 / Key / 模型钻取' },
  '/user/logs': { title: '请求日志', subtitle: '逐次请求、Token、成本与结果' },
};

export function adminPageMeta(pathname: string): PageMeta {
  if (adminMap[pathname]) return adminMap[pathname];
  if (pathname.startsWith('/admin/sources/')) {
    return { title: '源账号管理', subtitle: '该上游下的 OAuth 账号与余额' };
  }
  return { title: '管理后台' };
}

export function userPageMeta(pathname: string): PageMeta {
  return userMap[pathname] ?? { title: '个人面板' };
}
