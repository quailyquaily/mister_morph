---
layout: home

title: Mister Morph 文档
description: 包含 CLI、通道、运行模式和 Go 嵌入的文档。

hero:
  text: "Mister Morph 文档"
  tagline: "先看快速开始，再按你的目标继续读：扩展、长期运行，或配置治理。"
  actions:
    - theme: brand
      text: 快速开始
      link: /zh/guide/quickstart-cli
---

<section class="morph-home-section morph-home-start">
  <header class="morph-home-section-heading">
    <MorphKicker text="[ START // PATHS ]" />
    <h2>继续阅读</h2>
    <p>根据你的目的阅读相应的内容。</p>
  </header>
  <div class="morph-home-start-grid">
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">嵌入与扩展</h3>
        <p class="morph-home-route-copy">当你要在 Go 程序里复用 core，自己接宿主层、前端或外部系统，就从这里开始。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/zh/guide/build-your-own-agent">用 Core 搭建 Agent</a>
      </div>
    </article>
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">长期运行与通道</h3>
        <p class="morph-home-route-copy">当你要把它作为 Console、Telegram、Slack 这类长期运行入口来用，先看运行模式，再配置 guard。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/zh/guide/runtime-modes">Runtime 模式</a>
      </div>
    </article>
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">配置与治理</h3>
        <p class="morph-home-route-copy">当你已经能跑起来，接下来要把 provider、环境变量、默认值和安全边界定稳，就从这里开始。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/zh/guide/config-patterns">配置模式</a>
      </div>
    </article>
  </div>
</section>
