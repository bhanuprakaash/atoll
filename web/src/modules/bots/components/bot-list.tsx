import { useState, type DragEvent, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { Creature } from '@/components/creature';
import { Menu } from '@/components/menu';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import {
  useCreateGroup,
  useDeleteGroup,
  useGroups,
  useMoveGroup,
  usePlaceBot,
  useSetBotGroup,
  useUpdateGroup,
  type Group,
} from '@/modules/bots/api/groups';
import { BotMenu } from '@/modules/bots/components/bot-menu';
import { GroupNameField } from '@/modules/bots/components/group-name-field';
import { Dot, outUntil, Wave } from '@/modules/bots/components/thread-tree';
import { sidebarOrder } from '@/modules/bots/utils/sidebar-order';
import { nowSections, workersByBoss } from '@/modules/bots/utils/thread-tree';
import { relativeTime } from '@/utils/time';

const botDragType = 'application/x-reef-bot';
const groupDragType = 'application/x-reef-group';

const rowStyle = 'relative flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 outline-none';

function NowRow({ current, count }: { current: boolean; count: number }) {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      aria-label={count > 0 ? `Now, ${count} waiting` : 'Now'}
      aria-current={current ? 'page' : undefined}
      onClick={() => navigate('/now')}
      className={`${rowStyle} text-left text-[15px] ${current ? 'bg-selected' : 'hover:bg-field'}`}
    >
      {count > 0 ? (
        <span
          aria-hidden="true"
          className="flex size-7 flex-none items-center justify-center rounded-control bg-accent text-sm font-medium text-accent-ink"
        >
          {count}
        </span>
      ) : (
        <span
          aria-hidden="true"
          className="flex size-7 flex-none items-center justify-center rounded-control bg-field text-muted"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          >
            <circle cx="12" cy="12" r="8" />
            <circle cx="12" cy="12" r="2" fill="currentColor" />
          </svg>
        </span>
      )}
      Now
    </button>
  );
}

function BotNote({ bot }: { bot: Bot }) {
  if (bot.state === 'needs_you')
    return (
      <span className="flex items-center gap-1.5 text-[13px] text-ink">
        <Dot className="bg-accent" />
        <span className="min-w-0 truncate">{bot.activity || 'Needs you'}</span>
      </span>
    );
  if (bot.state === 'failed')
    return (
      <span className="flex items-center gap-1.5 text-[13px] text-muted">
        <Dot className="bg-danger" />
        <span className="min-w-0 truncate">Run failed</span>
      </span>
    );
  if (bot.state === 'working')
    return (
      <span className="flex items-center gap-1.5 text-[13px] text-muted">
        <span className="min-w-0 truncate">{bot.activity || 'Working'}</span>
        <Wave />
      </span>
    );
  const limited = bot.threads?.find((t) => t.state === 'limited');
  const unread = bot.threads?.some((t) => t.state === 'new');
  return (
    <span className={`block truncate text-[13px] ${unread ? 'text-ink' : 'text-muted'}`}>
      {limited
        ? outUntil(limited)
        : bot.activity || (bot.last_activity ? 'No reply' : 'No messages yet')}
    </span>
  );
}

function BotRow({
  bot,
  current,
  onNewGroup,
  onDropBot,
}: {
  bot: Bot;
  current: boolean;
  onNewGroup: () => void;
  onDropBot: (botId: string, after: boolean) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [dropAt, setDropAt] = useState<'before' | 'after' | null>(null);
  const after = (event: DragEvent) => {
    const box = event.currentTarget.getBoundingClientRect();
    return bot.id === 'chief' || event.clientY >= box.top + box.height / 2;
  };
  return (
    <>
      <Link
        to={`/bots/${bot.id}`}
        aria-current={current ? 'page' : undefined}
        aria-label={bot.name}
        draggable={bot.id !== 'chief'}
        onDragStart={(event) => {
          event.dataTransfer.setData(botDragType, bot.id);
        }}
        onDragOver={(event) => {
          if (!event.dataTransfer.types.includes(botDragType)) return;
          event.preventDefault();
          setDropAt(after(event) ? 'after' : 'before');
        }}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropAt(null);
        }}
        onDrop={(event) => {
          event.preventDefault();
          const botId = event.dataTransfer.getData(botDragType);
          if (botId) onDropBot(botId, after(event));
          setDropAt(null);
        }}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenu({ x: event.clientX, y: event.clientY });
        }}
        className={`${rowStyle} ${current ? 'bg-selected' : 'hover:bg-field'}`}
      >
        {dropAt && (
          <span
            aria-hidden="true"
            className={`absolute inset-x-2 h-0.5 rounded-full bg-ink ${dropAt === 'before' ? 'top-0' : 'bottom-0'}`}
          />
        )}
        <Creature look={bot.look} colour={bot.colour} state={bot.state} size={36} />
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate text-[15px]">{bot.name}</span>
            {bot.last_activity && (
              <time dateTime={bot.last_activity} className="flex-none text-xs text-label">
                {relativeTime(bot.last_activity)}
              </time>
            )}
          </span>
          <BotNote bot={bot} />
        </span>
      </Link>
      {menu && (
        <BotMenu bot={bot} at={menu} onClose={() => setMenu(null)} onNewGroup={onNewGroup} />
      )}
    </>
  );
}

