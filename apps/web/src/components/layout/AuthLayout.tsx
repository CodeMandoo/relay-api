import { Outlet } from 'react-router-dom';

export function AuthLayout() {
  return (
    <div className="relative flex min-h-screen w-full flex-col bg-background">
      <div className="pointer-events-none absolute inset-0 -z-10 bg-auth-spotlight" />
      <div className="pointer-events-none absolute inset-0 -z-10 dotted-grid opacity-[0.35] [mask-image:radial-gradient(ellipse_at_center,black_30%,transparent_75%)]" />
      <Outlet />
    </div>
  );
}
