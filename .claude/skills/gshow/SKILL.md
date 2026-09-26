---
name: gshow
description: 稼働中のGoプロセスのgoroutine(ハング・デッドロック・リーク・「今何をしているか分からない」)を調査・デバッグするときに使う。"goroutine leak", "Go process hang", "deadlock", "why is this stuck" のような英語での相談にも使う。gshow/gshow-trace CLI(github.com/hiroyukim/gshow)で対象プロセスからgoroutineダンプや実行トレースを取得し、-json で構造化データとして直接読む。生のテキストダンプ(pprof debug=2)をそのまま読ませる前に、まずこのスキルの手順に従う。
---

# gshow: Goプロセスのgoroutine調査

対象は「今動いているGoプロセス」であり、ソースコードを読むだけでは分からない実行時の状態(どの
goroutineが何個、どこで、何を待っているか)を調べるためのツール。このリポジトリ自身
(github.com/hiroyukim/gshow)の `cmd/gshow` と `cmd/gshow-trace` を使う。

出力は必ず `-json` を付けて取得する。色付きのテキスト表示(グリフのタイムラインやテーブル)は
人間の目で読むためのものであり、文字数を数えて状態を判定するような読み方はしないこと。

## 0. 対象プロセスへの接続方法を確認する

先に対象プロセスの状態を確認する。

| 対象の状態 | すること |
| --- | --- |
| `net/http/pprof` を組み込み済み、または既にpprofのアドレスが分かっている | 何もしない。そのアドレスを `-addr` に使う |
| 自分(agent)がこのセッションで起動したGoプロセスで、ソースを編集できる | `import _ "github.com/hiroyukim/gshow/probe"` 相当を追加するか、`gshow-build` でビルドし直す |
| ソースを編集できない、または`go build`をやり直したくない | まずは `-addr` で `net/http/pprof` が既に生きていないか試す。生きていなければ、ユーザーに `gshow-build` の利用か`probe`パッケージの組み込みを提案する |

詳細は `README.md` の「観測対象への接続方法」を参照。アドレスが分からない/繋がらない場合は、対
象プロセスがpprofを提供していない可能性が高いので、先に上記の対応をユーザーに提案する。

## 1. 「今、何が溜まっているか」を知りたい場合 → `gshow -json`

goroutineリークの疑い、ハング調査の第一歩、単に現状のスナップショットが欲しいとき。

```sh
go run ./cmd/gshow -addr <host:port> -json
```

出力(`internal/goroutine.Report`)は、状態とスタックが完全に一致するgoroutineをグループ化した
もの。読み方:

- `groups[].count` が大きい、かつ `state` が `chan receive`/`select` などの待機状態のまま何度
  実行しても減らない → リークの疑いが強い。`created_by` がリークの発生源。
- `groups[].stack` に代表1件の完全なスタックダンプが入っている。原因箇所の特定に使う。
- `groups[].member_ids` はそのグループに属するgoroutine IDの一覧。

リークを疑うときは、`-json` を数秒おきに2回以上叩いて `count` の推移を比較すること。1回のスナ
ップショットだけでは「増え続けている」かどうかは分からない。継続的に監視したいなら、ユーザーに
ライブダッシュボード(`gshow -addr <addr>`、引数なしなら対話的なTUIが起動する)の利用も提案し
てよい。

## 2. 「いつ・どれだけブロックされていたか」正確なタイミングが要る場合 → `gshow-trace -json`

「このgoroutineはリクエストの後どれくらいで生成されたか」「実際にCPUを使っていた時間はどれく
らいか」など、スナップショットでは分からない時系列の情報が必要なとき。

```sh
go run ./cmd/gshow-trace -addr <host:port> -seconds 2 -json
```

出力(`internal/xtrace.Report`)はgoroutineごとの状態タイムライン。読み方:

- `goroutines[].totals_ms` — 状態(`running_ms`/`runnable_ms`/`waiting_ms`/`syscall_ms`)ごとの
  合計時間。ほぼ全てが `waiting_ms` なら、そのgoroutineはCPUを使わず何かを待っているだけ。
- `goroutines[].reason` — 待機理由(`chan receive`・`select`・`sync` など)。原因の切り分けに使
  う。
- `goroutines[].first_seen_ms` — キャプチャ開始から何ms後に初めて観測されたか。`duration_ms`
  に対してこれが0に近くなければ、そのgoroutineはキャプチャ中に生成されたことが分かる(逆に、
  最初の約1秒はアイドルなgoroutineが観測されないことがある。バグではなくGo実行トレースの仕
  様。詳細はREADMEの「すべてのバーの先頭にある空白について」を参照)。
- `goroutines[].is_runtime_internal` — GC・トレース機構自身のgoroutine。デフォルトの `-json`
  はこれを除外して返す(除外数は `hidden_runtime_internal` に入っている)。すべて見たい場合の
  み `-all` を付ける。
- デフォルトでは活動量(状態変化の回数)が多い順に、最大40件(`-rows` で変更可)しか返らない。
  対象のgoroutine IDが分かっているなら `-rows` を増やすか、`-all` と併用して絞り込む。

## 判断の目安

- goroutine数が増え続ける、特定の `created_by` から生成されたグループが減らない → リーク。原
  因は `stack`/`created_by` から特定する。
- 全体が `waiting`/`chan receive`/`select` で止まっていて `running` 状態のgoroutineがほぼ無い
  → デッドロック、または外部(DB・API・別プロセス)からの応答待ちを疑う。`reason` と `stack` で
  何を待っているかを確認する。
- CPU使用率が高いのにハングして見える → `gshow-trace -json` の `running_ms`/`syscall_ms` が大
  きいgoroutineを探す。ビジーループの疑い。

## 出力をそのまま貼らない

`stack` フィールドや `spans` の生データをユーザーへの返答にそのまま大量に貼り付けない。原因の
特定に必要な部分(該当するgoroutineの状態・生成元・スタックの該当行)だけを抜き出して説明す
る。
