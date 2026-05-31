import { Suspense, lazy, type ReactNode } from 'react';
import { createBrowserRouter, Navigate } from 'react-router-dom';
import { AuthLayout } from '@/components/layout/AuthLayout';
import { AdminLayout } from '@/components/layout/AdminLayout';
import { UserLayout } from '@/components/layout/UserLayout';

const Login = lazy(() => import('@/pages/auth/Login'));

const AdminDashboard = lazy(() => import('@/pages/admin/Dashboard'));
const AdminUsers = lazy(() => import('@/pages/admin/Users'));
const AdminSources = lazy(() => import('@/pages/admin/Sources'));
const AdminSourceAccounts = lazy(() => import('@/pages/admin/SourceAccounts'));
const AdminModels = lazy(() => import('@/pages/admin/Models'));
const AdminInviteCodes = lazy(() => import('@/pages/admin/InviteCodes'));
const AdminLogs = lazy(() => import('@/pages/admin/Logs'));
const AdminUsage = lazy(() => import('@/pages/admin/Usage'));
const AdminSettings = lazy(() => import('@/pages/admin/Settings'));

const UserDashboard = lazy(() => import('@/pages/user/Dashboard'));
const UserModels = lazy(() => import('@/pages/user/Models'));
const UserApiKeys = lazy(() => import('@/pages/user/ApiKeys'));
const UserUsage = lazy(() => import('@/pages/user/Usage'));

const page = (node: ReactNode) => (
  <Suspense fallback={<div className="p-8 text-sm text-muted-foreground">正在加载页面...</div>}>
    {node}
  </Suspense>
);

export const router = createBrowserRouter([
  { path: '/', element: <Navigate to="/login" replace /> },
  {
    element: <AuthLayout />,
    children: [
      { path: '/login', element: page(<Login />) },
      { path: '/register', element: page(<Login />) },
    ],
  },
  {
    element: <AdminLayout />,
    children: [
      { path: '/admin', element: <Navigate to="/admin/dashboard" replace /> },
      { path: '/admin/dashboard', element: page(<AdminDashboard />) },
      { path: '/admin/users', element: page(<AdminUsers />) },
      { path: '/admin/sources', element: page(<AdminSources />) },
      { path: '/admin/sources/:sourceId/accounts', element: page(<AdminSourceAccounts />) },
      { path: '/admin/models', element: page(<AdminModels />) },
      { path: '/admin/invite-codes', element: page(<AdminInviteCodes />) },
      { path: '/admin/logs', element: page(<AdminLogs />) },
      { path: '/admin/usage', element: page(<AdminUsage />) },
      { path: '/admin/settings', element: page(<AdminSettings />) },
    ],
  },
  {
    element: <UserLayout />,
    children: [
      { path: '/user', element: <Navigate to="/user/dashboard" replace /> },
      { path: '/user/dashboard', element: page(<UserDashboard />) },
      { path: '/user/models', element: page(<UserModels />) },
      { path: '/user/api-keys', element: page(<UserApiKeys />) },
      { path: '/user/usage', element: page(<UserUsage />) },
    ],
  },
  { path: '*', element: <Navigate to="/login" replace /> },
]);
