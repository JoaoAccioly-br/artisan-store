// Configuração base de ESLint (Flat Config) para os frontends Vue 3 / Nuxt 3.
// Os presets específicos de Vue e Nuxt serão estendidos nos pacotes
// desktop/frontend (Fase 7) e web (Fase 8).
export default [
  {
    ignores: [
      '**/node_modules/**',
      '**/.nuxt/**',
      '**/.output/**',
      '**/dist/**',
      'api/**',
      'docs/**',
    ],
  },
]
