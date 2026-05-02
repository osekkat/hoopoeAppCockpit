import { tailwindTokenTheme } from "../../packages/design-system/src/tokens/index.ts";

export default {
  content: [
    "./src/**/*.{ts,tsx}",
    "../../packages/design-system/src/**/*.{ts,tsx}",
  ],
  theme: {
    extend: tailwindTokenTheme,
  },
  plugins: [],
};
