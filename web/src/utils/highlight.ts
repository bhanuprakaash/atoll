export type TokenKind = 'comment' | 'string' | 'number' | 'keyword';
export type Token = { text: string; kind?: TokenKind };

const _hash = new Set([
  'sh',
  'bash',
  'zsh',
  'shell',
  'yaml',
  'yml',
  'toml',
  'python',
  'py',
  'ruby',
]);
const _keywords = new Set(
  (
    'func function return if else elif for while switch case const let var type struct interface ' +
    'import from export package class def fn pub use true false null nil None True False async await ' +
    'range go defer in of new try catch throw break continue default extends enum static self mut impl ' +
    'match then fi do done'
  ).split(' '),
);

const _string = String.raw`"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|` + '`(?:\\\\.|[^`\\\\])*`';
const _rest = `(${_string})|(\\b\\d+(?:\\.\\d+)?\\b)|(\\b[A-Za-z_]\\w*\\b)`;
const _slash = new RegExp(`(\\/\\/.*|\\/\\*[\\s\\S]*?\\*\\/)|${_rest}`, 'g');
const _pound = new RegExp(`(#.*)|${_rest}`, 'g');

export function highlight(source: string, lang = ''): Token[] {
  const re = _hash.has(lang) ? _pound : _slash;
  const tokens: Token[] = [];
  let at = 0;
  for (const m of source.matchAll(re)) {
    const word = m[4];
    if (word !== undefined && !_keywords.has(word)) continue;
    if (m.index > at) tokens.push({ text: source.slice(at, m.index) });
    const kind: TokenKind = m[1] ? 'comment' : m[2] ? 'string' : m[3] ? 'number' : 'keyword';
    tokens.push({ text: m[0], kind });
    at = m.index + m[0].length;
  }
  if (at < source.length) tokens.push({ text: source.slice(at) });
  return tokens;
}
