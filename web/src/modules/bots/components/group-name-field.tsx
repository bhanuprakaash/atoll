import { TextField } from '@/components/text-field';

export function GroupNameField({
  name = '',
  onDone,
}: {
  name?: string;
  onDone: (name?: string) => void;
}) {
  return (
    <TextField
      size="bare"
      autoFocus
      aria-label="Group name"
      placeholder="Group name"
      defaultValue={name}
      onBlur={() => onDone()}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onDone();
        if (event.key === 'Enter') onDone(event.currentTarget.value.trim());
      }}
      className="mt-3 h-7 w-full rounded-lg border border-outline bg-field px-2 text-[13px]"
    />
  );
}
