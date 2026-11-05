// Builds the real atoll and the fake providers once, then serves the shared machine the Slice 2–5
// specs use: fake claude signed in, user name set so first run is done. Specs that need a machine
// of their own start one with startAtoll (atoll.ts).
import { rmSync } from 'node:fs';
import { build, startAtoll } from './atoll';

export default async function setup() {
  const out = build();
  process.env.ATOLL_E2E_OUT = out;
  const m = await startAtoll();
  process.env.ATOLL_URL = m.url;
  return () => {
    m.stop();
    rmSync(out, { recursive: true, force: true });
  };
}
