import { Box, outlineButton, RowText, rowClass, solidButton } from '@/components/settings-ui';
import { quiet } from '@/components/button';
import { useAppUpdate, useInstallUpdate } from '@/lib/app-update';

function Failure({ text, message }: { text: string; message: string }) {
  return (
    <div className="py-3 pl-3.5 pr-3 text-[13px]">
      <p className="text-danger">{text}</p>
      <details className="mt-1">
        <summary className={`cursor-pointer ${quiet}`}>Details</summary>
        <p className="mt-1 break-words text-muted">{message}</p>
      </details>
    </div>
  );
}

export function SoftwareUpdate() {
  const check = useAppUpdate();
  const install = useInstallUpdate();
  const update = check.data;
  const status =
    update && (update.available ? `Reef ${update.available} is available` : 'Up to date');
  return (
    <div role="group" aria-label="Software update">
      <Box>
        <div className={rowClass}>
          <RowText title={update ? `Reef ${update.current}` : 'Reef'} detail={status} />
          <button
            type="button"
            disabled={check.isFetching}
            onClick={() => {
              install.reset();
              void check.refetch();
            }}
            className={outlineButton}
          >
            Check for updates
          </button>
          {update?.available && (
            <button
              type="button"
              disabled={install.isPending}
              onClick={() => install.mutate()}
              className={solidButton}
            >
              {install.isPending ? 'Updating…' : 'Update and restart'}
            </button>
          )}
        </div>
        {install.isError ? (
          <Failure text="Couldn't update. Try again." message={install.error.message} />
        ) : (
          check.isError && (
            <Failure text="Couldn't check for updates. Try again." message={check.error.message} />
          )
        )}
      </Box>
    </div>
  );
}
