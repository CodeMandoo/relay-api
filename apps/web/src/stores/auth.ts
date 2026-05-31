import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { AuthUser, Role } from '@relay-api/lib';
import { authApi, clearAuthTokens, readAuthTokens } from '@/lib/api';

interface AuthState {
  user: AuthUser | null;
  isAuthed: boolean;
  isRestoring: boolean;
  login: (email: string, password: string, role?: Role) => Promise<AuthUser>;
  register: (payload: { email: string; password: string; name?: string; inviteCode: string; emailCode?: string }) => Promise<AuthUser>;
  restoreSession: () => Promise<AuthUser | null>;
  logout: () => void;
}

export const useAuth = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      isAuthed: false,
      isRestoring: false,
      login: async (email, password, role = 'user') => {
        const response = await authApi.login({ email, password, role });
        set({ user: response.user, isAuthed: true, isRestoring: false });
        return response.user;
      },
      register: async (payload) => {
        const response = await authApi.register(payload);
        set({ user: response.user, isAuthed: true, isRestoring: false });
        return response.user;
      },
      restoreSession: async () => {
        const tokens = readAuthTokens();
        if (!tokens?.accessToken) {
          set({ user: null, isAuthed: false, isRestoring: false });
          return null;
        }

        set({ isRestoring: true });
        try {
          const response = await authApi.me();
          set({ user: response.user, isAuthed: true, isRestoring: false });
          return response.user;
        } catch {
          if (!tokens.refreshToken) {
            clearAuthTokens();
            set({ user: null, isAuthed: false, isRestoring: false });
            return null;
          }
          try {
            const response = await authApi.refresh(tokens.refreshToken);
            set({ user: response.user, isAuthed: true, isRestoring: false });
            return response.user;
          } catch {
            clearAuthTokens();
            set({ user: null, isAuthed: false, isRestoring: false });
            return null;
          }
        }
      },
      logout: () => {
        clearAuthTokens();
        set({ user: null, isAuthed: false, isRestoring: false });
      },
    }),
    {
      name: 'relay.auth',
      partialize: (state) => ({ user: state.user, isAuthed: state.isAuthed }),
    },
  ),
);
