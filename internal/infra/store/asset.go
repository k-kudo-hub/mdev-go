package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/k-kudo-hub/mdev-go/assets"
)

// 埋め込みの使いどころ(ADR-0004 D4-4 の解釈)
//
// **設定や単価表の読み込み経路に埋め込みを混ぜない。** 6-2 で一度そうしたが
// 撤回した。理由は 2 つある。
//
//   - 既存の挙動が変わる。設置物が欠けた状態は現行版ではゼロ値の設定として
//     扱われており、そこへ埋め込みの既定値を入れると、hook 経路の課金記録が
//     黙って変わりうる。6-2 は「受け皿を用意する」フェーズで、既存の挙動へ
//     影響を出さないことが要件である
//   - 本番経路では到達しない。設置済みの CONDUCTOR_HOME には
//     config.default.json が必ずあるため、埋め込みへ落ちる分岐を保証するのは
//     テストだけになる
//
// 埋め込みが埋めるのは「まだ何も置かれていない」時点であり、そこを埋める
// 責任は読み込み側ではなく **設置側** にある。6-3 の `mdev install` が
// ReadAsset で取り出して実ファイルとして配置し、以降の読み込みは今までどおり
// 実ファイルだけを見る。この関数はその配置のための取り出し口である。

// ErrUnknownAsset は名前に覚えが無いことを表す。
var ErrUnknownAsset = errors.New("そのような資産はありません")

// AssetNames は同梱されている資産の名前を返す(CONDUCTOR_HOME からの相対パス)。
func AssetNames() []string { return assets.Names() }

// ReadAsset は資産の中身を返す。
//
// CONDUCTOR_HOME に同じ名前の実ファイルがあればそれを、無ければ実行ファイルに
// 埋め込まれたものを返す。**実ファイルを優先するのがこの関数の要点**である。
// 利用者がレイアウトや既定設定に手を入れた場合、その変更が更新のたびに
// 消えるのでは手を入れる意味が無い。埋め込みは「まだ何も置かれていない」
// ときの土台であって、上書きの対象ではない。
//
// 実ファイルがあるが読めない場合はエラーを返す。権限や壊れた設置を黙って
// 埋め込みで埋めると、利用者が置いたはずの内容と食い違ったまま動く。
//
// **有無の判定は Lstat で先に行う。** 読み取りの失敗をそのまま「無い」と
// 見なすと、リンク切れの symlink が置かれている場合に埋め込みへ落ちてしまう。
// そこに在るのは「利用者が置いた壊れた設置」であって「未設置」ではないので、
// 黙って別の内容を返さず、直すべき状態として報告する。
func ReadAsset(conductorHome, name string) ([]byte, error) {
	embedded, known := assets.Read(name)
	if !known {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAsset, name)
	}

	path := filepath.Join(conductorHome, filepath.FromSlash(name))
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return embedded, nil
		}
		return nil, fmt.Errorf("資産 %s の確認に失敗しました: %w", path, err)
	}

	b, err := os.ReadFile(path) //nolint:gosec // 埋め込み済みの名前に限る
	if err != nil {
		return nil, fmt.Errorf("資産 %s の読み取りに失敗しました: %w", path, err)
	}
	return b, nil
}
