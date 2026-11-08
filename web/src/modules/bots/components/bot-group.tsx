import { useState, type DragEvent, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Menu } from '@/components/menu';
import {
  useDeleteGroup,
  useMoveGroup,
  useUpdateGroup,
  type Group,
} from '@/modules/bots/api/groups';
import { GroupNameField } from '@/modules/bots/components/group-name-field';

export const botDragType = 'application/x-reef-bot';

export function BotGroup({
  group,
  name,
  heading,
  first,
  last,
  needsYou,
  onDropBot,
  children,
}: {
  group?: Group;
  name: string;
  heading: boolean;
  first?: boolean;
  last?: boolean;
  needsYou: boolean;
  onDropBot: (botId: string) => void;
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const update = useUpdateGroup();
  const move = useMoveGroup();
  const remove = useDeleteGroup();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [over, setOver] = useState(false);
  const folded = group?.folded ?? false;
  const drop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    const botId = event.dataTransfer.getData(botDragType);
    if (botId) onDropBot(botId);
  };
  return (
    <div
      role="group"
      aria-label={name}
      data-marker={folded && needsYou ? 'needs-you' : undefined}
      className={heading ? 'mt-3' : undefined}
    >
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
          <h3
            onDragOver={(event) => {
              event.preventDefault();
              setOver(true);
            }}
            onDragLeave={() => setOver(false)}
            onDrop={drop}
            className={`group/heading flex h-6.5 items-center rounded-lg pr-1 pl-2.5 text-xs font-normal text-label ${over ? 'bg-selected text-ink' : ''}`}
          >
            {group ? (
              <button
                type="button"
                aria-expanded={!folded}
                onClick={() => update.mutate({ id: group.id, folded: !folded })}
                className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
              >
                <span className="truncate">{name}</span>
                <svg
                  aria-hidden="true"
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  className={`flex-none opacity-0 group-hover/heading:opacity-100 ${folded ? '-rotate-90' : ''}`}
                >
                  <path d="M6 9l6 6 6-6" />
                </svg>
                {folded && needsYou && (
                  <span aria-hidden="true" className="size-1.5 flex-none rounded-full bg-danger" />
                )}
              </button>
            ) : (
              <span className="truncate">{name}</span>
            )}
            {group && (
              <button
                type="button"
                aria-label="Group actions"
                aria-haspopup="menu"
                onClick={(event) => {
                  const box = event.currentTarget.getBoundingClientRect();
                  setMenu({ x: box.left, y: box.bottom + 4 });
                }}
                className="flex size-5.5 flex-none items-center justify-center rounded-md text-ink opacity-0 group-hover/heading:opacity-100 hover:bg-tile focus-visible:opacity-100"
              >
                <svg
                  aria-hidden="true"
                  width="13"
                  height="13"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                >
                  <circle cx="5" cy="12" r="1.7" />
                  <circle cx="12" cy="12" r="1.7" />
                  <circle cx="19" cy="12" r="1.7" />
                </svg>
              </button>
            )}
          </h3>
        ))}
      {!folded && <ul className="flex flex-col gap-1">{children}</ul>}
      {menu && group && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Rename', onSelect: () => setRenaming(true) },
            {
              label: `New Bot in ${group.name}`,
              onSelect: () => navigate(`/bots/new?group=${group.id}`),
            },
            {
              label: 'Move up',
              disabled: first,
              onSelect: () => move.mutate({ id: group.id, up: true }),
            },
            {
              label: 'Move down',
              disabled: last,
              onSelect: () => move.mutate({ id: group.id, up: false }),
            },
            { label: 'Delete group', onSelect: () => remove.mutate(group.id) },
          ]}
        />
      )}
    </div>
  );
}
