# gshow

稼働中のGoプロセスで、goroutineが今何をしているかを観察するためのツール群。HTTPリクエストの
度に生成されるgoroutine、チャネル受信で待ち続けるworker、終了しないリークなど、実行中の様子を
そのまま確認できる。

2つの主要なコマンドで構成される。

- **`gshow`** — ライブのターミナルダッシュボード。一定間隔でgoroutineダンプを取得し、同じ動作
  をしているgoroutineをグループ化して表示する。前回取得時との差分も表示するので、goroutineの
  増減がそのまま追える。
- **`gshow-trace`** — 実行トレースのタイムライン表示。数秒間のGo実行トレースを取得し、各
  goroutineが実行中・実行可能・待機中・システムコール中のどれだったかを、ターミナル上のバー
  チャートとして描画する。

どちらのコマンドも、観測対象のプロセス側にgoroutineの情報を外部から取得できる仕組みを用意して
おく必要がある。それを用意するための `probe` パッケージと `gshow-build` コマンドも用意した。

## クイックスタート

観測対象がまだない場合は、付属のデモサーバーを使う。`net/http/pprof` を組み込んであり、次の2つ
のエンドポイントを試せる。`/work` はリクエストの度に短命なgoroutineを1つ起動し(起動したまま
待ち受けない、典型的なパターン)、`/leak` は終了しないgoroutineを起動する。

```sh
go run ./cmd/demo
```

別のターミナルで:

```sh
go run ./cmd/gshow
```

リクエストを送って、goroutineの増減がリアルタイムに表示されるのを確認する。

```sh
for i in $(seq 1 6); do curl -s http://localhost:6060/work >/dev/null & done
curl -s http://localhost:6060/leak >/dev/null
```

## `gshow`: ライブダッシュボード

1秒間隔(変更可能)でgoroutineダンプを取得し、状態と呼び出しスタックが完全に一致するgoroutine
をグループ化する。そして前回取得時との差分を「recent activity」欄に記録する。これにより、リク
エストの度に大量発生するgoroutineも、個々のスタックトレースの羅列に埋もれることなく、「+N個
起動」「-N個終了」という形でそのまま追える。

### 実行例

デモサーバーに `/work` を6回、`/leak` を1回リクエストした直後の出力:

```
  gshow    target http://localhost:6060/debug/pprof/goroutine?debug=2   goroutines 14 (-1)   updated 18:05:07
7 distinct stacks
 STATE             COUNT  WAIT        CREATED BY                    TOP FRAME
───────────────────────────────────────────────────────────────────────────────────────────────────
 sleep             5      -           main.handleWork               time.Sleep
 chan receive      4      -           main.startWorkerPool          main.startWorkerPool.func1
 IO wait           1      -           net/http.(*connReader).sta…   internal/poll.runtime_pollWait
 running           1      -           net/http.(*Server).Serve      runtime/pprof.writeGoroutine…
 chan receive      1      -           main.handleLeak               main.handleLeak.func1
recent activity
18:05:07 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:07 -1 finished  created by main.handleWork
18:05:07 +1 started  created by net/http.(*connReader).startBackgroundRead
18:05:06 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:06 +1 started  created by net/http.(*connReader).startBackgroundRead
18:05:06 +6 started  created by main.handleWork
18:05:06 +1 started  created by main.handleLeak
18:05:05 -1 finished  created by net/http.(*connReader).startBackgroundRead
18:05:05 +1 started  created by net/http.(*connReader).startBackgroundRead
↑/↓ select · enter stack detail · / filter · r refresh now · q quit
```

この出力から読み取れること。`main.handleWork` の5つのgoroutineは `time.Sleep` の途中(擬似的な
作業中)であり、4つのidle workerはチャネル受信で待機している。下の「recent activity」には直前
に起きたことがそのまま記録されている。`handleWork` のgoroutineが6つまとめて起動し(`/work` へ
の一斉リクエスト)、そのうち1つはすでに終了している。そして `handleLeak` のgoroutineが1つ起動
しているが、これは終了しないため `chan receive` のグループに残り続ける。`/leak` を叩き続けれ
ばこの数が増え続ける様子がそのまま見える。それがリークである。

行を選んで `enter` を押すと、そのグループに属する1つのgoroutineの完全なスタックトレースを表示
できる。

```
4 goroutine(s) — state: chan receive
created by main.startWorkerPool (/home/user/gshow/cmd/demo/main.go:73)
members: #8(-), #9(-), #10(-), #11(-)

stack (goroutine 8)
goroutine 8 [chan receive]:
main.startWorkerPool.func1()
	/home/user/gshow/cmd/demo/main.go:74 +0x45
created by main.startWorkerPool in goroutine 1
	/home/user/gshow/cmd/demo/main.go:73 +0x3a
```

### 使い方

