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
      width="w-17"
      autoFocus
      aria-label="Group name"
      placeholder="Group name"
      defaultValue={name}
      onBlur={() => onDone()}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onDone();
        if (event.key === 'Enter') onDone(event.currentTarget.value.trim());
      }}
      className="mx-auto my-1 h-7 rounded-lg border border-outline bg-field px-1.5 text-xs"
    />
  );
}
