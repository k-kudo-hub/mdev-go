// Package tui はダッシュボード TUI を実装する(Bubble Tea v2)。
//
// 制約(ADR-0002):
//   - 依存してよい internal パッケージは internal/app のみ
//   - internal/cli を参照しない(相互参照禁止)
//   - 入出力の変換のみを行い、業務判断を持たない
package tui
