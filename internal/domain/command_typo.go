package domain

// `mdev <未知の引数>` はセッション名として扱う。起動の入口を 1 語に保つ
// ためだが、そのままでは `mdev instal` のような打ち間違いが黙って
// 「instal というセッションを開く」に化ける。
//
// そこで、既知のコマンド名と **編集距離 1** の引数だけを差し戻す。1 文字の
// 打ち間違い(抜け・余り・取り違え・隣接の入れ替え)はほぼ打ち間違いだが、
// 2 文字以上違うならそれは別の語であり、利用者が意図した名前と見てよい。

// TypoDistanceLimit は打ち間違いと見なす編集距離の上限である。
const TypoDistanceLimit = 1

// NearestCommand は name に最も近い既知のコマンド名を返す。
//
// 編集距離が TypoDistanceLimit を超える場合は ok=false を返す(その語は
// 打ち間違いではなく、利用者が意図した名前と見なす)。完全一致は呼び出し
// 側で先に解決されている前提なので、ここでは扱わない。
//
// 候補が複数ある場合は commands の並び順で最初のものを返す。1 文字違いの
// コマンドが 2 つある状況で、どちらを勧めても打ち間違いの指摘としては
// 用を成す。
func NearestCommand(name string, commands []string) (string, bool) {
	for _, command := range commands {
		if command != name && editDistanceWithin(name, command, TypoDistanceLimit) {
			return command, true
		}
	}
	return "", false
}

// editDistanceWithin は a と b の編集距離が limit 以下かを返す。
//
// 距離そのものは要らないので、長さの差で早々に諦める。比較は **文字単位**で
// 行う(コマンド名は ASCII だが、引数には日本語が来うる)。
func editDistanceWithin(a, b string, limit int) bool {
	ar, br := []rune(a), []rune(b)
	if abs(len(ar)-len(br)) > limit {
		return false
	}
	return editDistance(ar, br) <= limit
}

// editDistance はレーベンシュタイン距離を返す。
//
// 隣接の入れ替え(`mdev isntall`)はレーベンシュタインでは距離 2 になる。
// 打ち間違いとしてはよくある形だが、ここでは拾わない。距離 2 まで広げると
// 「dev」と「zs」ほどの短い語で無関係な名前まで巻き込むためである。
func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// abs は絶対値を返す。
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// RenderCommandTypo は打ち間違いと思われる引数への案内を返す。
func RenderCommandTypo(name, nearest string) string {
	return "'" + name + "' はコマンドではありません('" + nearest + "' の打ち間違いでしょうか)。\n" +
		"セッション '" + name + "' を開くには: mdev attach " + name
}

// RenderOpeningSession はセッションを開くことを伝える 1 行を返す。
//
// 未知の引数をセッション名として扱う以上、**何が起きるかを先に言う**。
// 打ち間違いが差し戻しをすり抜けた場合でも、画面に名前が出ていれば
// 気づける。
func RenderOpeningSession(name string) string {
	return "セッション '" + name + "' を開きます\n"
}
