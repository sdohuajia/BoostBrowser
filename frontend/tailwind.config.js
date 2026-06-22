/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#fdf8f3',
          100: '#f9ede0',
          200: '#f3dbc2',
          300: '#e9c49d',
          400: '#dda876',
          500: '#c9915c',
          600: '#b87d4a',
          700: '#9a6840',
          800: '#7d5538',
          900: '#664630',
        },
        accent: {
          yellow: '#f5d547',
          orange: '#f5a962',
        },
        background: {
          DEFAULT: '#e5e7eb',
          card: '#ffffff',
        },
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'Roboto', '"Helvetica Neue"', 'Arial', 'sans-serif'],
      },
      borderRadius: {
        'DEFAULT': '0.5rem',
        'lg': '0.75rem',
        'xl': '1rem',
      },
      boxShadow: {
        'card': '0 2px 8px rgba(0, 0, 0, 0.08)',
        'card-hover': '0 4px 16px rgba(0, 0, 0, 0.12)',
      },
    },
  },
  plugins: [],
}