```sh
go run ./cmd/gshow -addr <host:port> -interval 1s
```

- `-addr` — 対象プロセスのHTTP pprof形式のアドレス。例: `localhost:6060`、または
  `.../debug/pprof/goroutine` の完全なURL。`-socket` も `-file` も指定しない場合のデフォルトは
  `localhost:6060`。
- `-socket` — `probe.ListenAndServeUnix` が待ち受けるUnixドメインソケットのパス。
- `-file` — 稼働中のプロセスの代わりに、保存済みのダンプを読み込む(`-` でstdin)。
- `-interval` — 取得間隔。デフォルト `1s`。
- `-timeout` — 1回の取得あたりのHTTPタイムアウト。デフォルト `5s`。

`-addr`・`-socket`・`-file` は同時に1つしか指定できない。

キー操作: `↑`/`↓` で選択、`enter` でグループの完全なスタックを表示、`esc` で戻る、`/` で状態・
生成元・関数名による絞り込み、`r` で即時更新、`q` で終了。

## 観測対象への接続方法

観測対象のプロセスの状態に応じて、接続方法を選ぶ。HTTPサーバーを持たないプロセスにも `gshow` を
届けられるようにしてある。

| 観測対象の状態 | すること | `gshow` の指定 |
| --- | --- | --- |
| `net/http/pprof` を組み込み済み | 何もしない | `-addr host:port` |
| 独自のHTTPマルチプレクサはあるが pprof はなし | `mux.Handle(probe.Path, probe.Handler())` | `-addr host:port` |
| HTTPサーバー自体がない | `go probe.ListenAndServe(":6061")` | `-addr host:port` |
| HTTPサーバーがなく、ポートも開けたくない | `go probe.ListenAndServeUnix("/tmp/x.sock")` | `-socket /tmp/x.sock` |
| 保存済み、またはパイプで受け取ったダンプがある | 何もしない | `-file dump.txt` / `-file -` |
| ソースに一切手を入れられない | `go build` の代わりに `gshow-build` でビルドする | 上記いずれかの `-addr` / `-socket` |

`probe`(このモジュールの `probe` パッケージ)は、`net/http/pprof` の
`/debug/pprof/goroutine?debug=2` と `/debug/pprof/trace` の両方と同じ形式でデータを返す。その
ため `gshow` と `gshow-trace` は、対象がどちらの実装であっても変更なしに動作する。
`net/http/pprof` パッケージ全体を組み込みたくない場合や、デフォルトのマルチプレクサを共有した
くない場合の代わりに使える。

### ソース変更なしでのビルド: `gshow-build`

importを1行追加することすら避けたい場合は、`gshow-build` を使う。`go build`/`run`/`install`/
`test` をラップし、ビルド時に `-overlay` でprobeを注入する。対象のソースファイルは一切変更され
ない。

```sh
go get github.com/hiroyukim/gshow   # 対象モジュールで一度だけ実行
gshow-build build ./cmd/yourapp
```

待ち受けアドレスは実行時に環境変数 `GSHOW_ADDR` から読み込む(デフォルト `localhost:6061`)。

依存関係が追加されていない場合、`gshow-build` は黙って何もしないわけではない。内部で呼び出す
`go build` がそのままエラーになり、何を実行すればよいかを明示する。

```
$ gshow-build build ./cmd/yourapp
gshow_probe_inject.go:3:8: no required module provides package github.com/hiroyukim/gshow/probe/auto; to add it:
	go get github.com/hiroyukim/gshow/probe/auto
```

`go.mod` に `require` 行があるだけでよく、実際にimportする箇所は不要である。ただしこれは、
`go mod tidy` を素のまま実行すると、使われていないと判断されて削除されてしまうことも意味する。
Goのプロジェクトがビルド時専用のツール依存を固定するときの標準的な方法で、`tidy` に消されない
ようピン留めする。

```go
//go:build tools

package tools

import _ "github.com/hiroyukim/gshow/probe/auto"
```

(この注入は、最初は `go build -toolexec` だけで実現しようとした。しかしこれは成立しない。
`-toolexec` のラッパーが呼ばれる時点で、`go build` は実際のソースファイルからパッケージの依存
グラフを既に確定させている。そのため、そこで新しいimportを注入してもコンパイラの
`-importcfg` には載らず、解決できない。`-overlay` はこの依存グラフが確定する前に新しいソース
ファイルを追加できるため、実際に機能する。両方の方式を実機で試した上で `-overlay` に決めた。)

## `gshow-trace`: 実行トレースのタイムライン

`gshow` のダッシュボードは、1秒おきの完全なスタックダンプという定期スナップショットの上に成り
立っている。「今何が溜まっているか」を見るには十分だが、goroutineが正確にいつブロックしたか、
2回のスナップショットの間に実際どれだけ実行されていたかまでは分からない。`gshow-trace` はその
代わりにGoの実行トレース(`go tool trace` と同じ仕組み)を取得する。実行トレースはすべてのスケ
ジューリングイベントをナノ秒単位のタイムスタンプ付きで記録しており、それをgoroutineごとのタイ
ムラインとしてターミナルに描画する。

