import eslint from "@eslint/js";
import typescript from "@typescript-eslint/eslint-plugin";
import parser from "@typescript-eslint/parser";
import vue from "eslint-plugin-vue";
import globals from "globals";

export default [
  {
    ignores: [
      ".vitepress/cache/**",
      ".vitepress/dist/**",
      "docs/**",
      "node_modules/**",
    ],
  },
  eslint.configs.recommended,
  ...vue.configs["flat/recommended"],
  {
    files: ["**/*.{ts,mts}"],
    languageOptions: {
      globals: globals.browser,
      parser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    plugins: {
      "@typescript-eslint": typescript,
    },
    rules: {
      ...typescript.configs.recommended.rules,
    },
  },
  {
    files: ["**/*.vue"],
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        ecmaVersion: "latest",
        parser,
        sourceType: "module",
      },
    },
    plugins: {
      "@typescript-eslint": typescript,
    },
    rules: {
      ...typescript.configs.recommended.rules,
      "vue/multi-word-component-names": "off",
    },
  },
  {
    files: ["**/*.mts"],
    languageOptions: {
      globals: globals.node,
    },
  },
];
