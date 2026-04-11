type LocaleKey = 'en' | 'zh' | 'ja'
type LocalizedText = Record<LocaleKey, string>
type SidebarSpec = Array<{
  text: LocalizedText
  items: Array<{
    slug: string
    text: LocalizedText
  }>
}>

const localePrefixes: Record<LocaleKey, string> = {
  en: '',
  zh: '/zh',
  ja: '/ja'
}

const sidebarSpec: SidebarSpec = [
  {
    text: {
      en: 'Getting Started',
      zh: '开始使用',
      ja: 'はじめに'
    },
    items: [
      {
        slug: 'overview',
        text: {
          en: 'Overview',
          zh: '总览',
          ja: '概要'
        }
      },
      {
        slug: 'quickstart-cli',
        text: {
          en: 'Quickstart (CLI)',
          zh: '快速开始（CLI）',
          ja: 'クイックスタート（CLI）'
        }
      }
    ]
  },
  {
    text: {
      en: 'Runtime',
      zh: '运行模式',
      ja: 'ランタイム'
    },
    items: [
      {
        slug: 'runtime-modes',
        text: {
          en: 'Runtime Modes',
          zh: 'Runtime 模式',
          ja: 'Runtime モード'
        }
      },
      {
        slug: 'prompt-architecture',
        text: {
          en: 'Prompt Architecture',
          zh: 'Prompt 组织',
          ja: 'Prompt 設計'
        }
      },
      {
        slug: 'memory',
        text: {
          en: 'Memory',
          zh: '记忆',
          ja: 'Memory'
        }
      },
      {
        slug: 'skills',
        text: {
          en: 'Skills',
          zh: '技能',
          ja: 'Skills'
        }
      },
      {
        slug: 'built-in-tools',
        text: {
          en: 'Built-in Tools',
          zh: '内置工具',
          ja: '組み込みツール'
        }
      },
      {
        slug: 'subagents',
        text: {
          en: 'Subagents',
          zh: 'Subagents',
          ja: 'Subagents'
        }
      },
      {
        slug: 'acp',
        text: {
          en: 'ACP',
          zh: 'ACP',
          ja: 'ACP'
        }
      },
      {
        slug: 'mcp',
        text: {
          en: 'MCP',
          zh: 'MCP',
          ja: 'MCP'
        }
      },
      {
        slug: 'llm-routing',
        text: {
          en: 'LLM Routing Policies',
          zh: 'LLM 路由策略',
          ja: 'LLM ルーティングポリシー'
        }
      }
    ]
  },
  {
    text: {
      en: 'Developer',
      zh: '开发者',
      ja: '開発者'
    },
    items: [
      {
        slug: 'build-your-own-agent',
        text: {
          en: 'Create Your Own AI Agent',
          zh: '创建自己的 AI Agent',
          ja: '自分の AI Agent を作る'
        }
      },
      {
        slug: 'build-your-own-agent-advanced',
        text: {
          en: 'Create Your Own AI Agent: Advanced',
          zh: '创建自己的 AI Agent：进阶',
          ja: '自分の AI Agent を作る：上級編'
        }
      },
      {
        slug: 'agent-level-customization',
        text: {
          en: 'Agent-Level Customization',
          zh: 'Agent 底层扩展',
          ja: 'Agent レイヤ拡張'
        }
      }
    ]
  },
  {
    text: {
      en: 'References',
      zh: '参考',
      ja: 'リファレンス'
    },
    items: [
      {
        slug: 'integration-references',
        text: {
          en: 'Integration API',
          zh: 'Integration API',
          ja: 'Integration API'
        }
      },
      {
        slug: 'config-reference',
        text: {
          en: 'Config Fields',
          zh: '配置字段',
          ja: '設定フィールド'
        }
      },
      {
        slug: 'env-vars-reference',
        text: {
          en: 'Environment Variables',
          zh: '环境变量',
          ja: '環境変数'
        }
      },
      {
        slug: 'cli-flags',
        text: {
          en: 'CLI Flags',
          zh: '命令行参数',
          ja: 'CLI フラグ'
        }
      }
    ]
  },
  {
    text: {
      en: 'Operations & Governance',
      zh: '运维与治理',
      ja: '運用とガバナンス'
    },
    items: [
      {
        slug: 'security-and-guard',
        text: {
          en: 'Security and Guard',
          zh: '安全与 Guard',
          ja: 'セキュリティと Guard'
        }
      },
      {
        slug: 'config-patterns',
        text: {
          en: 'Config Patterns',
          zh: '配置模式',
          ja: '設定パターン'
        }
      },
      {
        slug: 'docs-map',
        text: {
          en: 'Repository Docs Map',
          zh: '仓库文档地图',
          ja: 'リポジトリ文書マップ'
        }
      }
    ]
  }
]

