import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import type { Role } from '@relay-api/lib';
import { useAuth } from '@/stores/auth';

export function useAuthGuard(requiredRole?: Role) {
  const navigate = useNavigate();
  const user = useAuth((s) => s.user);
  const isAuthed = useAuth((s) => s.isAuthed);
  const isRestoring = useAuth((s) => s.isRestoring);
  const restoreSession = useAuth((s) => s.restoreSession);
  const logout = useAuth((s) => s.logout);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    let alive = true;
    restoreSession().finally(() => {
      if (alive) setChecked(true);
    });
    return () => {
      alive = false;
    };
  }, [restoreSession]);

  useEffect(() => {
    if (!checked || isRestoring) return;
    if (!isAuthed || !user) {
      navigate(requiredRole ? `/login?role=${requiredRole}` : '/login', { replace: true });
      return;
    }
    const allowed = !requiredRole || user.role === requiredRole || (requiredRole === 'user' && user.role === 'admin');
    if (!allowed) {
      logout();
      navigate(`/login?role=${requiredRole}`, { replace: true });
    }
  }, [checked, isRestoring, isAuthed, user, requiredRole, navigate, logout]);

  return checked && !isRestoring ? user : null;
}
