#!/usr/bin/env bash
# 无人值守安装：等价于 setup.sh --auto。
# 交互式菜单请直接运行 setup.sh。
#
#   scp nonerp-linux-amd64 setup.sh root@HOST:/root/
#   ssh root@HOST 'bash /root/setup.sh --domain mc.example.com'

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
if [ ! -f "$HERE/setup.sh" ]; then
  printf 'install.sh 需要同目录下有 setup.sh
' >&2
  exit 1
fi

exec bash "$HERE/setup.sh" --auto "$@"