const localeUi = {
  en: {
    label: 'English',
    lang: 'en-US',
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Overview', link: '/guide/overview' },
      { text: 'Developer', link: '/guide/build-your-own-agent' }
    ],
    docFooter: { prev: 'Previous', next: 'Next' },
    editLinkText: 'Edit this page on GitHub',
    outlineLabel: 'On this page'
  },
  zh: {
    label: '简体中文',
    lang: 'zh-CN',
    link: '/zh/',
    nav: [
      { text: '首页', link: '/zh/' },
      { text: '文档总览', link: '/zh/guide/overview' },
      { text: '开发者', link: '/zh/guide/build-your-own-agent' }
    ],
    docFooter: { prev: '上一页', next: '下一页' },
    editLinkText: '在 GitHub 编辑此页',
    outlineLabel: '页面目录'
  },
  ja: {
    label: '日本語',
    lang: 'ja-JP',
    link: '/ja/',
    nav: [
      { text: 'ホーム', link: '/ja/' },
      { text: 'ドキュメント総覧', link: '/ja/guide/overview' },
      { text: '開発者', link: '/ja/guide/build-your-own-agent' }
    ],
    docFooter: { prev: '前へ', next: '次へ' },
    editLinkText: 'GitHub でこのページを編集',
    outlineLabel: '目次'
  }
} as const

function localizeGuideLink(locale: LocaleKey, slug: string): string {
  return `${localePrefixes[locale]}/guide/${slug}`
}

function buildSidebar(locale: LocaleKey) {
  return sidebarSpec.map((section) => ({
    text: section.text[locale],
    items: section.items.map((item) => ({
      text: item.text[locale],
      link: localizeGuideLink(locale, item.slug)
    }))
  }))
}

export function createLocalesConfig(editLinkPattern: string) {
  return {
    root: {
      label: localeUi.en.label,
      lang: localeUi.en.lang,
      themeConfig: {
        nav: localeUi.en.nav,
        sidebar: buildSidebar('en'),
        docFooter: localeUi.en.docFooter,
        editLink: {
          pattern: editLinkPattern,
          text: localeUi.en.editLinkText
        },
        outline: {
          level: [2, 3],
          label: localeUi.en.outlineLabel
        }
      }
    },
    zh: {
      label: localeUi.zh.label,
      lang: localeUi.zh.lang,
      link: localeUi.zh.link,
      themeConfig: {
        nav: localeUi.zh.nav,
        sidebar: buildSidebar('zh'),
        docFooter: localeUi.zh.docFooter,
        editLink: {
          pattern: editLinkPattern,
          text: localeUi.zh.editLinkText
        },
        outline: {
          level: [2, 3],
          label: localeUi.zh.outlineLabel
        }
      }
    },
    ja: {
      label: localeUi.ja.label,
      lang: localeUi.ja.lang,
      link: localeUi.ja.link,
      themeConfig: {
        nav: localeUi.ja.nav,
        sidebar: buildSidebar('ja'),
        docFooter: localeUi.ja.docFooter,
        editLink: {
          pattern: editLinkPattern,
          text: localeUi.ja.editLinkText
        },
        outline: {
          level: [2, 3],
          label: localeUi.ja.outlineLabel
        }
      }
    }
  }
}
