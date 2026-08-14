package domain

import "path/filepath"

// SessionState はセッションの今の状態である。
type SessionState int

const (
	// SessionAbsent はその名前のセッションが無いことを表す。
	SessionAbsent SessionState = iota
	// SessionAlive は動いているセッションがあることを表す。
	SessionAlive
	// SessionExited は落ちたまま残っていることを表す。
	SessionExited
)

// NewSessionTimeLayout は `--new` が名前へ足す時刻の書式(現行版の `date +%H%M%S`)。
const NewSessionTimeLayout = "150405"

// SessionRequest はどのセッションを開くかの指定である。
type SessionRequest struct {
	// Name は利用者が指定した名前。空なら作業ディレクトリから決める。
	Name string
	// Dir は今いる作業ディレクトリ。
	Dir string
	// Stamp は `--new` のときに足す時刻。空なら足さない。
	Stamp string
}

// SessionName は開くセッションの名前を返す。
//
// 現行 init.zsh の mdev() を移したものである。
//
//   - 名前の指定があればそれを使い、ハッシュ源も同じ文字列にする
//   - 指定が無ければ作業ディレクトリの basename を名前に、**パス全体**を
//     ハッシュ源にする。名前が同じでも別のディレクトリなら別のセッションに
//     なるのはこのためである
//   - `--new` は名前とハッシュ源の**両方**へ時刻を足す。長い名前では時刻が
//     切り詰めで消えるため、ハッシュ側にも入れないと既定のセッションと
//     同じ名前になってしまう
func (r SessionRequest) SessionName() string {
	base, hashSrc := r.Name, r.Name
	if base == "" {
		base = filepath.Base(r.Dir)
		hashSrc = r.Dir
	}
	if r.Stamp != "" {
		base += "-" + r.Stamp
		hashSrc += "-" + r.Stamp
	}
	return ZellijSessionName(base, hashSrc)
}

// ParseSessionState は list-sessions の出力から name の状態を読む。
//
// 現行版は `awk '$1 == n {print; exit}'` で名前が一致する最初の行を見て、
// その行に EXITED が含まれるかで分けていた。ここでは掃除と同じ
// ParseSessionList を通す。名前そのものが " [Created " や "(EXITED" を
// 含む場合の取り違えを、あちらが既に解いているためである。
func ParseSessionState(out, name string) SessionState {
	for _, entry := range ParseSessionList(out) {
		if entry.Name != name {
			continue
		}
		if entry.Exited {
			return SessionExited
		}
		return SessionAlive
	}
	return SessionAbsent
}
