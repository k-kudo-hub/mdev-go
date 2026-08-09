# record-output 移植の調査記録(evidence)

移植元: `claude-conductor/scripts/record-output.sh`(以下「現行版」)。
判断の根拠は原則としてコマンド実行の実測に置く。実行環境は macOS 15 / `jq-1.7.1-apple` / Go 1.25。

## 1. jq の丸め(`round`)と Go の `math.Round`

現行版は `($cost * 1000000 | round | . / 1000000)` で小数第 6 位に丸める。

```console
$ jq -n '[0.5,1.5,2.5,-0.5,-1.5,2.675,82000.5,0.0000005] | map(round)'
[1, 2, 3, -1, -2, 3, 82001, 0]
```

0.5 → 1、1.5 → 2、2.5 → 3、-0.5 → -1 なので **絶対値の大きい方向へ丸める(round half away from zero)**。
Go の `math.Round` も定義が「round half away from zero」で一致する。

### 乱数 20 万件での突き合わせ

Go で 20 万件の値(0〜100 の一様乱数 / n/2000000 / (n+0.5)/1000000 / 0〜0.0001 の 4 系統)を生成し、
最短往復表記(`strconv.FormatFloat(v,'g',-1,64)`)で出力したものを jq に食わせて
`(. * 1000000 | round | . / 1000000)` を計算させ、Go の `math.Round(v*1e6)/1e6` と比較した。

```console
$ cut -f1 pairs.tsv | jq -R -r 'tonumber | (. * 1000000 | round | . / 1000000) | tostring' > jqout.txt
$ cut -f2 pairs.tsv > goout.txt
$ diff -q jqout.txt goout.txt && echo IDENTICAL
IDENTICAL: 200000/200000
```

(3) 系統は「小数第 7 位がちょうど 5」の値を狙って作ってあり、半数丸めの方向が確実に踏まれる。

さらに **`encoding/json` の出力表記も jq と完全一致**することを同じ 20 万件で確認した
(`json.Marshal(float64)` と `jq -r tostring` の文字列比較)。

```console
$ cut -f2 pairs.tsv | go run jsonfmt.go > gojson.txt
$ cut -f2 pairs.tsv | jq -R -r 'tonumber|tostring' > jqjson.txt
$ diff -q gojson.txt jqjson.txt && echo SAME
encoding/json output == jq output for all 200000
```

したがって cost は値としてだけでなく **daily ファイル上の表記としても現行版と一致**する。

## 2. jq の `//`(alternative)の真偽判定

```console
$ jq -n '["" // "fb", (false // "fb"), (null // "fb"), (0 // "fb")]'
["", "fb", "fb", 0]
```

**空文字と 0 は真**であり、偽は `null` と `false` だけ。影響:

- `$pricing[$model] // $pricing["claude-sonnet-4-6"] // {既定}`: モデル表に無ければ `null` なのでフォールバックが働く
- `($pricing.fast_multiplier // 6)`: **`fast_multiplier: 0` が設定されていれば 0 が採用される**(6 にはならない)。
  Go の `domain.Pricing.FastMultiplier float64` は「未設定」と「0」を区別できないため、
  `HasFastMultiplier bool` を追加して区別する(既存フィールドの型は変えない最小変更)
- `.file_path? // ""`: file_path が無い(null)ときだけ "" になる

## 3. jq の `add` と空配列

```console
$ jq -n '[null,100,null] | add'   # => 100(null は加算で無視される)
$ jq -n '[] | add // 0'           # => 0
```

`add` は `null + x = x` を使うため、`input_tokens` が欠けた usage が混ざっても合計は壊れない。

## 4. transcript のパース失敗条件(現行版がフォールバックへ落ちる条件)

現行版は `jq -sc ... 2>/dev/null` の**出力が空**のときにフォールバックレコードを書く。
どの入力で jq が失敗するかを実測した(クエリは record-output.sh:149-207 の抜粋)。

| 入力(1 行) | jq | 備考 |
|---|---|---|
| ファイルが空 | **成功** | turns 0 / model "unknown" / cost 0 の完全な summary が出る |
| `not json` | 失敗(parse error) | 1 行でも壊れていれば全体が失敗 |
| `123` / `"str"` / `true` / `[1,2]` | 失敗(Cannot index …) | トップレベルがオブジェクト以外 |
| `null` | **成功** | null への添字は null |
| `{"type":"user","message":"plain string"}` | 失敗 | `.message.content[]?` の `?` は `[]` にしか掛からず `.content` で落ちる |
| `{"message":{"content":"notarray"}}` | **成功** | `[]?` が効くので content が文字列でも落ちない |
| `{"message":{"usage":5}}` | 失敗 | `select(.message.usage?)` を 5 が通り `.input_tokens` で落ちる |

