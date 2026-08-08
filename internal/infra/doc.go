// Package infra は internal/app が定義する port の実装(adapter)を持つ。
//
// Zellij CLI の呼び出し、ファイル入出力、プロセス起動、通知、GitHub API 呼び出しは
// すべてこの配下のサブパッケージ(zellij / store / agent / notify / github)に閉じ込める。
//
// 制約(ADR-0002):
//   - 依存してよい internal パッケージは internal/app と internal/domain のみ
package infra
