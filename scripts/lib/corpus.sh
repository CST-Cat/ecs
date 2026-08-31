#!/usr/bin/env bash
# 固定 Silesia 语料的唯一构建实现。
#
# 这份语料有两个消费者：工具包 stage（build_tools.sh 放进 share/ecs/corpus）
# 和独立发布物（build_corpus.sh 打成 tar.gz）。两者此前各写了一遍下载、解压、
# 按 lock.json 顺序拼接和校验的逻辑——同一个过程写两遍，校验自然也就出现两遍。
#
# 语料内容直接决定 zstd 跑分的可比性，因此这里的两处校验各有独立含义：
# 下载校验确认镜像给的 ZIP 就是 lock.json 钉住的那份（跨越信任边界），
# 拼接后校验确认按 lock.json 的成员顺序拼出的字节流没有偏差（本地构建结果）。

# ecs_build_silesia_corpus WORK_DIR OUTPUT_PATH
#
# 在 WORK_DIR 中下载并解压 Silesia ZIP，按 lock.json 的成员顺序拼接出固定语料，
# 写到 OUTPUT_PATH。OUTPUT_PATH 的父目录必须已存在。
ecs_build_silesia_corpus() {
  local work=$1 output=$2
  local source_url source_sha corpus_sha corpus_bytes
  local zip_path source_dir member actual_bytes actual_sha
  local -a order

  source_url=$(ecs_lock_corpus_field source_url) || return 1
  source_sha=$(ecs_lock_corpus_field source_sha256) || return 1
  corpus_sha=$(ecs_lock_corpus_field sha256) || return 1
  corpus_bytes=$(ecs_lock_corpus_field bytes) || return 1
  mapfile -t order < <(ecs_lock_corpus_order) || return 1
  [[ "${#order[@]}" -gt 0 ]] || {
    echo 'ecs-corpus: tools lock has no corpus member order' >&2
    return 1
  }

  zip_path="$work/silesia.zip"
  source_dir="$work/silesia"
  ecs_download_sha256 "$source_url" "$source_sha" "$zip_path" 'Silesia source ZIP' || return 1
  mkdir -p "$source_dir"
  unzip -q "$zip_path" -d "$source_dir" || {
    echo 'ecs-corpus: Silesia download is not a valid ZIP archive' >&2
    return 1
  }

  : >"$output"
  for member in "${order[@]}"; do
    [[ -f "$source_dir/$member" ]] || {
      echo "ecs-corpus: Silesia ZIP omitted $member" >&2
      return 1
    }
    cat "$source_dir/$member" >>"$output"
  done

  actual_bytes=$(stat -c %s "$output")
  [[ "$actual_bytes" -eq "$corpus_bytes" ]] || {
    echo "ecs-corpus: fixed corpus byte length = $actual_bytes, want $corpus_bytes" >&2
    return 1
  }
  actual_sha=$(sha256sum "$output" | awk '{print $1}')
  [[ "$actual_sha" == "$corpus_sha" ]] || {
    echo "ecs-corpus: fixed corpus SHA-256 = $actual_sha, want $corpus_sha" >&2
    return 1
  }
  chmod 0644 "$output"
}
