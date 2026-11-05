import { useState, type FormEvent, type ReactNode } from 'react';
import { Creature, type CreatureLook } from '@/components/creature';
import { solidButton } from '@/components/settings-ui';

const floaters: { look: CreatureLook; size: number; place: string; drift: string }[] = [
  {
    look: 'shark',
    size: 150,
    place: '-left-10 top-[4%] opacity-15',
    drift: 'motion-safe:animate-drift-a',
  },
  {
    look: 'penguin',
    size: 44,
    place: 'left-[23%] top-[34%] opacity-40',
    drift: 'motion-safe:animate-drift-c',
  },
  {
    look: 'whale',
    size: 78,
    place: 'left-[9%] top-[70%] opacity-25',
    drift: 'motion-safe:animate-drift-b',
  },
  {
    look: 'seahorse',
    size: 64,
    place: 'left-[72%] top-[9%] opacity-30',
    drift: 'motion-safe:animate-drift-c [animation-direction:reverse]',
  },
  {
    look: 'jellyfish',
    size: 180,
    place: '-right-15 top-[34%] opacity-10',
    drift: 'motion-safe:animate-drift-b',
  },
  {
    look: 'shark',
    size: 96,
    place: 'left-[83%] top-[76%] opacity-20',
    drift: 'motion-safe:animate-drift-a',
  },
];

const step_ = 'flex flex-col items-center gap-5.5 text-center motion-safe:animate-rise';
const back = 'h-9 rounded-full px-3.5 text-[13px] text-muted';

export function FirstRun({
  initialName,
  providers,
  onSave,
  onOpen,
}: {
  initialName: string;
  providers: ReactNode;
  onSave: (name: string) => Promise<void>;
  onOpen: (text?: string) => void;
}) {
  const [step, setStep] = useState(1);
  const [name, setName] = useState(initialName);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const asks = [
    'Make a reviewer Bot for my repo',
    'Add ~/code/atoll as a repo',
    'Connect GitLab so Bots can open MRs',
  ];
  function saveName(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    void onSave(name.trim())
      .then(() => setStep(2))
      .catch((err: Error) => setError(err.message))
      .finally(() => setSaving(false));
  }
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden px-4 py-12">
      <div aria-hidden="true" className="pointer-events-none">
        {floaters.map((f) => (
          <span key={`${f.look}${f.size}`} className={`absolute ${f.place} ${f.drift}`}>
            <Creature look={f.look} size={f.size} />
          </span>
        ))}
      </div>
      <div className="relative flex w-full max-w-135 flex-col gap-7">
        <div role="list" aria-label="Steps" className="flex justify-center gap-2">
          {[1, 2, 3].map((n) => (
            <span
              key={n}
              role="listitem"
              aria-label={`Step ${n}${n === step ? ', current' : ''}`}
              className={`h-1.5 rounded-full motion-safe:transition-[width] motion-safe:duration-250 ${n === step ? 'w-7' : 'w-1.5'} ${n <= step ? 'bg-accent' : 'bg-outline'}`}
            />
          ))}
        </div>
        {step === 1 && (
          <form onSubmit={saveName} className={step_}>
            <Creature size={84} />
            <div>
              <h1 className="text-[40px] font-normal tracking-tighter">Welcome to Reef</h1>
              <p className="mt-2.5 text-base text-muted">
                Bots that answer, explore, and ship code for you.
              </p>
            </div>
            <label className="flex w-full max-w-100 flex-col gap-2 text-left">
              <span className="text-[13px] text-muted">What should your Bots call you?</span>
              <input
                className="h-13 rounded-[18px] border border-outline bg-box px-4.5 text-[17px] text-ink outline-none focus:border-muted"
                placeholder="Your name"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <button
              type="submit"
              disabled={!name.trim() || saving}
              className={`${solidButton} h-11 px-7 text-[14.5px]`}
            >
              Continue
            </button>
            {error && <p className="text-danger">{error}</p>}
          </form>
        )}
        {step === 2 && (
          <div className="flex flex-col gap-4.5 motion-safe:animate-rise">
            <div className="text-center">
              <h1 className="text-3xl font-normal tracking-tight">
                Hi {name}. Let’s find your agents.
              </h1>
              <p className="mt-2 text-muted">
                Your Bots run on these. Sign in to at least one. Reef keeps its own sign-in, apart
                from yours.
              </p>
            </div>
            {providers}
            <div className="mt-1 flex items-center justify-between">
              <button type="button" className={back} onClick={() => setStep(1)}>
                Back
              </button>
              <button
                type="button"
                className={`${solidButton} h-10 px-5.5`}
                onClick={() => setStep(3)}
              >
                Continue
              </button>
            </div>
          </div>
        )}
        {step === 3 && (
          <div className={step_}>
            <Creature size={120} />
            <div>
              <h1 className="text-[32px] font-normal tracking-tight">Meet Chief</h1>
              <p className="mx-auto mt-2 max-w-105 text-muted">
                Chief runs the place. Tell it what you need and it sets Reef up for you. Every
                change shows in the chat with Undo.
              </p>
            </div>
            <div
              role="group"
              aria-label="Example asks"
              className="flex w-full flex-col gap-2 text-left"
            >
              {asks.map((ask) => (
                <button
                  key={ask}
                  type="button"
                  className="flex items-center gap-3.5 rounded-[20px] border border-line bg-box px-4 py-3 text-left"
                  onClick={() => onOpen(ask)}
                >
                  <span className="flex-1">“{ask}”</span>
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="var(--color-muted)"
                    strokeWidth="2"
                    strokeLinecap="round"
                    aria-hidden="true"
                  >
                    <path d="M5 12h14M13 6l6 6-6 6" />
                  </svg>
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <button type="button" className={back} onClick={() => setStep(2)}>
                Back
              </button>
              <button
                type="button"
                className={`${solidButton} h-11 px-6.5 text-[14.5px]`}
                onClick={() => onOpen()}
              >
                Open Chief
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
