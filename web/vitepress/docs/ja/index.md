---
layout: home

title: Mister Morph ドキュメント
description: CLI、チャネル、ランタイム、Go 組み込みを扱うドキュメント。

hero:
  text: "Mister Morph Docs"
  tagline: "まずクイックスタート。その後は目的に応じて、拡張、長期運用、設定とガバナンスへ進む。"
  actions:
    - theme: brand
      text: クイックスタート
      link: /ja/guide/quickstart-cli
---

<section class="morph-home-section morph-home-start">
  <header class="morph-home-section-heading">
    <MorphKicker text="[ START // PATHS ]" />
    <h2>続きを読む</h2>
    <p>目的に応じて必要な内容を読んでください。</p>
  </header>
  <div class="morph-home-start-grid">
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">組み込みと拡張</h3>
        <p class="morph-home-route-copy">Go プログラムの中で core を再利用し、独自のホスト、UI、外部システムへつなぎたいならここから始める。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/ja/guide/build-your-own-agent">Core で Agent を構築</a>
      </div>
    </article>
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">長期運用とチャネル</h3>
        <p class="morph-home-route-copy">Console、Telegram、Slack などの長期運用入口として使うなら、まず runtime モードを見て、その後に guard を設定する。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/ja/guide/runtime-modes">Runtime モード</a>
      </div>
    </article>
    <article class="morph-home-route">
      <div class="morph-home-route-head">
        <h3 class="morph-home-route-title">設定とガバナンス</h3>
        <p class="morph-home-route-copy">すでに動いていて、次は provider、環境変数、既定値、安全境界を固めたいならここから始める。</p>
      </div>
      <div class="morph-home-route-actions">
        <a class="morph-home-route-primary" href="/ja/guide/config-patterns">設定パターン</a>
      </div>
    </article>
  </div>
</section>
