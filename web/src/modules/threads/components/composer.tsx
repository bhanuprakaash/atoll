import {
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type FormEvent,
  type ReactNode,
} from 'react';
import { TextArea } from '@/components/text-field';
import { ZoomImage } from '@/components/zoom-image';
import { useSendMessage, type ImageInput, type Run } from '@/modules/threads/api/messages';
import { useStopRun } from '@/modules/threads/api/runs';

const roundButton =
  'flex size-10 flex-none items-center justify-center rounded-full bg-accent text-accent-ink disabled:bg-segment disabled:text-muted';

const imageTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];
const maxImageBytes = 5 * 1024 * 1024;
const maxImages = 10;

type Attachment = ImageInput & { id: number; url: string };

const readImage = (file: File, id: number) =>
  new Promise<Attachment>((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error(`Could not read ${file.name}`));
    reader.onload = () => {
      const url = reader.result as string;
      resolve({ id, mime: file.type, data: url.slice(url.indexOf(',') + 1), url });
    };
    reader.readAsDataURL(file);
  });

export function Composer({
  threadId,
  botName,
  runId,
  waiting,
  disabled,
  onSent,
  context,
  onFirstSend,
}: {
  threadId: string;
  botName: string;
  runId?: string;
  waiting?: boolean;
  disabled: boolean;
  onSent: (run: Run) => void;
  context?: ReactNode;
  onFirstSend?: (text: string, images: ImageInput[]) => void;
}) {
  const [text, setText] = useState('');
  const [images, setImages] = useState<Attachment[]>([]);
  const [imageError, setImageError] = useState('');
  const nextImageId = useRef(0);
  const box = useRef<HTMLTextAreaElement>(null);
  const send = useSendMessage(threadId);
  const stop = useStopRun();
  const canSend = !disabled && !send.isPending && (text.trim() !== '' || images.length > 0);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 194)}px`;
  }, [text]);

  const addImages = async (files: File[]) => {
    if (files.length === 0) return;
    const bad = files.find((f) => !imageTypes.includes(f.type));
    if (bad)
      return setImageError(`${bad.name || 'That file'} is not a PNG, JPEG, GIF or WebP image`);
    if (files.some((f) => f.size > maxImageBytes))
      return setImageError('Images can be at most 5 MB each');
    if (images.length + files.length > maxImages)
      return setImageError(`A message can have at most ${maxImages} images`);
    try {
      const added = await Promise.all(files.map((f) => readImage(f, nextImageId.current++)));
      setImageError('');
      setImages((current) => [...current, ...added]);
    } catch (err) {
      setImageError((err as Error).message);
    }
  };
  const onPaste = (e: ClipboardEvent) => {
    const files = Array.from(e.clipboardData.files);
    if (files.length === 0) return;
    e.preventDefault();
    void addImages(files);
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    void addImages(Array.from(e.dataTransfer.files));
  };

  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    if (!canSend) return;
    if (onFirstSend) {
      onFirstSend(text, images);
      setText('');
      setImages([]);
      return;
    }
    send.mutate(
      { text, images },
      {
        onSuccess: ({ run }) => {
          setText('');
          setImages([]);
          if (run) onSent(run);
        },
      },
    );
  };

  const input = (
    <TextArea
      ref={box}
      variant="bare"
      aria-label={`Message ${botName}${runId ? ', run in progress' : ''}`}
      placeholder={
        runId
          ? `${botName} is working…`
          : onFirstSend
            ? `Ask ${botName} anything, or paste an issue or MR link`
            : `Message ${botName}`
      }
      rows={1}
      value={text}
      onChange={(e) => setText(e.target.value)}
      onPaste={onPaste}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
          e.preventDefault();
          submit();
        }
      }}
      className="block max-h-[194px] overflow-y-auto py-2.25 text-[15px] leading-[22px]"
    />
  );
  const action =
    runId && !(waiting && canSend) ? (
      <button
        type="button"
        aria-label="Stop"
        disabled={stop.isPending}
        onClick={() => stop.mutate(runId)}
        className={roundButton}
      >
        <svg aria-hidden="true" width="14" height="14" viewBox="0 0 14 14">
          <rect x="2" y="2" width="10" height="10" rx="2" fill="currentColor" />
        </svg>
      </button>
    ) : (
      <button type="submit" aria-label="Send" disabled={!canSend} className={roundButton}>
        <svg
          aria-hidden="true"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M12 19V5M5 12l7-7 7 7" />
        </svg>
      </button>
    );

  return (
    <div className="flex flex-col gap-2" onDragOver={(e) => e.preventDefault()} onDrop={onDrop}>
      {images.length > 0 && (
        <div className="flex flex-wrap gap-2 px-1.5">
          {images.map((img) => (
            <div key={img.id} data-testid="composer-attachment" className="relative">
              <ZoomImage
                src={img.url}
                alt="Attachment"
                className="size-16 rounded-xl object-cover"
              />
              <button
                type="button"
                aria-label="Remove image"
                onClick={() => setImages((current) => current.filter((i) => i.id !== img.id))}
                className="absolute -top-1.5 -right-1.5 flex size-5 items-center justify-center rounded-full bg-accent text-xs leading-none text-accent-ink"
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}
      <form
        onSubmit={submit}
        className={
          context
            ? 'flex flex-col gap-1.5 rounded-[26px] border border-composer-line bg-composer pt-1.5 pr-2.5 pb-2.5 pl-4.5'
            : `flex items-end gap-2.5 rounded-[28px] border border-composer-line bg-composer py-2 pr-2 pl-4.5 ${onFirstSend ? 'min-h-15' : 'min-h-14'}`
        }
      >
        {context ? (
          <>
            <div className="pr-2">{input}</div>
            <div className="flex items-center gap-1.5">
              <div
                role="group"
                aria-label="Thread context"
                className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden text-[12.5px] text-ink/80"
              >
                {context}
              </div>
              {action}
            </div>
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1 self-center">{input}</div>
            {action}
          </>
        )}
      </form>
      {imageError && (
        <p role="alert" className="text-[13px] text-danger">
          {imageError}
        </p>
      )}
      {(send.isError || stop.isError) && (
        <p className="text-[13px] text-danger">{send.error?.message || stop.error?.message}</p>
      )}
    </div>
  );
}
