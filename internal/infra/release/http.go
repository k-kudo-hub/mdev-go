package release

import (
	"fmt"
	"io"
	"net/http"
)

// newHTTPClient は配布物の取得に使う HTTP クライアントを返す。
//
// file:// を扱えるようにしてあるのは、更新の流れを実際のネットワーク無しで
// 試せるようにするためである(現行版も CONDUCTOR_TARBALL_URL に file:// を
// 渡す形でテストしている)。tarball の取得とバイナリの取得で同じ作りを使う。
func newHTTPClient() *http.Client {
	transport := &http.Transport{}
	transport.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	return &http.Client{Timeout: downloadTimeout, Transport: transport}
}

// getOK は URL を取得し、200 以外を失敗として返す。
// 応答の本体は呼び出し側が閉じる。
func getOK(client *http.Client, url string) (*http.Response, error) {
	resp, err := client.Get(url) //nolint:noctx // Client.Timeout で上限を持つ
	if err != nil {
		return nil, err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("状態コード %d", resp.StatusCode)
	}
	return resp, nil
}

// copyLimited は src から dst へ最大 maxBytes まで写し、書いた量を返す。
//
// **上限に達したら error にする。** 上限ちょうどで切ると、切り詰めた内容を
// 正常なものとして扱ってしまう(壊れた tarball を展開しにいく、途中までの
// バイナリを実行ファイルとして置く)。上限より 1 バイト多く読み、書けた量で
// 超過を判定する。
func copyLimited(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, maxBytes+1))
	if err != nil {
		return written, err //nolint:wrapcheck // 呼び出し側が用途に応じて包む
	}
	if written > maxBytes {
		return written, fmt.Errorf("受け取った内容が上限 %d バイトを超えました", maxBytes)
	}
	return written, nil
}
