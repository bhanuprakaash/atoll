import { useState } from 'react';
import { NavLink, useNavigate } from 'react-router';
import { Creature } from '@/components/creature';
import { Menu } from '@/components/menu';
import { useBots, usePinBot, type Bot } from '@/modules/bots/api/bots';
import { useCreateGroup, useGroups, useSetBotGroup, type Group } from '@/modules/bots/api/groups';
import { BotGroup, botDragType } from '@/modules/bots/components/bot-group';
import { GroupNameField } from '@/modules/bots/components/group-name-field';
import { ThreadTree, Wave } from '@/modules/bots/components/thread-tree';
import { threadTree } from '@/modules/bots/utils/thread-tree';
import { relativeTime } from '@/utils/time';

function PinButton({ bot }: { bot: Bot }) {
  const pin = usePinBot(bot.id);
  const label = `${bot.pinned ? 'Unpin' : 'Pin'} ${bot.name}`;
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={(e) => {
        e.preventDefault();
        pin.mutate(!bot.pinned);
      }}
      className={`flex size-5 flex-none items-center justify-center rounded text-muted hover:text-ink focus-visible:opacity-100 group-hover:opacity-100 ${bot.pinned ? '' : 'opacity-0'}`}
    >
      <svg
        aria-hidden="true"
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill={bot.pinned ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M9 4h6l-1 6 3 3H7l3-3-1-6zM12 13v7" />
      </svg>
    </button>
  );
}

function BotRow({
  bot,
  open,
  groups,
  onMoveToGroup,
  onNewGroup,
  unfolded,
  onUnfold,
  onMore,
  onCloseThread,
  onRenameThread,
}: {
  bot: Bot;
  open: { botId?: string; threadId?: string };
  groups: Group[];
  onMoveToGroup: (groupId: string | null) => void;
  onNewGroup: () => void;
  unfolded: boolean;
  onUnfold: (unfolded: boolean) => void;
  onMore: (botId: string) => void;
  onCloseThread: (threadId: string) => void;
  onRenameThread: (threadId: string, title: string) => void;
}) {
  const navigate = useNavigate();
  const pin = usePinBot(bot.id);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const newThread = () => navigate(`/bots/${bot.id}/new`);
  return (
    <li>
      <div
        draggable={bot.id !== 'chief'}
        onDragStart={(event) => event.dataTransfer.setData(botDragType, bot.id)}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenu({ x: event.clientX, y: event.clientY });
        }}
        className={`group flex items-center rounded-card ${open.botId === bot.id ? 'bg-selected' : ''}`}
      >
        <NavLink
          to={`/bots/${bot.id}`}
          className="flex min-w-0 flex-1 items-center gap-3 px-2.5 py-[9px]"
        >
          <Creature look={bot.look} state={bot.state} size={36} />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="flex justify-between gap-2 text-[15px]">
              <span className="truncate">{bot.name}</span>
              <span className="flex flex-none items-center gap-1.5">
                <span className="text-xs text-muted">{relativeTime(bot.last_activity)}</span>
                {bot.id !== 'chief' && <PinButton bot={bot} />}
              </span>
            </span>
            {bot.state === 'working' && (
              <span className="flex items-center gap-1.5 text-[13px] text-muted">
                <span className="truncate">{bot.activity}</span>
                <Wave />
              </span>
            )}
            {bot.state === 'needs_you' && (
              <span className="truncate text-[13px] text-needs-ink">{bot.activity}</span>
            )}
          </span>
        </NavLink>
        <button
          type="button"
          aria-label={`New thread with ${bot.name}`}
          title="New thread (⌘N)"
          onClick={newThread}
          className="mr-2.5 hidden size-7 flex-none items-center justify-center rounded-control bg-tile text-ink group-focus-within:flex group-hover:flex"
        >
          <svg
            aria-hidden="true"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
          >
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>
      </div>
      <ThreadTree
        bot={bot}
        selected={open.botId === bot.id}
        openThreadId={open.botId === bot.id ? open.threadId : undefined}
        unfolded={unfolded}
        onUnfold={onUnfold}
        onMore={onMore}
        onCloseThread={onCloseThread}
        onRenameThread={onRenameThread}
      />
      {menu && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'New thread', onSelect: newThread },
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
                        onSelect: () => onMoveToGroup(group.id),
                      })),
                      { label: 'No group', onSelect: () => onMoveToGroup(null) },
                      { label: 'New group…', onSelect: onNewGroup },
                    ],
                  },
                ]),
            { label: 'Bot settings', onSelect: () => navigate(`/bots/${bot.id}?settings`) },
          ]}
        />
      )}
    </li>
  );
}

