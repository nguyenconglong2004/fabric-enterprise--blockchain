module.exports = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Sora', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
      colors: {
        ink: {
          950: '#070b12',
          900: '#0d1420',
          800: '#121a27',
          700: '#182233',
          600: '#243044',
        },
        mint: {
          DEFAULT: '#14f195',
          dim: 'rgba(20, 241, 149, 0.12)',
        },
        aqua: '#4fd1c5',
      },
      boxShadow: {
        panel: '0 0 0 1px rgba(36, 48, 68, 0.8), 0 18px 40px rgba(0, 0, 0, 0.35)',
      },
    },
  },
  plugins: [],
};