```sh
go run ./cmd/gshow-trace -addr localhost:6060 -seconds 3
```

### 実行例

デモサーバーに `/work` を連続リクエストし、`/leak` も1回リクエストした状態でのキャプチャ:

```
capturing a 3s execution trace from localhost:6060...
captured 3.001s across 19 goroutines (most active first, showing up to 12) - 20 runtime/GC housekeeping goroutines hidden, pass -all to show them

g97      - (select)                ·······························································································
g12      main.ticker (chan receiv…                                 ·······························································
g4       - (system goroutine wait) ·······························································································
g26      main.doWork                                               ···························································
g84      main.doWork                                               ···························································
g90      -                                                                                                                        
g102     -                                                                                                                        
g49      -                                                                                                                        
g96      main.handleLeak.func1                                     ·······························································
g93      main.doWork                                               ·······························································
g8       main.startWorkerPool.fun…                                 ·······························································
g9       main.startWorkerPool.fun…                                 ·······························································

█ running   ▒ runnable  ▓ syscall   · waiting     not alive
```

(実際のターミナルでは、凡例の色でそれぞれの文字が色分けされる。上はプレーンテキストでの表示。)
1行が1つのgoroutineに対応し、生成元の関数名でラベル付けされている。バーはキャプチャ期間全体に
対応し、1文字が1つの時間区間を表す。

### すべてのバーの先頭にある空白について

このサンプルでは、どのバーも先頭にしばらく空白があり、その後にドットが始まっている。worker
pool(`g8`/`g9`)のように、キャプチャ開始前から明らかに存在していたgoroutineでも同じ空白があ
る。これはこのツールの不具合ではなく、Goの実行トレースの仕様である。スケジューリングされてい
ないgoroutineは、トレーサーが次に全goroutineの状態をまとめて同期するタイミング(このトレース
形式は状態をおよそ1秒ごとの「世代」単位でまとめている)まで、一切イベントを出さない。そのため
どのキャプチャでも、最初の約1秒間はキャプチャ開始前から存在していたidle状態のgoroutineが見え
ない。これは「動いていない」のではなく、「まだデータがない」状態である。`-seconds` を長くすれ
ばこの空白の割合は相対的に小さくなるが、なくなりはしない。より正確な全体像が必要な場合は、生
のトレースを保存して本家のツールで開く。

```sh
gshow-trace -addr localhost:6060 -seconds 3 -out trace.out
go tool trace trace.out
```

### 使い方

```sh
go run ./cmd/gshow-trace -addr <host:port> -seconds 2
```

- `-addr` — 対象プロセスのpprofアドレス。デフォルト `localhost:6060`。
- `-seconds` — キャプチャする長さ(秒)。デフォルト `2`。
- `-out` — 生のトレースもこのパスに保存する(`go tool trace` 用)。
- `-width` — 出力の幅(桁数)。デフォルト `160`。
- `-rows` — 表示するgoroutineの最大数。状態変化が多いものから順に表示する。デフォルト `40`。
- `-all` — GC・トレース機構自身のgoroutine(デフォルトでは非表示。後述)も表示する。
- `-json` — テキストのタイムラインの代わりに、機械可読なJSONレポートを標準出力に表示する。

生成元の関数が `runtime`・`runtime/trace`・`runtime/pprof` に属するgoroutineは、デフォルトで非
表示にしている。GCのworkerやトレーサー自身の内部処理は、対象が何であっても毎回のキャプチャに
出現し、ノイズにしかならないためである。`-all` を指定すればこれらも表示できる。

このコマンドにはライブ更新モードがない。キャプチャは `trace.Start`/`trace.Stop` による有限区
間の取得であり、常時ストリーミングする性質のものではないため、ライブダッシュボードの1モードと
してではなく、独立した単発のコマンドにしてある。

### JSON出力(`-json`)

テキストのタイムラインは人間が目で追うにはよいが、色付きのグリフを1文字ずつ数えるような読み方
は、AIエージェントなど別のプログラムから扱うには向いていない。「このgoroutineはどれくらいブロ
ックされていたか」に答えるには、バーの文字数を数えるより、区間の開始・終了時刻を直接読める方が
はるかに確実である。`-json` を指定すると、`-rows`/`-all` によるgoroutineの選定と並び順はテキス
ト表示と同じまま、その結果を構造化データとして出力する。

```sh
gshow-trace -addr localhost:6060 -seconds 1 -json -rows 3
```

