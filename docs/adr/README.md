# Architecture Decision Records

このディレクトリには、mdev-go の設計判断を ADR(Architecture Decision Record)として記録する。

## 運用ルール

- ファイル名は `NNNN-short-title.md`(NNNN は 4 桁連番)
- 一度 Accepted になった ADR は書き換えない。判断を変える場合は新しい ADR を作成し、古い ADR の Status を `Superseded by ADR-NNNN` に更新する
- 実装に影響する設計判断(アーキテクチャ、採用ライブラリ、品質基準、互換性方針)は必ず ADR にしてからコードを書く

## Status の遷移

| Status | 意味 |
|--------|------|
| Proposed | 提案中。レビュー待ち |
| Accepted | 承認済み。実装の根拠となる |
| Superseded by ADR-NNNN | 新しい ADR に置き換えられた |
| Rejected | 却下された(記録として残す) |

## テンプレート

```markdown
# ADR-NNNN: タイトル

- Status: Proposed
- Date: YYYY-MM-DD

## Context

判断が必要になった背景。事実のみを書く。

## Decision

決定内容。「〜する」と断定形で書く。

## Consequences

この決定によって得られるもの・失うもの・課される制約。
```