確認コマンド(抜粋):

```console
$ jq -sc '[.[] | .message.content[]? | select(.type=="tool_use")] as $t | {tools:($t|length)}' mstr.jsonl
jq: error (at mstr.jsonl:1): Cannot index string with string "content"
```

**Go 版の方針**: 1 行ずつ struct へ `json.Unmarshal` し、1 行でも失敗したらパース失敗とする。
`message` を `*claudeMessage`、`usage` を `*claudeUsage` にすることで上表の失敗条件がそのまま再現され、
`content` を `json.RawMessage` にすることで「content が文字列でも成功」も再現される。

さらに現行版が落ちる条件のうち、Go 版で明示的にパース失敗へ倒すもの:

- `tool_use` に `name` が無い / 文字列でない
  → 現行版も `select(.name | test("^mcp__slack"))` が `null (null) cannot be matched` で落ちる
- `content` 配列の要素がオブジェクト以外 → 現行版も `select(.type=="tool_use")` で落ちる

## 5. pricing の読み込み(config が壊れている場合の結論)

現行版 record-output.sh:24-32 は、他のスクリプトの `load_config`(ファイル単位のフォールバック)とは**別の読み方**をする。

```bash
if [ -f "$CONFIG_FILE" ]; then PRICING_JSON=$(jq -c '.pricing // empty' "$CONFIG_FILE" 2>/dev/null); fi
if [ -z "$PRICING_JSON" ] && [ -f "$CONFIG_DEFAULT" ]; then PRICING_JSON=$(jq -c '.pricing // empty' "$CONFIG_DEFAULT" 2>/dev/null); fi
PRICING_JSON="${PRICING_JSON:-"{}"}"
```

つまり **pricing キー単位の 2 段フォールバック**で、config.json が壊れていても
(jq が失敗して空文字になるため)config.default.json の pricing が使われ、
それも無ければ空 pricing `{}` で**続行する**(エラー終了しない)。

**結論**: PR #2 の持ち越し事項について、既存の `store.LoadConfig`(壊れ JSON をエラーにする)は**変更しない**。
理由は 2 つある。

1. 現行版でも「壊れ config → エラー終了」は record-output に限れば起きない挙動であり、
   record-output だけが必要とするのは pricing である。`LoadConfig` の厳格さは
   設定全体を必要とする他のユースケース(task_types・agents 等、フェーズ 2 以降)には依然として適切
2. record-output.sh が使う 2 段フォールバックは `LoadConfig` の「ファイル単位フォールバック」とは
   仕様が異なるため、そもそも同じ関数では表現できない

そのため `store.LoadPricing(conductorHome) domain.Pricing` を新設し、record-output 経路だけがこれを使う。
壊れた config.json はエラーにせず既定へフォールバックする(= 現行挙動)。

### pricing が JSON オブジェクト以外だった場合

`--argjson pricing '[1,2]'` のように配列が渡ると `$pricing[$model]` が
`Cannot index array with string` で落ち、**summary: null のフォールバックレコード**になる。
Go 版は `domain.Pricing.UnmarshalJSON` が配列を拒否するため空 pricing となり、
**summary は生成される**(ハードコード既定単価で計算される)。この 1 点のみ差異が残る。
再現には手書きの壊れた config が必要で、実運用上の経路が無いため許容する(「現行仕様との差異」に記載)。

## 6. markers の正規表現(現行版の実測)

`markers.sh`(scratchpad)で record-output.sh:149-207 の jq プログラムをそのまま実行し、判定を実測した。

### merged: `gh\s+pr\s+merge`

- 区切りは `\s+` なので `gh  pr   merge`・タブ・改行・CR・FF いずれも真
- **アンカーが無いため部分一致**: `echo gh pr merge` も真
- 大文字小文字は区別する: `GH PR MERGE` は偽
- `ghprmerge`(区切り無し)は偽、`xghy pr merge` も偽
- `mcp__github__merge_pull_request` は名前の完全一致で真。Bash 以外のツールでは command を見ない

### doc: `\.(md|mdx|txt|rst|adoc)$`

`a.md` `a.mdx` `a.txt` `a.rst` `a.adoc` が真。`a.mdd` `a.md.bak` `README.MD`(大文字) `amd` は偽。
対象ツールは Write / Edit のみで、Read の file_path は見ない。

### slack: `^mcp__slack`

前方一致なので `mcp__slackx` も真。`mcp__slac` `xmcp__slack` は偽。

### 型が違うときの現行版の挙動

| ケース | 現行版 |
|---|---|
| Write/Edit の `input` が無い / null / 文字列 | doc = false(`.input? // {}` と `?` が吸収する) |
| Write/Edit の `file_path` が数値 | **jq エラー** → summary null のフォールバック |
| Bash の `command` が数値 | **jq エラー** → 同上 |
| Read の `file_path` / `command` が数値 | 影響なし(その名前のツールは判定対象外) |

