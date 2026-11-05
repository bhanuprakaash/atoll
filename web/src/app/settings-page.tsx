import { NavLink, Outlet } from 'react-router';

export function SettingsPage() {
  return (
    <div className="flex min-h-0 flex-1">
      <nav
        aria-label="Settings"
        className="flex w-48 shrink-0 flex-col gap-1 border-r border-line p-4"
      >
        <h1 className="px-3 py-2 text-lg font-medium">Settings</h1>
        {[
          ['general', 'General'],
          ['providers', 'Providers'],
          ['connections', 'Connections'],
          ['skills', 'Skills'],
          ['repos', 'Repos'],
          ['memory', 'Memory'],
        ].map(([path, label]) => (
          <NavLink
            key={path}
            to={`/settings/${path}`}
            className={({ isActive }) =>
              `rounded-control px-3 py-2 text-sm ${isActive ? 'bg-composer text-ink' : 'text-muted'}`
            }
          >
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="min-w-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  );
}
