import { readdirSync } from 'node:fs';
import js from '@eslint/js';
import globals from 'globals';
import importPlugin from 'eslint-plugin-import';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  js.configs.recommended,
  tseslint.configs.recommended,
  { files: ['src/**/*.{ts,tsx}'], languageOptions: { globals: globals.browser } },
  { files: ['*.config.{js,ts}'], languageOptions: { globals: globals.node } },
  {
    files: ['src/modules/**/*.{ts,tsx}'],
    plugins: { import: importPlugin },
    settings: { 'import/resolver': { typescript: { project: './tsconfig.json' } } },
    rules: {
      'import/no-restricted-paths': [
        'error',
        {
          zones: readdirSync('./src/modules').map((m) => ({
            target: `./src/modules/${m}`,
            from: './src/modules',
            except: [`./${m}`],
          })),
        },
      ],
    },
  },
);