```json
{
  "target": "localhost:6060",
  "duration_ms": 1003.189953,
  "goroutine_count": 33,
  "hidden_runtime_internal": 21,
  "goroutines": [
    {
      "id": 56,
      "creator": "-",
      "is_runtime_internal": false,
      "reason": "select",
      "first_seen_ms": 0.163712,
      "last_seen_ms": 1003.189953,
      "totals_ms": {
        "running_ms": 0.294977,
        "runnable_ms": 0.020416,
        "waiting_ms": 1002.710848,
        "syscall_ms": 0
      },
      "spans": [
        { "state": "Running", "start_ms": 0.163712, "end_ms": 0.254784 },
        { "state": "Waiting", "reason": "select", "start_ms": 0.254784, "end_ms": 1000.87328 },
        { "state": "Runnable", "start_ms": 1000.87328, "end_ms": 1000.88896 },
        { "state": "Running", "start_ms": 1000.88896, "end_ms": 1000.96928 },
        { "state": "Waiting", "reason": "sync", "start_ms": 1000.96928, "end_ms": 1003.05984 },
        { "state": "Waiting", "start_ms": 1003.05984, "end_ms": 1003.061632 },
        { "state": "Runnable", "start_ms": 1003.061632, "end_ms": 1003.066368 },
        { "state": "Running", "start_ms": 1003.066368, "end_ms": 1003.189953 }
      ]
    }
  ]
}
```
(実際の出力にはここまでで指定した3件のgoroutine分の要素が並ぶが、紙面の都合で1件目のみ抜粋し
た。`creator` が `"-"` なのは、このgoroutineに帰属できるフレームが見つからなかったことを示し
ており、これも実際にあり得る結果である。)

`/work` リクエストで生成される側のgoroutineは、こういう記録になる(別のキャプチャからの抜粋):

```json
{
  "id": 51,
  "creator": "main.doWork",
  "is_runtime_internal": false,
  "first_seen_ms": 1001.481856,
  "last_seen_ms": 1002.195073,
  "totals_ms": {
    "running_ms": 0,
    "runnable_ms": 0,
    "waiting_ms": 0.713217,
    "syscall_ms": 0
  },
  "spans": [
    { "state": "Waiting", "start_ms": 1001.481856, "end_ms": 1002.193152 },
    { "state": "Waiting", "start_ms": 1002.193152, "end_ms": 1002.195073 }
  ]
}
```

`first_seen_ms` がキャプチャ開始よりだいぶ後ろにあることから、このgoroutineがキャプチャの途中
で生成されたことが分かる。テキスト表示でバーの先頭にある空白を目で確認する代わりに、この数値を
そのまま比較すればよい。

フィールドの意味:

- `creator` — このgoroutineを要約するのに最も有用なフレーム。テキスト表示の生成元ラベルと同じ
  ロジックで選んでいる(内部の停止関数ではなく、`go func(){...}` の呼び出し元に近いフレームを
  優先する)。
- `reason` — 最も長く滞在した待機区間の理由(`chan receive` や `select` など)。理由が付いた区
  間が一つもなければ省略される。
- `totals_ms` — 状態ごとの合計滞在時間。各 `spans` の `state` を集計したもの。
- `spans` — 状態が変わるたびの区間を時系列順に並べたもの。`reason` は理由が付く区間(主に
  `Waiting`)にのみ含まれる。
- `first_seen_ms` / `last_seen_ms` — このgoroutineについて最初に記録された区間の開始時刻と、
  最後の区間の終了時刻。`first_seen_ms` が `0` に近くない場合、そのgoroutineはキャプチャの途中
  で生成されたことを意味する(前述の「すべてのバーの先頭にある空白について」も参照)。

## 仕組み

- `probe` / `probe/auto` — 対象プロセスを観測可能にする。goroutineダンプと実行トレースの両方
  を、`net/http/pprof` と同じ形式で提供するハンドラ(または1行で起動できる簡易サーバー)。
- `cmd/gshow-build` — `go build` 系のコマンドを `-overlay` でラップし、対象パッケージに
  `probe/auto` の blank import を注入する。importを手で1行書くことすら避けたい場合に使う。
- `internal/goroutine` — goroutineダンプを解析し、状態と呼び出しスタックの組み合わせで
  goroutineをグループ化する。引数の値(goroutineごとに異なる生のアドレス)は同一判定から除外
  している。
- `internal/fetch` — 対象からHTTP(TCPまたはUnixソケット)経由でダンプを取得する、または保存
  済みのダンプを読み込む。
- `internal/tui` — 取得のたびに前回との差分を取り、どのgoroutineが生成元ごとに起動・終了した
  かを記録する。
- `internal/xtrace` — 実行トレースを(`golang.org/x/exp/trace` を使って)解析し、goroutineごと
  の状態タイムラインを構築する。同じ選定結果を、テキストのタイムラインとしても、`-json` 用の
  構造化データ(`Report`)としても描画できる。
