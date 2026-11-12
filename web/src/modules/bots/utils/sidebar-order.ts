import type { Bot } from '@/modules/bots/api/bots';
import type { Group } from '@/modules/bots/api/groups';

// The sidebar's Bots: Chief and pinned ones on top, then each group's, then the ungrouped.
export function sidebarOrder(bots: Bot[], groups: Group[]) {
  const sorted = [...bots].sort((a, b) => Number(b.id === 'chief') - Number(a.id === 'chief'));
  const top = sorted.filter((bot) => bot.id === 'chief' || bot.pinned);
  const rest = sorted.filter((bot) => !top.includes(bot));
  const inGroup = (id: string) => rest.filter((bot) => bot.group_id === id);
  const ungrouped = rest.filter((bot) => !groups.some((group) => group.id === bot.group_id));
  return { top, inGroup, ungrouped };
}