Go 版はツール名で分岐してから file_path / command を取り出し、
「対象ツールで文字列以外ならパース失敗」を再現する。

### `\s` の実体は Unicode White_Space(要注意)

jq(Oniguruma)の `\s` は ASCII 空白だけではない。コードポイントを総当たりした結果:

```console
$ jq -n -c -f ws2.jq
[9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288]
```

これは Unicode の White_Space プロパティそのものである(ZWSP U+200B は含まない)。
一方 **Go の RE2 の `\s` は `[\t\n\f\r ]` だけ**で、垂直タブ U+000B すら含まない。

```console
$ go run vt.go
"gh\vpr\vmerge" -> false     # jq は true
```

Go の `regexp` は `\p{White_Space}` を解釈できない(Categories と Scripts しか引けない)ため、
**明示的な文字クラス**へ展開して一致させる:

```
gh[\t\n\v\f\r \x{0085}\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}]+pr…
```

上の実測リストと同じ集合であることを Go のテストで固定する。

### `$`(行末アンカー)の差

Oniguruma の `$` は Perl と同じで **文字列末尾の改行 1 個の手前にも一致する**。

```console
$ jq -n -c '{nl:("a.md\n"|test("\\.(md)$")), nlb:("a.md\nb"|test("\\.(md)$")), nl2:("a.md\n\n"|test("\\.(md)$"))}'
{"nl":true,"nlb":false,"nl2":false}
```

Go の `$`(非マルチライン)は文字列末尾だけなので、doc の正規表現は
`\.(md|mdx|txt|rst|adoc)\n?$` として合わせた。`^` の意味は両者で同じだった。

## 7. codex 集計の実測(record-output.sh:70-119)

`cxcheck.sh`(scratchpad)で codex 用の jq プログラムをそのまま実行した。

| 入力 | 現行版 |
|---|---|
| `_call` で終わる payload.type だけがツール(`_call_output` は除外) | tool_calls 2 / tools_used ["apply_patch","exec"] |
| `payload.name` が無ければ `payload.type` をツール名にする | tools_used ["local_shell_call"] |
| model は **最後** の turn_context(claude は最初の message.model) | "gpt-5.6-thinking" |
| usage は **最後** の token_count の total_token_usage(累計値) | in 25 / out 3 / read 5 / write 7 |
| `total_token_usage` が null の token_count は候補から外れ、直前の値が残る | in 10 / out 1 |
| `.input // .arguments // ""` の順で merged 判定の文字列を選ぶ | arguments でも真 |
| input がオブジェクトなら `tostring` で compact JSON になる | `{"cmd":"gh pr merge 1"}` |

`tostring` の出力も実測した(オブジェクト → compact JSON、数値 → `"5"`、文字列 → そのまま)。
Go 側は `json.Compact` で再現する(キーの並びは入力のまま保たれる)。

パース失敗になるのは以下(いずれも実測):壊れた行 / トップレベルがオブジェクト以外 /
各行種別で payload がスカラー / token_count の info・total_token_usage がスカラー /
input_tokens が文字列 / response_item の payload.type が数値 / turn_context の model が数値。

## 8. 現行仕様との差異(意図的に一致させなかった点)

いずれも「現行版は summary を出すが Go 版はフォールバック(summary: null)へ落とす」方向で、
値を偽らない側に倒してある。実在の transcript / rollout では起こらない型崩れのみである。

1. **codex のツール名が文字列でない**。現行版は `.name // .type` を `unique` に流すだけなので
   `tools_used: [7]` のように数値が混ざった summary を出す(実測)。
   `ToolsUsed []string` の Go 版では同じ JSON を作れないため、パース失敗にする
2. **claude の `message.model` / `usage.speed` が文字列でない**。model は現行版でも
   `$pricing[$model]` で落ちるため一致するが、speed は現行版だと数値のまま summary に載る
3. **`pricing` が JSON オブジェクト以外**(5 節に既述)。現行版はフォールバック、Go 版は空 pricing で続行
4. **モデルの単価に `input` などのキーが無い**。現行版は `$in * null` で落ちるが、
   Go 版は `ModelPricing` のゼロ値(0)として計算する

## 9. ゴールデンテストと変異テスト

### fixture の作り方

`scripts/gen-golden-record.sh` が現行 record-output.sh を隔離環境
(`env -i` + `HOME` / `CONDUCTOR_HOME` を `mktemp -d` 配下へ)で実行し、
追記された daily log を `internal/infra/store/testdata/golden-record/<case>/expected/daily/`
に保存する。実環境のファイルには一切触れない(hook 側の `scripts/gen-golden.sh` と同じ方式)。

