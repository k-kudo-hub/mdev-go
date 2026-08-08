// Package cli はサブコマンドを定義する(cobra)。
//
// 制約(ADR-0002):
//   - 依存してよい internal パッケージは internal/app のみ
//   - internal/tui を参照しない(相互参照禁止)
//   - 入出力の変換のみを行い、業務判断を持たない
package cli
