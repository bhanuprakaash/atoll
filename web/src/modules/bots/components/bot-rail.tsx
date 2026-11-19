import { useState, type DragEvent, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { Creature } from '@/components/creature';
import { Menu } from '@/components/menu';
import { Tooltip, useTooltip } from '@/components/tooltip';
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
import { sidebarOrder } from '@/modules/bots/utils/sidebar-order';

const botDragType = 'application/x-reef-bot';
const groupDragType = 'application/x-reef-group';

const stateLabel: Partial<Record<Bot['state'], string>> = {
  needs_you: 'Needs you',
  working: 'Working',
  failed: 'Run failed',
};

const faceStyle =
  'relative flex w-full flex-col items-center gap-1 px-1 py-2 text-[11px] text-muted outline-none hover:text-ink focus-visible:text-ink';

function Bar() {
  return (
    <span aria-hidden="true" className="absolute inset-y-2 left-0 w-[3px] rounded-r-full bg-ink" />
  );
}

function NowButton({
  current,
  count,
  onClick,
}: {
  current: boolean;
  count: number;
  onClick: (button: HTMLButtonElement) => void;
}) {
  return (
    <button
      type="button"
      data-current={current ? 'true' : undefined}
      onClick={(event) => onClick(event.currentTarget)}
      className={`${faceStyle} ${current ? 'text-ink' : ''}`}
    >
      {current && <Bar />}
      <span
        aria-hidden="true"
        className="flex size-8 items-center justify-center rounded-full bg-field"
      >
        <svg
          width="16"
          height="16"
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
      <span>Now</span>
      {count > 0 && (
        <span
          data-badge
          className="absolute top-1 right-2 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] text-accent-ink"
        >
          {count}
        </span>
      )}
    </button>
  );
}

function BotFace({
  bot,
  current,
  onSelect,
  onNewGroup,
  onDragging,
  onDropBot,
}: {
  bot: Bot;
  current: boolean;
  onSelect: (link: HTMLAnchorElement) => void;
  onNewGroup: () => void;
  onDragging: (dragging: boolean) => void;
  onDropBot: (botId: string, after: boolean) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [dropAt, setDropAt] = useState<'before' | 'after' | null>(null);
  const [anchor, tip] = useTooltip();
  const state = stateLabel[bot.state];
  const after = (event: DragEvent) => {
    const box = event.currentTarget.getBoundingClientRect();
    return bot.id === 'chief' || event.clientY >= box.top + box.height / 2;
  };
  return (
    <>
      <Link
        to={`/bots/${bot.id}`}
        data-current={current ? 'true' : undefined}
        data-marker={bot.state === 'needs_you' ? 'needs-you' : undefined}
        draggable={bot.id !== 'chief'}
        onDragStart={(event) => {
          event.dataTransfer.setData(botDragType, bot.id);
          onDragging(true);
        }}
        onDragEnd={() => onDragging(false)}
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
          onDragging(false);
        }}
        onClick={(event) => onSelect(event.currentTarget)}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenu({ x: event.clientX, y: event.clientY });
        }}
        className={`${faceStyle} ${current ? 'text-ink' : ''}`}
        {...tip}
      >
        {current && <Bar />}
        {dropAt && (
          <span
            aria-hidden="true"
            className={`absolute inset-x-2 h-0.5 rounded-full bg-ink ${dropAt === 'before' ? 'top-0' : 'bottom-0'}`}
          />
        )}
        <Creature look={bot.look} colour={bot.colour} state={bot.state} size={40} />
        <span className="max-w-16 truncate">{bot.name}</span>
      </Link>
      {anchor && (
        <Tooltip anchor={anchor}>
          <span>{bot.name}</span>
          {state && <span className="text-xs text-muted">{state}</span>}
        </Tooltip>
      )}
      {menu && (
        <BotMenu bot={bot} at={menu} onClose={() => setMenu(null)} onNewGroup={onNewGroup} />
      )}
    </>
  );
}

