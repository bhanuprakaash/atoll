import { useState, type ReactNode } from 'react';
import { Button } from '@/components/button';
import { Creature } from '@/components/creature';
import { controlClass, Field } from '@/components/field';

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
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden p-8">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 flex items-center justify-around opacity-30"
      >
        <Creature look="whale" size={100} />
        <Creature look="turtle" size={80} />
        <Creature look="jellyfish" size={110} />
      </div>
      <div className="relative flex w-full max-w-xl flex-col gap-6 rounded-card border border-line bg-card p-8">
        {step === 1 && (
          <>
            <h1 className="text-3xl">Welcome to Reef</h1>
            <Field label="What should your Bots call you?">
              {(id) => (
                <input
                  id={id}
                  className={controlClass}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              )}
            </Field>
            <Button
              disabled={!name.trim() || saving}
              onClick={() => {
                setSaving(true);
                void onSave(name.trim())
                  .then(() => setStep(2))
                  .catch((e: Error) => setError(e.message))
                  .finally(() => setSaving(false));
              }}
            >
              Continue
            </Button>
            {error && <p className="text-danger">{error}</p>}
          </>
        )}
        {step === 2 && (
          <>
            <h1 className="text-3xl">Hi {name}. Let’s find your agents.</h1>
            {providers}
            <Button onClick={() => setStep(3)}>Continue</Button>
          </>
        )}
        {step === 3 && (
          <>
            <Creature size={80} />
            <h1 className="text-3xl">Meet Chief</h1>
            <div role="group" aria-label="Example asks" className="flex flex-col gap-2">
              {asks.map((ask) => (
                <button
                  key={ask}
                  className="rounded-control border border-line p-3 text-left"
                  onClick={() => onOpen(ask)}
                >
                  {ask}
                </button>
              ))}
            </div>
            <Button onClick={() => onOpen()}>Open Chief</Button>
          </>
        )}
      </div>
    </div>
  );
}
