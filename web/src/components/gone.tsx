import { Link } from 'react-router';

export function Gone({ title, to, link }: { title: string; to: string; link: string }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-7">
      <p className="text-base">{title}</p>
      <Link className="text-sm text-muted underline decoration-outline underline-offset-3" to={to}>
        {link}
      </Link>
    </div>
  );
}
