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
