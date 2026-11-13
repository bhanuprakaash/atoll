// cost is a price at the cent, or "<$0.01" under a cent.
export function cost(usd: number) {
  return usd < 0.01 ? '<$0.01' : `$${usd.toFixed(2)}`;
}

// modelName shows a Claude id as its name and version (claude-sonnet-5-5 is "Sonnet 5.5"); any
// other id as it is.
function modelName(model: string) {
  const claude = /^claude-(opus|sonnet|haiku)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?$/.exec(model);
  if (!claude) return model;
  const [, name, major, minor] = claude;
  return `${name[0].toUpperCase()}${name.slice(1)} ${major}${minor ? `.${minor}` : ''}`;
}

export type RunUsage = { model: string; calls: number; cost_usd?: number };

// usageLine is the end of a run's footer: "Sonnet 5.5, 7 calls". Cost is not shown until the analytics page.
export function usageLine({ model, calls }: RunUsage) {
  return `${modelName(model)}, ${calls} ${calls === 1 ? 'call' : 'calls'}`;
}
