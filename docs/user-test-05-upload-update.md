# ユーザーテスト 05: ログアップロード / News / update(フェーズ 5)

フェーズ 5 で Go 化した機能の実地確認。これが合格すると **Shell 版との機能パリティ達成**(残りは install.sh 本体と init.zsh のみ = 配布モデル再設計後の統合作業)。

## 前提(セットアップ済み)

- mdev-go **v0.9.0** を `~/.claude-conductor/bin/mdev` へ配備済み
- claude-conductor **v0.8.0**(FLAVOR 対応 install.sh)を適用済み
- `mdev hooks switch` 実行済み → **hooks は Go 版**、`~/.claude-conductor/FLAVOR` = `go`
- install.sh 再実行での巻き戻り無しは検証済み(レイアウト 5 ペイン Go 版維持・hooks 不変・バックアップ増加なし)
- upload 設定: `k-kudo-hub/context-hub` / work-log / main(有効)

## テスト項目

### 1. hooks の Go 版動作(Waiting / Done の反映)

1. `mdev` でセッションを起動し、`n` で新規タスクを作成
2. タスク内で Claude と 1〜2 往復し、入力待ちで放置
3. **期待**: Waiting パネルにタブが表示される(Go 版 `mdev hook notify` 経由)
4. プロンプトに入力を送る → **期待**: Waiting から消える(`mdev hook resolve` 経由)

### 2. ログアップロード付き削除(dd)— 最重要

1. 会話履歴のあるタスクタブで Main タブの task-control から `dd`(または Dashboard で `d`+番号)
2. **期待**:
   - `Uploading log...` の後、`アップロードしました -> https://github.com/k-kudo-hub/context-hub/blob/main/work-log/...` が表示されタブが閉じる
   - Done パネルに 1 件だけ記録が増える(重複しない)
   - context-hub の該当パスに markdown(サマリ表 + 会話要約)が push されている
3. 確認ポイント: 要約が日本語の箇条書きであること、markdown 内に生のシークレットが無いこと

### 3. News の再取得

1. News ペインで `r`
2. **期待**: エラーなく一覧が更新される(Go 版 fetcher。表示形式は従来と同じ)

### 4. mdev update / check-update

```sh
~/.claude-conductor/bin/mdev update
# 期待: 「既に最新です(v0.8.0)。」

~/.claude-conductor/bin/mdev check-update --force
# 期待: 何も表示されない(最新のため)。~/.claude-conductor/.update-check に「<今日> v0.8.0」
```

### 5. 巻き戻り防止の再確認(任意)

```sh
cd ~/projects/claude-conductor && echo "n" | ./install.sh
grep -c "bin/mdev pane" ~/.claude-conductor/layouts/multi.kdl   # 期待: 5
jq -r '.hooks.Stop[0].hooks[-1].command' ~/.claude/settings.json # 期待: .../bin/mdev hook notify
```

## 問題が出た場合

- dd が `Upload failed. Deletion cancelled.` になる → タブは残る(会話は失われない)。そのまま報告してください
- hooks を Shell 版に戻す: `~/.claude-conductor/bin/mdev hooks restore`(FLAVOR も消え、次の install で完全に Shell 版へ戻る)
