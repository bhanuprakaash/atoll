// relativeTime is a list time: the clock today, the weekday this week, else the date.
export function relativeTime(value: string) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  const now = new Date();
  if (date.toDateString() === now.toDateString())
    return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - now.getDay());
  if (date >= start) return date.toLocaleDateString([], { weekday: 'short' });
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
}
