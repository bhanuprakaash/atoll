import { useNavigate } from 'react-router';
import { Menu } from '@/components/menu';
import { usePinBot, type Bot } from '@/modules/bots/api/bots';
import { useGroups, useSetBotGroup } from '@/modules/bots/api/groups';

export function BotMenu({
  bot,
  at,
  onClose,
  onNewGroup,
}: {
  bot: Bot;
  at: { x: number; y: number };
  onClose: () => void;
  onNewGroup: () => void;
}) {
  const navigate = useNavigate();
  const pin = usePinBot(bot.id);
  const { data: groups = [] } = useGroups();
  const setGroup = useSetBotGroup();
  const moveTo = (groupId: string | null) => setGroup.mutate({ botId: bot.id, groupId });
  return (
    <Menu
      at={at}
      onClose={onClose}
      items={[
        { label: 'New thread', onSelect: () => navigate(`/bots/${bot.id}/new`) },
        ...(bot.id === 'chief'
          ? []
          : [
              {
                label: bot.pinned ? 'Unpin' : 'Pin to sidebar',
                onSelect: () => pin.mutate(!bot.pinned),
              },
              {
                label: 'Move to group',
                items: [
                  ...groups.map((group) => ({
                    label: group.name,
                    onSelect: () => moveTo(group.id),
                  })),
                  { label: 'No group', onSelect: () => moveTo(null) },
                  { label: 'New group…', onSelect: onNewGroup },
                ],
              },
            ]),
        { label: 'Bot settings', onSelect: () => navigate(`/bots/${bot.id}?settings`) },
      ]}
    />
  );
}
