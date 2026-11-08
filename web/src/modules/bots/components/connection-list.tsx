import { Fragment, type ReactNode } from 'react';
import { Link } from 'react-router';
import { ChoiceList } from '@/modules/bots/components/choice-list';

type Connection = {
  id: string;
  kind: 'gitlab' | 'github';
  instance_url: string;
  status: 'pending' | 'connected' | 'needs_sign_in';
};

export function ConnectionList({
  connections,
  selected,
  onChange,
  reconnect,
}: {
  connections: Connection[];
  selected: string[];
  onChange: (ids: string[]) => void;
  reconnect: (connection: Connection) => ReactNode;
}) {
  return (
    <>
      <ChoiceList
        title="Connections"
        singular="connection"
        choices={connections.map((c) => ({
          id: c.id,
          name: `${c.kind === 'github' ? 'GitHub' : 'GitLab'} · ${c.instance_url.replace(/^https?:\/\//, '')}`,
          brand: c.kind,
        }))}
        selected={selected}
        onChange={onChange}
        empty={
          <>
            No connections yet.{' '}
            <Link to="/settings/connections" className="text-ink underline underline-offset-2">
              Connect GitLab in Settings
            </Link>
          </>
        }
      />
      {connections
        .filter((c) => c.kind === 'gitlab' && selected.includes(c.id) && c.status !== 'connected')
        .map((c) => (
          <Fragment key={c.id}>{reconnect(c)}</Fragment>
        ))}
    </>
  );
}