function BotGroup({
  group,
  name,
  heading,
  onDropBot,
  onDropGroup,
  children,
}: {
  group?: Group;
  name: string;
  heading: boolean;
  onDropBot: (botId: string) => void;
  onDropGroup?: (groupId: string) => void;
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const update = useUpdateGroup();
  const remove = useDeleteGroup();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [over, setOver] = useState(false);
  const accepts = (event: DragEvent) =>
    event.dataTransfer.types.includes(botDragType) ||
    (!!group && event.dataTransfer.types.includes(groupDragType));
  const drop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    const botId = event.dataTransfer.getData(botDragType);
    const groupId = event.dataTransfer.getData(groupDragType);
    if (botId) onDropBot(botId);
    else if (groupId) onDropGroup?.(groupId);
  };
  return (
    <div role="group" aria-label={name} className="flex flex-col">
      {heading &&
        (renaming && group ? (
          <GroupNameField
            name={group.name}
            onDone={(next) => {
              setRenaming(false);
              if (next && next !== group.name) update.mutate({ id: group.id, name: next });
            }}
          />
        ) : (
          <button
            type="button"
            aria-label={name}
            aria-haspopup={group ? 'menu' : undefined}
            onClick={(event) => {
              if (!group) return;
              const box = event.currentTarget.getBoundingClientRect();
              setMenu({ x: box.right + 4, y: box.top });
            }}
            onContextMenu={(event) => {
              if (!group) return;
              event.preventDefault();
              setMenu({ x: event.clientX, y: event.clientY });
            }}
            draggable={!!group}
            onDragStart={(event) => {
              event.dataTransfer.setData(groupDragType, group?.id ?? '');
            }}
            onDragOver={(event) => {
              if (!accepts(event)) return;
              event.preventDefault();
              setOver(true);
            }}
            onDragLeave={() => setOver(false)}
            onDrop={drop}
            className={`mt-3 flex h-6 w-full items-center px-2 text-left text-xs outline-none focus-visible:text-ink ${over ? 'text-ink' : 'text-label'}`}
          >
            <span className="truncate">{name}</span>
          </button>
        ))}
      {children}
      {menu && group && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Rename', onSelect: () => setRenaming(true) },
            {
              label: 'New Bot in this group',
              onSelect: () => navigate(`/bots/new?group=${group.id}`),
            },
            { label: 'Delete group', onSelect: () => remove.mutate(group.id) },
          ]}
        />
      )}
    </div>
  );
}

export function BotList({
  now,
  openBotId,
  creatingGroup,
  onCreatingGroup,
}: {
  now: boolean;
  openBotId?: string;
  creatingGroup: { botId?: string } | null;
  onCreatingGroup: (creating: { botId?: string } | null) => void;
}) {
  const { data: bots = [] } = useBots();
  const { data: groups = [] } = useGroups();
  const createGroup = useCreateGroup();
  const setGroup = useSetBotGroup();
  const place = usePlaceBot();
  const moveGroup = useMoveGroup();
  const { top, inGroup, ungrouped } = sidebarOrder(bots, groups);
  const waiting =
    nowSections(
      bots.flatMap((bot) => bot.threads ?? []),
      undefined,
      workersByBoss(bots),
    ).find((section) => section.label === 'Needs you')?.threads.length ?? 0;
  const dropOnRow = (target: Bot) => (botId: string, after: boolean) => {
    const dragged = bots.find((bot) => bot.id === botId);
    if (!dragged || dragged === target) return;
    const pinned = target.id === 'chief' || target.pinned;
    const groupId = pinned
      ? dragged.group_id || null
      : groups.some((group) => group.id === target.group_id)
        ? target.group_id
        : null;
    const section = pinned
      ? top.filter((bot) => bot.id !== 'chief')
      : groupId
        ? inGroup(groupId)
        : ungrouped;
    const others = section.filter((bot) => bot !== dragged);
    const before =
      target.id === 'chief' ? others[0] : after ? others[others.indexOf(target) + 1] : target;
    place.mutate({ botId, pinned, groupId, before: before?.id ?? null });
  };
  const row = (bot: Bot) => (
    <BotRow
      key={bot.id}
      bot={bot}
      current={!now && openBotId === bot.id}
      onNewGroup={() => onCreatingGroup({ botId: bot.id })}
      onDropBot={dropOnRow(bot)}
    />
  );
  const moveTo = (groupId: string | null) => (botId: string) =>
    place.mutate({ botId, pinned: false, groupId, before: null });
  const dropOnGroup = (target: Group) => (id: string) => {
    if (id !== target.id) moveGroup.mutate({ id, before: target.id });
  };
  return (
    <>
      <div className="flex-none px-2">
        <NowRow current={now} count={waiting} />
        <div aria-hidden="true" className="mx-2 my-1.5 h-px bg-line" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2 [scrollbar-width:thin]">
        <div className="flex flex-col">
          <div className="flex flex-col">{top.map(row)}</div>
          {groups.map((group) => (
            <BotGroup
              key={group.id}
              group={group}
              name={group.name}
              heading
              onDropBot={moveTo(group.id)}
              onDropGroup={dropOnGroup(group)}
            >
              {inGroup(group.id).map(row)}
            </BotGroup>
          ))}
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
          {ungrouped.length > 0 && (
            <BotGroup name="Ungrouped" heading={groups.length > 0} onDropBot={moveTo(null)}>
              {ungrouped.map(row)}
            </BotGroup>
          )}
        </div>
      </div>
    </>
  );
}
