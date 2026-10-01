/// <reference types="vite/client" />

/** 仓库 slug（owner/repo）——vite.config.ts define 构建期注入（CI=github.repository，缺省=组织仓库） */
declare const __REPO_SLUG__: string
