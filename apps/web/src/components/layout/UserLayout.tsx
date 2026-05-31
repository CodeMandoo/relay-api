import { useEffect } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'framer-motion';
import { Sidebar, userNav } from './Sidebar';
import { Header } from './Header';
import { useAuthGuard } from '@/hooks/useAuthGuard';
import { userPageMeta } from './page-meta';
import { useDocumentMeta } from './useDocumentMeta';

export function UserLayout() {
  const user = useAuthGuard('user');
  const location = useLocation();
  const meta = userPageMeta(location.pathname);
  useDocumentMeta(meta.title);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    window.history.scrollRestoration = 'manual';

    const resetScroll = () => {
      window.scrollTo({ top: 0, left: 0, behavior: 'auto' });
      document.documentElement.scrollTop = 0;
      document.body.scrollTop = 0;
      document.querySelector('main')?.scrollTo({ top: 0, left: 0, behavior: 'auto' });
    };

    resetScroll();
    const frame = window.requestAnimationFrame(resetScroll);

    return () => window.cancelAnimationFrame(frame);
  }, [location.pathname]);

  if (!user) return null;

  return (
    <div className="relative flex min-h-screen w-full bg-background">
      {/* Background Layer */}
      <div className="mesh-gradient" />

      {/* Detached Floating Sidebar */}
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-[280px] p-4 lg:block">
        <div className="h-full rounded-xl border bg-sidebar/50 shadow-2xl shadow-black/5 backdrop-blur-xl">
          <Sidebar groups={userNav} />
        </div>
      </aside>

      <div className="flex min-h-screen w-full flex-col lg:pl-[280px]">
        <div className="sticky top-0 z-40 px-4 pt-4 lg:px-8">
          <div className="rounded-lg border bg-background/60 shadow-lg shadow-black/[0.02] backdrop-blur-xl">
            <Header title={meta.title} subtitle={meta.subtitle} nav={userNav} scope="user" />
          </div>
        </div>

        <main className="relative flex-1">
          <AnimatePresence mode="wait">
            <motion.div
              key={location.pathname}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -4 }}
              transition={{ duration: 0.24, ease: [0.22, 1, 0.36, 1] }}
              className="page-shell"
            >
              <Outlet />
            </motion.div>
          </AnimatePresence>
        </main>
      </div>
    </div>
  );
}
