import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    // shadcn/ui primitives export both components and cva variant helpers by
    // convention; keeping the stock file shape avoids diffs on `npx shadcn add`.
    files: ['src/components/ui/**/*.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // Shared statistics presentation module (docs/ref/todo/
    // statistics-nav-restructure-plan.md T-01) intentionally exports a hook
    // (useStatsWindow) and a type (StatsWindow) alongside its components, by
    // plan design — keeping all of them in one file is the point (§2.6).
    files: ['src/components/statistics/DnsStatsShared.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // Shared Traffic-page presentation module (docs/ref/todo/
    // statistics-traffic-page-plan.md T-08) — same rationale as
    // DnsStatsShared.tsx above: intentionally exports hooks
    // (useSortableRows, useTextFilter) and types alongside its components.
    files: ['src/components/statistics/TrafficStatsShared.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
