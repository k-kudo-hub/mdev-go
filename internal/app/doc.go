// Package app はユースケースを組み立てる。
//
// ユースケースが必要とする操作を port(interface)としてこのパッケージで定義し、
// その実装(adapter)は internal/infra に置く。
//
// 制約(ADR-0002):
//   - 依存してよい internal パッケージは internal/domain のみ
//   - port は「ユースケースが必要とする操作」単位で定義し、
//     Zellij のコマンド体系をそのまま interface にしない
package app
