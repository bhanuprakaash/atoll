// clockTime is the locale's clock time: "3:02 PM" or "15:02".
export function clockTime(value: string) {
  return new Date(value).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

// relativeTime is a list time: the clock today, the weekday this week, else the date.
export function relativeTime(value: string) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  const now = new Date();
  if (date.toDateString() === now.toDateString()) return clockTime(value);
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - now.getDay());
  if (date >= start) return date.toLocaleDateString([], { weekday: 'short' });
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

// age is how long ago value was: "now", "12m", "2h" or "3d".
export function age(value: string) {
  const minutes = Math.floor((Date.now() - Date.parse(value)) / 60_000);
  if (!(minutes >= 1)) return 'now';
  if (minutes < 60) return `${minutes}m`;
  if (minutes < 1440) return `${Math.floor(minutes / 60)}h`;
  return `${Math.floor(minutes / 1440)}d`;
}
