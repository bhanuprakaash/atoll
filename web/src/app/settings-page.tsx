import { NavLink, Outlet } from 'react-router';

export function SettingsPage() {
  return (
    <>
      <header className="flex gap-4 border-b border-line px-4 py-3">
        {[
          ['repos', 'Repos'],
          ['connections', 'Connections'],
        ].map(([path, label]) => (
          <NavLink
            key={path}
            to={`/settings/${path}`}
            className={({ isActive }) => `text-sm ${isActive ? 'font-medium' : 'text-muted'}`}
          >
            {label}
          </NavLink>
        ))}
      </header>
      <Outlet />
    </>
  );
}
