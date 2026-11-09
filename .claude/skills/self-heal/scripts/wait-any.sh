#!/bin/sh
# Block until something needs the driver, then print it and exit:
#   WAITING <run> <bot>   a run waits on an approval card
#   LEAD <run> <status>   the lead finished a run (after SINCE) whose reply asks for review or a decision, or failed
#   LOWMEM <mb>           free memory under 2 GB
# Usage: wait-any.sh <since_unix_micros>   (pass `date +%s%6N` taken right after your last action)
# Also appends free memory to logs/mem.log every 30 s.
D=${REEF_HOME:-$HOME/reef-dogfood}; DB=$D/data/atoll.db; SINCE=${1:-0}; LEAD=${REEF_LEAD:-poteto-agent}; n=0
LID=$(sqlite3 "$DB" "select id from bots where name='$LEAD'")
while :; do
  W=$(sqlite3 "$DB" "select r.id||' '||b.name from runs r join threads t on t.id=r.thread_id join bots b on b.id=t.bot_id where r.status='waiting' limit 1")
  [ -n "$W" ] && { echo "WAITING $W"; exit 0; }
  L=$(sqlite3 "$DB" "select r.id||' '||r.status from runs r join threads t on t.id=r.thread_id where t.bot_id='$LID' and r.finished_at>$SINCE and r.status!='running' and (r.reply like '%ready for your review%' or r.reply like '%your call%' or r.reply like '%your a/b%' or r.reply like '%need your%' or r.status='failed') order by r.finished_at desc limit 1")
  [ -n "$L" ] && { echo "LEAD $L"; sqlite3 "$DB" "select finished_at||char(10)||reply from runs where id='${L%% *}'"; exit 0; }
  n=$((n+1)); if [ $((n % 6)) -eq 0 ]; then
    A=$(free -m | awk '/Mem:/{print $7}')
    echo "$(date +%T) avail_mb=$A $(pgrep -c -x claude) claude $(pgrep -c codex) codex" >> "$D/logs/mem.log"
    [ "$A" -lt 2000 ] && { echo "LOWMEM $A"; exit 0; }
  fi
  sleep 5
done
