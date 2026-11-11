import { Box, outlineButton, RowText, rowClass, solidButton } from '@/components/settings-ui';
import { ErrorNotice } from '@/components/error-notice';
import { useAppUpdate, useInstallUpdate } from '@/lib/app-update';

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
          <ErrorNotice
            title="Couldn't update. Try again."
            error={install.error}
            className="py-3 pl-3.5 pr-3"
          />
        ) : (
          check.isError && (
            <ErrorNotice
              title="Couldn't check for updates. Try again."
              error={check.error}
              className="py-3 pl-3.5 pr-3"
            />
          )
        )}
      </Box>
    </div>
  );
}