export function BotList({
  search,
  open,
  creatingGroup,
  onCreatingGroup,
  onMore,
  onCloseThread,
  onRenameThread,
}: {
  search: string;
  open: { botId?: string; threadId?: string };
  creatingGroup: { botId?: string } | null;
  onCreatingGroup: (creating: { botId?: string } | null) => void;
  onMore: (botId: string) => void;
  onCloseThread: (threadId: string) => void;
  onRenameThread: (threadId: string, title: string) => void;
}) {
  const { data: bots = [] } = useBots();
  const { data: groups = [] } = useGroups();
  const createGroup = useCreateGroup();
  const setGroup = useSetBotGroup();
  const [unfolded, setUnfolded] = useState<string | null>(null);
  const [openBot, setOpenBot] = useState(open.botId);
  if (openBot !== open.botId) {
    setOpenBot(open.botId);
    setUnfolded(null);
  }
  const unfoldedBot = bots.find((bot) => bot.id === unfolded);
  if (
    unfoldedBot &&
    threadTree(
      unfoldedBot.threads ?? [],
      open.botId === unfoldedBot.id ? open.threadId : undefined,
      false,
    ).more?.kind !== 'running'
  )
    setUnfolded(null);
  const matching = bots
    .filter((bot) => bot.name.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => Number(b.id === 'chief') - Number(a.id === 'chief'));
  const top = matching.filter((bot) => bot.id === 'chief' || bot.pinned);
  const rest = matching.filter((bot) => !top.includes(bot));
  const inGroup = (id: string) => rest.filter((bot) => bot.group_id === id);
  const ungrouped = rest.filter((bot) => !groups.some((group) => group.id === bot.group_id));
  const row = (bot: Bot) => (
    <BotRow
      key={bot.id}
      bot={bot}
      open={open}
      groups={groups}
      onMoveToGroup={(groupId) => setGroup.mutate({ botId: bot.id, groupId })}
      onNewGroup={() => onCreatingGroup({ botId: bot.id })}
      unfolded={unfolded === bot.id}
      onUnfold={(on) => setUnfolded(on ? bot.id : null)}
      onMore={onMore}
      onCloseThread={onCloseThread}
      onRenameThread={onRenameThread}
    />
  );
  const needsYou = (members: Bot[]) =>
    members.some((bot) => bot.threads?.some((thread) => thread.state === 'needs_you'));
  const moveTo = (groupId: string | null) => (botId: string) => setGroup.mutate({ botId, groupId });
  return (
    <div className="flex flex-col gap-0.5">
      <ul className="flex flex-col gap-1">
        {search && matching.length === 0 && (
          <li className="px-2.5 py-2 text-sm text-muted">No Bots match</li>
        )}
        {top.map(row)}
      </ul>
      {groups.map((group, index) => {
        const members = inGroup(group.id);
        if (search && members.length === 0) return null;
        return (
          <BotGroup
            key={group.id}
            group={group}
            name={group.name}
            heading
            first={index === 0}
            last={index === groups.length - 1}
            needsYou={needsYou(members)}
            onDropBot={moveTo(group.id)}
          >
            {members.map(row)}
          </BotGroup>
        );
      })}
      {creatingGroup && (
        <GroupNameField
          onDone={(name) => {
            onCreatingGroup(null);
            if (!name) return;
            createGroup.mutate(name, {
              onSuccess: (group) => {
                if (creatingGroup.botId)
                  setGroup.mutate({ botId: creatingGroup.botId, groupId: group.id });
              },
            });
          }}
        />
      )}
      <BotGroup
        name="Ungrouped"
        heading={groups.length > 0}
        needsYou={false}
        onDropBot={moveTo(null)}
      >
        {ungrouped.map(row)}
      </BotGroup>
    </div>
  );
}
