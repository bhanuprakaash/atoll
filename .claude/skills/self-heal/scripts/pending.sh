#!/bin/sh
# Print the status of a Bot's newest run and the commands of its open approval cards. Usage: pending.sh <bot name>
D=${REEF_HOME:-$HOME/reef-dogfood}; DB=$D/data/atoll.db
R=$(sqlite3 "$DB" "select r.id from runs r join threads t on t.id=r.thread_id join bots b on b.id=t.bot_id where b.name='$1' order by r.started_at desc limit 1")
F=$D/data/runs/$R.jsonl
sqlite3 "$DB" "select id||' '||status from runs where id='$R'"
closed=$(jq -r 'select(.type=="approval_closed")|.data.id' "$F")
jq -r 'select(.type=="approval")|.data|(.id+"\t"+((.input.command//.input.path//.tool)|tostring|.[0:300]))' "$F" |
while IFS="$(printf '\t')" read -r id c; do echo "$closed" | grep -qx "$id" || echo "OPEN: $c"; done