transcript の絶対パスは実行のたびに変わるため、pending には `{{TRANSCRIPT_DIR}}` という
プレースホルダを書き、実行直前に実パスへ、保存時にプレースホルダへ往復させる。
Go 側のテストも同じ置換を行う。

完了時刻は fixture の最後の行の `completed_at` から復元して固定クロックに与える。
日付をまたいだ瞬間に生成されると再現できない fixture になるため、
daily のファイル名と `completed_at` の日付が一致するまで生成をやり直す。

### ケース一覧(26 件)

| case | 何を固定するか |
|---|---|
| claude-full | 正常系(turns / tool_calls / tools_used / markers / 任意フィールド) |
| claude-cache-cost | キャッシュトークン込みの料金(0.082) |
| claude-fast-multiplier | speed=fast の 6 倍(0.18) |
| claude-sonnet | sonnet の単価(0.018) |
| pricing-fallback-sonnet | 未知モデル → claude-sonnet-4-6 |
| pricing-fallback-hardcoded | sonnet も無い → ハードコード既定単価 |
| pricing-no-config | 設定ファイルが 1 つも無い |
| pricing-broken-user-config | 壊れた config.json → config.default.json |
| claude-merge-mcp / claude-merge-bash / claude-no-merge | markers.merged の 2 経路と偽 |
| codex-full | codex の集計と gpt の単価(9.5) |
| codex-unknown-model | 単価未知は cost null |
| claude-parse-failure / -empty-message / codex-parse-failure | 解析失敗の 3 パターン |
| no-transcript / -empty-message / transcript-file-missing | transcript 無しの 3 パターン |
| agent-carried | transcript 無しでも agent が残る |
| minimal-pending | 空の任意フィールドはキーごと省略 |
| duplicate-tab-picks-first | 同一タブはファイル名の昇順で最初の 1 件 |
| broken-pending-skipped | 壊れた pending を読み飛ばす |
| no-pending | 該当が無ければ何も書かない |
| outside-zellij | セッション名は unknown |
| existing-daily-appended | 既存行を保って末尾に追記する |

### 変異テスト(31 件すべて検出)

実装に 1 箇所ずつ誤りを入れ、ゴールデンテストが必ず落ちることを確認した
(scratchpad の `mutate.sh` / `mutate2.sh`。毎回 `git checkout` で戻す)。
ビルドが通らない変異は「変異として無効」として除外し、振る舞いだけが変わる形へ書き直してある。

| 変異 | 落ちた case 数 |
|---|---|
| cost の丸めを切り捨てにする | 1 |
| fast_multiplier の既定値を 6 → 1 | 1 |
| fast の倍率を無視する | 12 |
| sonnet へのフォールバックを消す | 1 |
| ハードコード既定単価を変える | 3 |
| codex でも claude の単価へフォールバックする | 2 |
| codex の input からキャッシュ分を引かない | 3 |
| codex の usage を最初の token_count にする | 1 |
| codex の model を最初の turn_context にする | 1 |
| codex の speed を standard 以外にする | 3 |
| claude の model を最後の 1 件にする | 1 |
| tools_used を降順に並べる | 2 |
| tools_used の重複を除かない | 3 |
| merged の正規表現を先頭一致にする | 3 |
| doc の拡張子から md を外す | 2 |
| slack の判定を部分一致にする | 1 |
| codex で slack / doc も判定する | 3 |
| Parse failed の既定文言を変える | 3 |
| No summary available の既定文言を変える | 3 |
| 成功時にも既定文言を当てる | 1 |
| 空の任意フィールドを省略しない | 24 |
| agent を落とす | 5 |
| completed_at の書式を RFC3339 にする | 26 |
| pending をファイル名の降順で探す | 2 |
| 壊れた pending でも探索を打ち切る | 2 |
| daily を追記でなく上書きにする | 2 |
| pricing のフォールバック順を逆にする | 1 |
| 空の pricing でも次のファイルへ落ちる | 1 |

## 10. その他の差異(仕様ではなく実装都合)

1. **pending の並び順**: 現行版の glob はロケールの照合順、Go の `os.ReadDir` はバイト順。
   セッション ID(UUID 相当)では一致する。
2. **daily ディレクトリの先行作成**: 現行版は pending の有無に関わらず
   `mkdir -p "$DAILY_DIR"` を実行するため、何も書かない場合でも空ディレクトリが残る。
   Go 版は追記するときだけ作る。ゴールデンテストはファイルの集合で比較するため差は出ない。
3. **pending ディレクトリを読めない場合**: 現行版は glob が何も返さず黙って exit 0 するが、
   Go 版はエラーを返す。原因の分かる失敗を握り潰さないためである。