function BotGroup({
  group,
  name,
  count,
  line,
  dragging,
  onDragging,
  onDropBot,
  onDropGroup,
  children,
}: {
  group?: Group;
  name: string;
  count: number;
  line: boolean;
  dragging: boolean;
  onDragging: (dragging: boolean) => void;
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
  const [anchor, tip] = useTooltip();
  const accepts = (event: DragEvent) =>
    event.dataTransfer.types.includes(botDragType) ||
    (!!group && event.dataTransfer.types.includes(groupDragType));
  const drop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    onDragging(false);
    const botId = event.dataTransfer.getData(botDragType);
    const groupId = event.dataTransfer.getData(groupDragType);
    if (botId) onDropBot(botId);
    else if (groupId) onDropGroup?.(groupId);
  };
  return (
    <div role="group" aria-label={name} className="flex flex-col">
      {line &&
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
              onDragging(true);
            }}
            onDragEnd={() => onDragging(false)}
            onDragOver={(event) => {
              if (!accepts(event)) return;
              event.preventDefault();
              setOver(true);
            }}
            onDragLeave={() => setOver(false)}
            onDrop={drop}
            className={`group/line flex h-4 w-full items-center justify-center outline-none ${dragging ? 'relative z-10 after:absolute after:inset-x-0 after:-inset-y-1' : ''}`}
            {...tip}
          >
            <span
              className={`h-px transition-[width] motion-reduce:transition-none group-hover/line:w-14 group-hover/line:bg-muted group-focus-visible/line:w-14 group-focus-visible/line:bg-muted ${over ? 'w-14 bg-ink' : 'w-8 bg-sidebar-line'}`}
            />
          </button>
        ))}
      {children}
      {anchor && (
        <Tooltip anchor={anchor}>
          <span>
            {name} <span className="text-muted">· {count === 1 ? '1 Bot' : `${count} Bots`}</span>
          </span>
        </Tooltip>
      )}
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

export function BotRail({
  view,
  openBotId,
  onView,
  creatingGroup,
  onCreatingGroup,
}: {
  view?: string;
  openBotId?: string;
  onView: (view: string, from: HTMLElement) => void;
  creatingGroup: { botId?: string } | null;
  onCreatingGroup: (creating: { botId?: string } | null) => void;
}) {
  const { data: bots = [] } = useBots();
  const { data: groups = [] } = useGroups();
  const createGroup = useCreateGroup();
  const setGroup = useSetBotGroup();
  const place = usePlaceBot();
  const moveGroup = useMoveGroup();
  const [dragging, setDragging] = useState(false);
  const { top, inGroup, ungrouped } = sidebarOrder(bots, groups);
  const needsYou = bots.reduce(
    (n, bot) => n + (bot.threads ?? []).filter((t) => t.state === 'needs_you').length,
    0,
  );
  const dropOnFace = (target: Bot) => (botId: string, after: boolean) => {
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
  const face = (bot: Bot) => (
    <BotFace
      key={bot.id}
      bot={bot}
      current={view === bot.id || openBotId === bot.id}
      onSelect={(link) => onView(bot.id, link)}
      onNewGroup={() => onCreatingGroup({ botId: bot.id })}
      onDragging={setDragging}
      onDropBot={dropOnFace(bot)}
    />
  );
  const moveTo = (groupId: string | null) => (botId: string) =>
    place.mutate({ botId, pinned: false, groupId, before: null });
  const dropOnGroup = (target: Group) => (id: string) => {
    if (id !== target.id) moveGroup.mutate({ id, before: target.id });
  };
  return (
    <div className="flex flex-col">
      <NowButton
        current={view === 'now' && !openBotId}
        count={needsYou}
        onClick={(button) => onView('now', button)}
      />
      <div className="flex flex-col">{top.map(face)}</div>
      {groups.map((group) => {
        const members = inGroup(group.id);
        return (
          <BotGroup
            key={group.id}
            group={group}
            name={group.name}
            count={members.length}
            line
            dragging={dragging}
            onDragging={setDragging}
            onDropBot={moveTo(group.id)}
            onDropGroup={dropOnGroup(group)}
          >
            {members.map(face)}
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
        count={ungrouped.length}
        line={groups.length > 0}
        dragging={dragging}
        onDragging={setDragging}
        onDropBot={moveTo(null)}
      >
        {ungrouped.map(face)}
      </BotGroup>
    </div>
  );
}
